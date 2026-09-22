package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// CodexConfigDir and CodexConfigFile locate the project-scoped Codex
// config: <dir>/.codex/config.toml.
const (
	CodexConfigDir  = ".codex"
	CodexConfigFile = "config.toml"
)

// CodexSnapshot is the parsed state of a project's .codex/config.toml.
//
// This loader decodes into plain Go values (map[string]interface{}) for
// the MVP's read path. It does not yet preserve byte-exact formatting or
// comments for round-tripping; surgical, span-based editing that
// preserves untouched content is implemented separately (codex_block.go)
// and used for writes.
type CodexSnapshot struct {
	Path   string
	Exists bool
	Raw    []byte
	Hash   string

	Servers map[string]Server
	// Extras holds, per server, the table's fields not modeled by Server
	// (timeouts, OAuth options, enabled state, tool filters, ...).
	Extras      map[string]map[string]interface{}
	Unsupported map[string]string
}

// LoadCodex reads and parses dir/.codex/config.toml. A missing file is
// not an error: Exists is false and Servers is empty.
func LoadCodex(dir string) (*CodexSnapshot, error) {
	return LoadCodexFile(filepath.Join(dir, CodexConfigDir, CodexConfigFile))
}

// LoadCodexFile reads a Codex config from an explicit path. It is used for
// read-only attribution of user-level configuration.
func LoadCodexFile(path string) (*CodexSnapshot, error) {
	snap := &CodexSnapshot{
		Path:        path,
		Servers:     map[string]Server{},
		Extras:      map[string]map[string]interface{}{},
		Unsupported: map[string]string{},
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

	var doc map[string]interface{}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	serversRaw, ok := doc["mcp_servers"]
	if !ok {
		return snap, nil
	}
	servers, ok := serversRaw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("parse %s: mcp_servers is not a table", path)
	}

	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry, ok := servers[name].(map[string]interface{})
		if !ok {
			snap.Unsupported[name] = "server entry is not a table"
			continue
		}
		srv, extras, err := parseCodexServer(name, entry)
		if err != nil {
			snap.Unsupported[name] = err.Error()
			continue
		}
		snap.Servers[name] = srv
		if len(extras) > 0 {
			snap.Extras[name] = extras
		}
	}

	return snap, nil
}

func parseCodexServer(name string, entry map[string]interface{}) (Server, map[string]interface{}, error) {
	srv := Server{Name: name}
	extras := make(map[string]interface{}, len(entry))
	for k, v := range entry {
		extras[k] = v
	}

	command, hasCommand := stringField(entry, "command")
	url, hasURL := stringField(entry, "url")

	switch {
	case hasCommand:
		srv.Type = ServerTypeStdio
		srv.Command = command
		delete(extras, "command")

		if argsRaw, ok := entry["args"]; ok {
			args, err := stringSlice(argsRaw)
			if err != nil {
				return Server{}, nil, fmt.Errorf("args: %w", err)
			}
			srv.Args = args
			delete(extras, "args")
		}

		env, err := parseCodexEnv(entry)
		if err != nil {
			return Server{}, nil, err
		}
		srv.Env = env
		delete(extras, "env")
		delete(extras, "env_vars")

	case hasURL:
		srv.Type = ServerTypeRemote
		srv.URL = url
		delete(extras, "url")

		headers, err := parseCodexHeaders(entry)
		if err != nil {
			return Server{}, nil, err
		}
		srv.Headers = headers
		delete(extras, "http_headers")
		delete(extras, "env_http_headers")

		if bt, ok := stringField(entry, "bearer_token_env_var"); ok {
			if hasHeaderCI(headers, "Authorization") {
				return Server{}, nil, fmt.Errorf("both an Authorization header and bearer_token_env_var are set")
			}
			srv.BearerTokenEnvVar = bt
			delete(extras, "bearer_token_env_var")
		}

	default:
		return Server{}, nil, fmt.Errorf("entry has neither command nor url")
	}

	return srv, extras, nil
}

// parseCodexEnv reads literal values from the `env` table and passthrough
// names from the `env_vars` array. Codex has no syntax for a passthrough
// default; that limitation surfaces as a portability warning on save, not
// here at load time.
func parseCodexEnv(entry map[string]interface{}) ([]EnvVar, error) {
	var out []EnvVar
	seen := map[string]bool{}

	if envRaw, ok := entry["env"]; ok {
		envMap, ok := envRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("env: expected table")
		}
		names := make([]string, 0, len(envMap))
		for k := range envMap {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, name := range names {
			s, ok := envMap[name].(string)
			if !ok {
				return nil, fmt.Errorf("env.%s: expected string", name)
			}
			out = append(out, EnvVar{Name: name, Kind: EnvVarLiteral, Value: s})
			seen[name] = true
		}
	}

	if varsRaw, ok := entry["env_vars"]; ok {
		names, err := stringSlice(varsRaw)
		if err != nil {
			return nil, fmt.Errorf("env_vars: %w", err)
		}
		sorted := append([]string(nil), names...)
		sort.Strings(sorted)
		for _, name := range sorted {
			if seen[name] {
				return nil, fmt.Errorf("env var %s declared both literally and as passthrough", name)
			}
			out = append(out, EnvVar{Name: name, Kind: EnvVarPassthrough})
			seen[name] = true
		}
	}

	return out, nil
}

func parseCodexHeaders(entry map[string]interface{}) (map[string]HeaderValue, error) {
	out := map[string]HeaderValue{}

	if hRaw, ok := entry["http_headers"]; ok {
		hMap, ok := hRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("http_headers: expected table")
		}
		for name, v := range hMap {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("http_headers.%s: expected string", name)
			}
			out[name] = HeaderValue{Kind: EnvVarLiteral, Value: s}
		}
	}

	if eRaw, ok := entry["env_http_headers"]; ok {
		eMap, ok := eRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("env_http_headers: expected table")
		}
		for name, v := range eMap {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("env_http_headers.%s: expected string", name)
			}
			if _, exists := out[name]; exists {
				return nil, fmt.Errorf("header %s declared both literally and as passthrough", name)
			}
			out[name] = HeaderValue{Kind: EnvVarPassthrough, EnvName: s}
		}
	}

	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func hasHeaderCI(headers map[string]HeaderValue, name string) bool {
	for k := range headers {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func stringField(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func stringSlice(v interface{}) ([]string, error) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("expected array")
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("expected string element")
		}
		out = append(out, s)
	}
	return out, nil
}
