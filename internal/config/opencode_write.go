package config

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// renderOpenCodeDoc returns the new opencode.json bytes for snap after
// upserting each server in upsert and deleting each name in deletes.
// Servers not mentioned keep their original raw JSON verbatim
// (semantically; the whole document is re-marshaled with indentation).
func renderOpenCodeDoc(snap *OpenCodeSnapshot, upsert map[string]Server, deletes []string) ([]byte, []PortabilityWarning, error) {
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
		entry, w, err := renderOpenCodeServerEntry(name, srv, snap.Extras[name])
		if err != nil {
			return nil, nil, fmt.Errorf("render opencode entry for %s: %w", name, err)
		}
		servers[name] = entry
		warnings = append(warnings, w...)
	}

	top := make(map[string]json.RawMessage, len(snap.OtherTopLevel)+1)
	for k, v := range snap.OtherTopLevel {
		top[k] = v
	}
	serversJSON, err := json.Marshal(servers)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal mcp: %w", err)
	}
	top["mcp"] = serversJSON

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(top); err != nil {
		return nil, nil, fmt.Errorf("marshal opencode.json: %w", err)
	}
	return buf.Bytes(), warnings, nil
}

func renderOpenCodeServerEntry(name string, srv Server, extras map[string]json.RawMessage) (json.RawMessage, []PortabilityWarning, error) {
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

	var warnings []PortabilityWarning

	switch srv.Type {
	case ServerTypeStdio:
		delete(fields, "url")
		delete(fields, "headers")

		if err := set("type", "local"); err != nil {
			return nil, nil, err
		}
		command := append([]string{srv.Command}, srv.Args...)
		if err := set("command", command); err != nil {
			return nil, nil, err
		}
		if len(srv.Env) > 0 {
			env := make(map[string]string, len(srv.Env))
			for _, ev := range srv.Env {
				env[ev.Name] = renderOpenCodeEnvValue(ev)
				if ev.Kind == EnvVarPassthrough && ev.Default != nil {
					warnings = append(warnings, PortabilityWarning{
						ServerName: name,
						Client:     ClientOpenCode,
						Detail:     fmt.Sprintf("env %s has a default value that OpenCode cannot represent; only the passthrough reference is written", ev.Name),
					})
				}
			}
			if err := set("environment", env); err != nil {
				return nil, nil, err
			}
		} else {
			delete(fields, "environment")
		}

	case ServerTypeRemote:
		delete(fields, "command")
		delete(fields, "environment")

		if err := set("type", "remote"); err != nil {
			return nil, nil, err
		}
		if err := set("url", srv.URL); err != nil {
			return nil, nil, err
		}
		if len(srv.Headers) > 0 {
			headers := make(map[string]string, len(srv.Headers))
			for hname, h := range srv.Headers {
				headers[hname] = renderOpenCodeHeaderValue(h)
			}
			if err := set("headers", headers); err != nil {
				return nil, nil, err
			}
		} else {
			delete(fields, "headers")
		}
		if srv.BearerTokenEnvVar != "" {
			warnings = append(warnings, PortabilityWarning{
				ServerName: name,
				Client:     ClientOpenCode,
				Detail:     "bearer_token_env_var has no OpenCode equivalent; express it as an Authorization header to carry it over",
			})
		}

	default:
		return nil, nil, fmt.Errorf("unknown server type %q", srv.Type)
	}

	b, err := json.Marshal(fields)
	if err != nil {
		return nil, nil, err
	}
	return b, warnings, nil
}
