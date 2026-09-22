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

// OpenCodeConfigFile is the project-scoped OpenCode MCP config file name.
const OpenCodeConfigFile = "opencode.json"

// OpenCodeSnapshot is the parsed state of a project's opencode.json.
type OpenCodeSnapshot struct {
	Path   string
	Exists bool
	Raw    []byte
	Hash   string

	Servers       map[string]Server
	Extras        map[string]map[string]json.RawMessage
	OtherTopLevel map[string]json.RawMessage

	// RawServers holds every entry under mcp exactly as read, including
	// ones reported in Unsupported. A write that doesn't touch a given
	// server re-emits this raw entry rather than a re-marshaled
	// reconstruction, so unloadable entries are never silently dropped.
	RawServers map[string]json.RawMessage

	Unsupported map[string]string
}

type rawOpenCodeServer struct {
	Type        string            `json:"type,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// LoadOpenCode reads and parses dir/opencode.json. A missing file is not
// an error: Exists is false and Servers is empty.
func LoadOpenCode(dir string) (*OpenCodeSnapshot, error) {
	path := filepath.Join(dir, OpenCodeConfigFile)
	snap := &OpenCodeSnapshot{
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

	serversRaw, ok := top["mcp"]
	for k, v := range top {
		if k == "mcp" {
			continue
		}
		snap.OtherTopLevel[k] = v
	}
	if !ok {
		return snap, nil
	}

	var entries map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &entries); err != nil {
		return nil, fmt.Errorf("parse %s mcp: %w", path, err)
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entryRaw := entries[name]
		snap.RawServers[name] = entryRaw

		var rs rawOpenCodeServer
		if err := json.Unmarshal(entryRaw, &rs); err != nil {
			snap.Unsupported[name] = fmt.Sprintf("malformed server entry: %v", err)
			continue
		}

		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entryRaw, &fields); err != nil {
			snap.Unsupported[name] = fmt.Sprintf("malformed server entry: %v", err)
			continue
		}
		known := []string{"type", "command", "environment", "url", "headers"}
		for _, k := range known {
			delete(fields, k)
		}
		if len(fields) > 0 {
			snap.Extras[name] = fields
		}

		srv := Server{Name: name}
		switch rs.Type {
		case "local", "":
			if len(rs.Command) == 0 {
				snap.Unsupported[name] = "local server missing command"
				continue
			}
			srv.Type = ServerTypeStdio
			srv.Command = rs.Command[0]
			srv.Args = rs.Command[1:]
			env, err := parseOpenCodeEnv(rs.Environment)
			if err != nil {
				snap.Unsupported[name] = err.Error()
				continue
			}
			srv.Env = env
		case "remote":
			if rs.URL == "" {
				snap.Unsupported[name] = "remote server missing url"
				continue
			}
			srv.Type = ServerTypeRemote
			srv.URL = rs.URL
			headers, err := parseOpenCodeHeaders(rs.Headers)
			if err != nil {
				snap.Unsupported[name] = err.Error()
				continue
			}
			srv.Headers = headers
		default:
			snap.Unsupported[name] = fmt.Sprintf("unknown server type %q", rs.Type)
			continue
		}
		snap.Servers[name] = srv
	}

	return snap, nil
}

func parseOpenCodeEnv(m map[string]string) ([]EnvVar, error) {
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
		kind, envName, literal := parseOpenCodeValue(m[name])
		if kind == EnvVarPassthrough && envName != name {
			return nil, fmt.Errorf("environment %s references differently-named var {env:%s}, which mcpctl cannot represent", name, envName)
		}
		out = append(out, EnvVar{Name: name, Kind: kind, Value: literal})
	}
	return out, nil
}

func parseOpenCodeHeaders(m map[string]string) (map[string]HeaderValue, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]HeaderValue, len(m))
	for name, v := range m {
		kind, envName, literal := parseOpenCodeValue(v)
		if kind == EnvVarPassthrough {
			out[name] = HeaderValue{Kind: EnvVarPassthrough, EnvName: envName}
		} else {
			out[name] = HeaderValue{Kind: EnvVarLiteral, Value: literal}
		}
	}
	return out, nil
}

func renderOpenCodeEnvValue(ev EnvVar) string {
	if ev.Kind == EnvVarLiteral {
		return ev.Value
	}
	return renderOpenCodeValue(ev.Kind, ev.Name, "")
}

func renderOpenCodeHeaderValue(h HeaderValue) string {
	if h.Kind == EnvVarLiteral {
		return h.Value
	}
	return renderOpenCodeValue(h.Kind, h.EnvName, "")
}
