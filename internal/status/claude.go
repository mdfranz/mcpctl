package status

import (
	"regexp"
	"strings"
	"time"
)

// claudeListEntry is one line parsed from `claude mcp list`.
type claudeListEntry struct {
	Name       string
	Descriptor string // command, or "url (HTTP)" for remote servers
	StatusText string
}

// Claude separates a server name from its descriptor with a colon followed by
// whitespace. Names themselves may contain colons (for example plugin server
// names such as "plugin:logfire:logfire"), so the separator must not match a
// bare colon inside the name or inside a URL such as "https://".
var claudeListLine = regexp.MustCompile(`^([^\s].*?):[ \t]+(.+?)\s+-\s+(.+)$`)

// parseClaudeList parses `claude mcp list` output. It tolerates a leading
// informational warning line (observed: a claude.ai-connectors notice)
// and a "Checking MCP server health…" progress line, neither of which is
// a server entry, and the empty-project message.
func parseClaudeList(output string) []claudeListEntry {
	var entries []claudeListEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "⚠") {
			continue
		}
		if strings.Contains(line, "Checking MCP server health") {
			continue
		}
		if strings.Contains(line, "No MCP servers configured") {
			continue
		}
		m := claudeListLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		entries = append(entries, claudeListEntry{Name: m[1], Descriptor: m[2], StatusText: m[3]})
	}
	return entries
}

// classifyClaudeStatus maps a status phrase from `claude mcp list`
// output to canonical states. Confirmed against a real Claude Code CLI:
// "Pending approval" (.mcp.json servers awaiting project trust) and
// "✘ Failed to connect" (U+2718 HEAVY BALLOT X, not U+2717 "✗" as
// originally guessed; the "Failed" substring check is what actually
// caught it before this was fixed). The ✓/Connected success mapping
// remains unconfirmed against live output; treat it as provisional.
// Anything unrecognized is reported as an incomplete check rather than
// guessed as success or failure.
func classifyClaudeStatus(statusText string) (ConfigState, ConnectionState, AuthState, CheckState) {
	switch {
	case strings.Contains(statusText, "Pending approval"):
		return ConfigPendingApproval, ConnectionUnchecked, AuthUnknown, CheckComplete
	case strings.Contains(strings.ToLower(statusText), "rejected"):
		return ConfigRejected, ConnectionUnchecked, AuthUnknown, CheckComplete
	case strings.Contains(strings.ToLower(statusText), "disabled"):
		return ConfigDisabled, ConnectionUnchecked, AuthUnknown, CheckComplete
	case strings.HasPrefix(statusText, "✓") || strings.Contains(statusText, "Connected"):
		return ConfigPresent, ConnectionConnected, AuthUnknown, CheckComplete
	case strings.HasPrefix(statusText, "✘") || strings.HasPrefix(statusText, "✗") || strings.Contains(statusText, "Failed"):
		return ConfigPresent, ConnectionFailed, AuthUnknown, CheckComplete
	default:
		return ConfigUnknown, ConnectionUnchecked, AuthUnknown, CheckUnavailable
	}
}

var claudeScopeLine = regexp.MustCompile(`(?m)^\s*Scope:\s*(.+)$`)

// parseClaudeScope reads the Scope line from `claude mcp get <name>`
// output. Confirmed observed value: "Project config (shared via
// .mcp.json)" -> ScopeProject. Any other populated Scope line is treated
// as ScopeOther (some non-project source) rather than guessed further.
func parseClaudeScope(getOutput string) Scope {
	scope, _, _ := parseClaudeAttribution(getOutput)
	return scope
}

func parseClaudeAttribution(getOutput string) (Scope, SourceKind, SourceConfidence) {
	m := claudeScopeLine.FindStringSubmatch(getOutput)
	if m == nil {
		return ScopeUnknown, SourceUnknown, SourceUnknownConfidence
	}
	sourceText := strings.ToLower(m[1])
	switch {
	case strings.Contains(sourceText, "project config"):
		return ScopeProject, SourceProject, SourceConfirmed
	case strings.Contains(sourceText, "plugin"):
		return ScopeOther, SourcePlugin, SourceConfirmed
	case strings.Contains(sourceText, "managed") || strings.Contains(sourceText, "enterprise"):
		return ScopeOther, SourceManaged, SourceConfirmed
	case strings.Contains(sourceText, "global"):
		return ScopeOther, SourceGlobal, SourceConfirmed
	case strings.Contains(sourceText, "user") || strings.Contains(sourceText, "claude.ai"):
		return ScopeOther, SourceUser, SourceConfirmed
	default:
		return ScopeOther, SourceUnknown, SourceConfirmed
	}
}

// BuildClaudeResults turns one `claude mcp list` run into Results. getFor,
// if non-nil, is called once per listed server to run `claude mcp get
// <name>` and resolve Scope; check.go supplies it bound to a
// context/timeout/dir so this function stays free of process execution.
func BuildClaudeResults(listOut, clientVersion string, checkedAt time.Time, listEvidence Evidence, getFor func(name string) (string, Evidence, error)) []Result {
	entries := parseClaudeList(listOut)
	results := make([]Result, 0, len(entries))
	for _, e := range entries {
		configState, conn, auth, checkState := classifyClaudeStatus(e.StatusText)
		res := Result{
			ServerName:       e.Name,
			Client:           "claude",
			ClientVersion:    clientVersion,
			Target:           e.Descriptor,
			ConfigState:      configState,
			Connection:       conn,
			AuthState:        auth,
			AuthMethod:       AuthMethodUnknown,
			CheckState:       checkState,
			Scope:            ScopeUnknown,
			Source:           SourceUnknown,
			SourceConfidence: SourceUnknownConfidence,
			CheckedAt:        checkedAt,
			Evidence:         []Evidence{listEvidence},
		}
		if getFor != nil {
			getOut, getEv, err := getFor(e.Name)
			res.Evidence = append(res.Evidence, getEv)
			if err == nil {
				res.Scope, res.Source, res.SourceConfidence = parseClaudeAttribution(getOut)
			}
		}
		results = append(results, res)
	}
	return results
}
