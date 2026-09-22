package status

import (
	"strings"
	"time"

	"github.com/mdfranz/mcpctl/internal/config"
)

// opencodeEntry is one server block parsed from `opencode mcp list`,
// whose output is an ANSI-styled box-drawing UI, not structured data.
type opencodeEntry struct {
	Marker     string // e.g. "✗"
	Name       string
	StatusText string
	Detail     []string // indented lines under the entry (error text, command/url)
}

// parseOpenCodeList parses `opencode mcp list` output after stripping
// ANSI escapes. Recognized structure (confirmed against real output):
//
//	┌  MCP Servers
//	│
//	●  ✗ <name> <status>
//	│      <detail line 1>
//	│      <detail line 2>
//	│
//	└  N server(s)
//
// and the empty-project case, a "▲  No MCP servers configured" line with
// no entries.
func parseOpenCodeList(raw string) []opencodeEntry {
	clean := stripANSI(raw)
	var entries []opencodeEntry
	var current *opencodeEntry

	for _, line := range strings.Split(clean, "\n") {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))
		switch {
		case trimmed == "":
			continue
		case strings.HasPrefix(trimmed, "●"):
			fields := strings.Fields(strings.TrimPrefix(trimmed, "●"))
			if len(fields) < 2 {
				current = nil
				continue
			}
			entries = append(entries, opencodeEntry{
				Marker:     fields[0],
				Name:       fields[1],
				StatusText: strings.Join(fields[2:], " "),
			})
			current = &entries[len(entries)-1]
		case strings.HasPrefix(trimmed, "│"):
			detail := strings.TrimSpace(strings.TrimPrefix(trimmed, "│"))
			if detail != "" && current != nil {
				current.Detail = append(current.Detail, detail)
			}
		case strings.HasPrefix(trimmed, "└"):
			current = nil
		default:
			// "┌  MCP Servers" header, "▲  No MCP servers configured", or
			// anything else unrecognized: not a server entry, ignore.
			continue
		}
	}
	return entries
}

// classifyOpenCodeStatus maps a status word from `opencode mcp list` to
// canonical states. Only "failed" has been confirmed against real
// output (a stdio server that doesn't speak MCP, and an unreachable
// remote URL). "connected"/"ok"/"disabled" are plausible based on
// OpenCode's own terminology but unconfirmed; anything not recognized is
// reported as an incomplete check rather than guessed.
func classifyOpenCodeStatus(statusText string) (ConfigState, ConnectionState, CheckState) {
	switch strings.ToLower(statusText) {
	case "failed":
		return ConfigPresent, ConnectionFailed, CheckComplete
	case "connected", "ok":
		return ConfigPresent, ConnectionConnected, CheckComplete
	case "disabled":
		return ConfigDisabled, ConnectionUnchecked, CheckComplete
	default:
		return ConfigUnknown, ConnectionUnchecked, CheckUnavailable
	}
}

// opencodeScope infers Scope by checking whether any detail line
// mentions the project's own command/url for that server name. OpenCode
// gives no explicit scope field, and this project's opencode.json is the
// only source checked, so a name-only match without a descriptor match
// is reported as ScopeUnknown rather than assumed to be this project's.
func opencodeScope(entry opencodeEntry, projectServers map[string]config.Server) Scope {
	proj, ok := projectServers[entry.Name]
	if !ok {
		return ScopeOther
	}
	var want string
	switch proj.Type {
	case config.ServerTypeStdio:
		want = proj.Command
	case config.ServerTypeRemote:
		want = proj.URL
	}
	if want == "" {
		return ScopeUnknown
	}
	for _, d := range entry.Detail {
		if strings.Contains(d, want) {
			return ScopeProject
		}
	}
	return ScopeUnknown
}

// BuildOpenCodeResults turns one `opencode mcp list` run into Results.
func BuildOpenCodeResults(listOut, clientVersion string, checkedAt time.Time, listEvidence Evidence, projectServers map[string]config.Server) []Result {
	entries := parseOpenCodeList(listOut)
	results := make([]Result, 0, len(entries))
	for _, e := range entries {
		configState, conn, checkState := classifyOpenCodeStatus(e.StatusText)
		res := Result{
			ServerName:    e.Name,
			Client:        "opencode",
			ClientVersion: clientVersion,
			ConfigState:   configState,
			Connection:    conn,
			AuthState:     AuthUnknown,
			AuthMethod:    AuthMethodUnknown,
			CheckState:    checkState,
			Scope:         opencodeScope(e, projectServers),
			CheckedAt:     checkedAt,
			Evidence:      []Evidence{listEvidence},
		}
		if len(e.Detail) > 0 {
			res.Evidence = append(res.Evidence, Evidence{Summary: strings.Join(e.Detail, " | ")})
		}
		results = append(results, res)
	}
	return results
}
