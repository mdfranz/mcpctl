package status

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdfranz/mcpctl/internal/config"
)

// codexTransport mirrors the fields observed in `codex mcp list --json` /
// `codex mcp get <name> --json` output (codex-cli 0.155.1). Unknown
// fields are ignored by encoding/json, so this only needs what status
// actually uses.
type codexTransport struct {
	Type              string            `json:"type"`
	Command           string            `json:"command,omitempty"`
	Args              []string          `json:"args,omitempty"`
	URL               string            `json:"url,omitempty"`
	BearerTokenEnvVar *string           `json:"bearer_token_env_var,omitempty"`
	HTTPHeaders       map[string]string `json:"http_headers,omitempty"`
	EnvHTTPHeaders    map[string]string `json:"env_http_headers,omitempty"`
}

type codexServerEntry struct {
	Name           string         `json:"name"`
	Enabled        bool           `json:"enabled"`
	DisabledReason *string        `json:"disabled_reason"`
	Transport      codexTransport `json:"transport"`
	// AuthStatus is present on `list` entries (observed: "unsupported"
	// for stdio, "unknown" for a remote server with no bearer token
	// configured) but absent on `get` output.
	AuthStatus string `json:"auth_status"`
}

// parseCodexList parses `codex mcp list --json` output (a JSON array).
func parseCodexList(raw []byte) ([]codexServerEntry, error) {
	var entries []codexServerEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse codex mcp list --json: %w", err)
	}
	return entries, nil
}

// classifyCodexAuth maps codex's auth_status string to canonical states.
// Confirmed observed values: "unsupported" (no OAuth concept applies to
// this server) and "unknown" (codex has no evidence either way).
// Anything else is passed through as AuthUnknown rather than guessed.
func classifyCodexAuth(authStatus string, bearerEnvVar *string) (AuthState, AuthMethod) {
	if bearerEnvVar != nil && *bearerEnvVar != "" {
		// A configured bearer env var name is evidence of the auth
		// method, not proof the credential is valid, so AuthState is
		// still driven by authStatus below.
		switch authStatus {
		case "unsupported":
			return AuthNotRequired, AuthMethodBearer
		default:
			return AuthUnknown, AuthMethodBearer
		}
	}
	switch authStatus {
	case "unsupported":
		return AuthNotRequired, AuthMethodNone
	default:
		return AuthUnknown, AuthMethodUnknown
	}
}

// codexScope reports whether entry appears to be this project's own
// definition. codex mcp list/get gives no scope/origin field at all, so
// this is inferred by comparing the reported command/url against the
// project's own .codex/config.toml, and is only ever a best-effort
// attribution, never a claim the client itself confirmed.
func codexScope(entry codexServerEntry, projectServers map[string]config.Server) Scope {
	proj, ok := projectServers[entry.Name]
	if !ok {
		return ScopeOther
	}
	switch entry.Transport.Type {
	case "stdio":
		if proj.Type == config.ServerTypeStdio && proj.Command == entry.Transport.Command {
			return ScopeProject
		}
	case "streamable_http", "http", "sse":
		if proj.Type == config.ServerTypeRemote && proj.URL == entry.Transport.URL {
			return ScopeProject
		}
	}
	return ScopeOther
}

// BuildCodexResults turns one `codex mcp list --json` run into Results.
// projectServers is the project's own canonical Codex server set (from
// config.LoadCodex), used only to infer Scope since codex's own output
// carries no origin information. Codex's list/get commands report
// configuration, not live connectivity, so Connection is always
// ConnectionUnchecked here.
func BuildCodexResults(listJSON []byte, clientVersion string, checkedAt time.Time, listEvidence Evidence, projectServers map[string]config.Server) ([]Result, error) {
	entries, err := parseCodexList(listJSON)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(entries))
	for _, e := range entries {
		configState := ConfigPresent
		if !e.Enabled {
			configState = ConfigDisabled
		}
		authState, authMethod := classifyCodexAuth(e.AuthStatus, e.Transport.BearerTokenEnvVar)

		// listEvidence's raw Summary is the whole `codex mcp list --json`
		// array; that's not a useful per-server diagnostic, so each
		// result gets its own compact summary of just its entry instead,
		// while keeping listEvidence's Command/ExitCode/Duration.
		entryEvidence := listEvidence
		entryEvidence.Summary = fmt.Sprintf("enabled=%v transport=%s auth_status=%s", e.Enabled, e.Transport.Type, e.AuthStatus)
		if e.DisabledReason != nil && *e.DisabledReason != "" {
			entryEvidence.Summary += fmt.Sprintf(" disabled_reason=%q", *e.DisabledReason)
		}

		results = append(results, Result{
			ServerName:    e.Name,
			Client:        "codex",
			ClientVersion: clientVersion,
			Target:        e.Transport.URL,
			ConfigState:   configState,
			Connection:    ConnectionUnchecked,
			AuthState:     authState,
			AuthMethod:    authMethod,
			CheckState:    CheckComplete,
			Scope:         codexScope(e, projectServers),
			CheckedAt:     checkedAt,
			Evidence:      []Evidence{entryEvidence},
		})
	}
	return results, nil
}
