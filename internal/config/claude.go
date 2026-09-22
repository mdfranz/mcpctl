package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ClaudeConfigFile is the project-scoped Claude Code MCP config file name.
const ClaudeConfigFile = ".mcp.json"

// ClaudeSnapshot is the parsed state of a project's .mcp.json.
type ClaudeSnapshot struct {
	Path   string
	Exists bool
	Raw    []byte
	Hash   string

	// Servers holds the canonical projection of each entry under
	// mcpServers. Entries this adapter cannot represent are omitted here
	// and reported in Unsupported instead.
	Servers map[string]Server

	// Extras holds, per server, the raw JSON of fields not modeled by
	// Server (e.g. client-specific options). Never copied to other clients.
	Extras map[string]map[string]json.RawMessage

	// OtherTopLevel holds top-level keys besides mcpServers, preserved
	// verbatim on write.
	OtherTopLevel map[string]json.RawMessage

	// RawServers holds every entry under mcpServers exactly as read,
	// including ones reported in Unsupported. A write that doesn't touch
	// a given server re-emits this raw entry rather than a re-marshaled
	// reconstruction, so unloadable entries are never silently dropped.
	RawServers map[string]json.RawMessage

	// Unsupported maps a server name to why it could not be loaded into
	// the canonical model. Saving must not silently drop these.
	Unsupported map[string]string
}

type rawClaudeServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// LoadClaude reads and parses dir/.mcp.json. A missing file is not an
// error: Exists is false and Servers is empty.
func LoadClaude(dir string) (*ClaudeSnapshot, error) {
	path := filepath.Join(dir, ClaudeConfigFile)
	snap := &ClaudeSnapshot{
		Path:          path,
		Servers:       map[string]Server{},
		Extras:        map[string]map[string]json.RawMessage{},
		OtherTopLevel: map[string]json.RawMessage{},
		RawServers:    map[string]json.RawMessage{},
		Unsupported:   map[string]string{},
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return snap, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	snap.Exists = true
	snap.Raw = raw
	sum := sha256.Sum256(raw)
	snap.Hash = hex.EncodeToString(sum[:])

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	serversRaw, ok := top["mcpServers"]
	for k, v := range top {
		if k == "mcpServers" {
			continue
		}
		snap.OtherTopLevel[k] = v
	}
	if !ok {
		return snap, nil
	}

	var entries map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &entries); err != nil {
		return nil, fmt.Errorf("parse %s mcpServers: %w", path, err)
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entryRaw := entries[name]
		snap.RawServers[name] = entryRaw

		var rs rawClaudeServer
		if err := json.Unmarshal(entryRaw, &rs); err != nil {
			snap.Unsupported[name] = fmt.Sprintf("malformed server entry: %v", err)
			continue
		}

		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entryRaw, &fields); err != nil {
			snap.Unsupported[name] = fmt.Sprintf("malformed server entry: %v", err)
			continue
		}
		known := []string{"type", "command", "args", "env", "url", "headers"}
		for _, k := range known {
			delete(fields, k)
		}
		if len(fields) > 0 {
			snap.Extras[name] = fields
		}

		srv := Server{Name: name}
		switch {
		case rs.Command != "":
			srv.Type = ServerTypeStdio
			srv.Command = rs.Command
			srv.Args = rs.Args
			env, err := parseClaudeEnv(rs.Env)
			if err != nil {
				snap.Unsupported[name] = err.Error()
				continue
			}
			srv.Env = env
		case rs.URL != "":
			srv.Type = ServerTypeRemote
			srv.URL = rs.URL
			srv.Transport = rs.Type
			headers, err := parseClaudeHeaders(rs.Headers)
			if err != nil {
				snap.Unsupported[name] = err.Error()
				continue
			}
			srv.Headers = headers
		default:
			snap.Unsupported[name] = "entry has neither command nor url"
			continue
		}
		snap.Servers[name] = srv
	}

	return snap, nil
}

// parseClaudeEnv converts a raw env map into canonical EnvVars. Passthrough
// values whose referenced OS env var name differs from the server's own
// env var name (e.g. "FOO": "${BAR}") can't be represented by the
// canonical model and are reported as an error rather than silently
// coerced or dropped.
func parseClaudeEnv(m map[string]string) ([]EnvVar, error) {
	if len(m) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)

	out := make([]EnvVar, 0, len(names))
	for _, name := range names {
		kind, envName, def, literal := parseClaudeValue(m[name])
		if kind == EnvVarPassthrough && envName != name {
			return nil, fmt.Errorf("env %s references differently-named var ${%s}, which mcpctl cannot represent", name, envName)
		}
		ev := EnvVar{Name: name, Kind: kind}
		if kind == EnvVarPassthrough {
			ev.Default = def
		} else {
			ev.Value = literal
		}
		out = append(out, ev)
	}
	return out, nil
}

func parseClaudeHeaders(m map[string]string) (map[string]HeaderValue, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]HeaderValue, len(m))
	for name, v := range m {
		kind, envName, _, literal := parseClaudeValue(v)
		if kind == EnvVarPassthrough {
			out[name] = HeaderValue{Kind: EnvVarPassthrough, EnvName: envName}
		} else {
			out[name] = HeaderValue{Kind: EnvVarLiteral, Value: literal}
		}
	}
	return out, nil
}

func renderClaudeEnvValue(ev EnvVar) string {
	if ev.Kind == EnvVarLiteral {
		return ev.Value
	}
	return renderClaudeValue(ev.Kind, ev.Name, ev.Default, "")
}

func renderClaudeHeaderValue(h HeaderValue) string {
	if h.Kind == EnvVarLiteral {
		return h.Value
	}
	return renderClaudeValue(h.Kind, h.EnvName, nil, "")
}
