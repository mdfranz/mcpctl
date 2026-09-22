package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// renderClaudeDoc returns the new .mcp.json bytes for snap after upserting
// each server in upsert and deleting each name in deletes. Servers not
// mentioned keep their original raw JSON verbatim (semantically, not
// necessarily byte-for-byte: the whole document is re-marshaled with
// indentation). Top-level keys besides mcpServers are preserved as-is.
func renderClaudeDoc(snap *ClaudeSnapshot, upsert map[string]Server, deletes []string) ([]byte, []PortabilityWarning, error) {
	del := make(map[string]bool, len(deletes))
	for _, n := range deletes {
		del[n] = true
	}

	servers := make(map[string]json.RawMessage, len(snap.RawServers)+len(upsert))
	for name, raw := range snap.RawServers {
		if del[name] {
			continue
		}
		if _, isUpsert := upsert[name]; isUpsert {
			continue
		}
		servers[name] = raw
	}

	var warnings []PortabilityWarning
	for name, srv := range upsert {
		entry, err := renderClaudeServerEntry(srv, snap.Extras[name])
		if err != nil {
			return nil, nil, fmt.Errorf("render claude entry for %s: %w", name, err)
		}
		servers[name] = entry
	}

	top := make(map[string]json.RawMessage, len(snap.OtherTopLevel)+1)
	for k, v := range snap.OtherTopLevel {
		top[k] = v
	}
	serversJSON, err := json.Marshal(servers)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal mcpServers: %w", err)
	}
	top["mcpServers"] = serversJSON

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(top); err != nil {
		return nil, nil, fmt.Errorf("marshal .mcp.json: %w", err)
	}
	return buf.Bytes(), warnings, nil
}

func renderClaudeServerEntry(srv Server, extras map[string]json.RawMessage) (json.RawMessage, error) {
	fields := make(map[string]json.RawMessage, len(extras)+4)
	for k, v := range extras {
		fields[k] = v
	}
	set := func(key string, v interface{}) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		fields[key] = b
		return nil
	}

	switch srv.Type {
	case ServerTypeStdio:
		delete(fields, "type")
		delete(fields, "url")
		delete(fields, "headers")

		if err := set("command", srv.Command); err != nil {
			return nil, err
		}
		if len(srv.Args) > 0 {
			if err := set("args", srv.Args); err != nil {
				return nil, err
			}
		} else {
			delete(fields, "args")
		}
		if len(srv.Env) > 0 {
			env := make(map[string]string, len(srv.Env))
			for _, ev := range srv.Env {
				env[ev.Name] = renderClaudeEnvValue(ev)
			}
			if err := set("env", env); err != nil {
				return nil, err
			}
		} else {
			delete(fields, "env")
		}

	case ServerTypeRemote:
		delete(fields, "command")
		delete(fields, "args")
		delete(fields, "env")

		if srv.Transport != "" {
			if err := set("type", srv.Transport); err != nil {
				return nil, err
			}
		} else {
			delete(fields, "type")
		}
		if err := set("url", srv.URL); err != nil {
			return nil, err
		}
		if len(srv.Headers) > 0 {
			headers := make(map[string]string, len(srv.Headers))
			for name, h := range srv.Headers {
				headers[name] = renderClaudeHeaderValue(h)
			}
			if err := set("headers", headers); err != nil {
				return nil, err
			}
		} else {
			delete(fields, "headers")
		}

	default:
		return nil, fmt.Errorf("unknown server type %q", srv.Type)
	}

	return json.Marshal(fields)
}
