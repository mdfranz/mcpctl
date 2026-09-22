package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// PortabilityWarning flags canonical information that a target client's
// format cannot fully represent. Saving still proceeds; the loss is
// expected, not drift.
type PortabilityWarning struct {
	ServerName string
	Client     ClientKind
	Detail     string
}

// renderCodexServerBlock renders one server as standalone Codex TOML
// table text (starting with `[mcp_servers.<name>]`), suitable for
// ApplyCodexEdit's upsert map. extras (unmodeled fields such as
// timeouts, OAuth options, or tool filters) are merged in verbatim so
// they survive the rewrite, preserved semantically even though this
// rewrites the whole table's formatting rather than editing bytes
// in place.
func renderCodexServerBlock(name string, srv Server, extras map[string]interface{}) (string, []PortabilityWarning, error) {
	entry := make(map[string]interface{}, len(extras)+4)
	for k, v := range extras {
		entry[k] = v
	}
	var warnings []PortabilityWarning

	switch srv.Type {
	case ServerTypeStdio:
		entry["command"] = srv.Command
		if len(srv.Args) > 0 {
			entry["args"] = srv.Args
		} else {
			delete(entry, "args")
		}

		literalEnv := map[string]string{}
		var passthrough []string
		for _, ev := range srv.Env {
			if ev.Kind == EnvVarLiteral {
				literalEnv[ev.Name] = ev.Value
				continue
			}
			passthrough = append(passthrough, ev.Name)
			if ev.Default != nil {
				warnings = append(warnings, PortabilityWarning{
					ServerName: name,
					Client:     ClientCodex,
					Detail:     fmt.Sprintf("env %s has a default value that Codex cannot represent; only the passthrough reference is written", ev.Name),
				})
			}
		}
		if len(literalEnv) > 0 {
			entry["env"] = literalEnv
		} else {
			delete(entry, "env")
		}
		if len(passthrough) > 0 {
			sort.Strings(passthrough)
			entry["env_vars"] = passthrough
		} else {
			delete(entry, "env_vars")
		}
		delete(entry, "url")
		delete(entry, "http_headers")
		delete(entry, "env_http_headers")
		delete(entry, "bearer_token_env_var")

	case ServerTypeRemote:
		entry["url"] = srv.URL
		delete(entry, "command")
		delete(entry, "args")
		delete(entry, "env")
		delete(entry, "env_vars")

		literalHeaders := map[string]string{}
		envHeaders := map[string]string{}
		for hname, h := range srv.Headers {
			if h.Kind == EnvVarLiteral {
				literalHeaders[hname] = h.Value
			} else {
				envHeaders[hname] = h.EnvName
			}
		}
		if len(literalHeaders) > 0 {
			entry["http_headers"] = literalHeaders
		} else {
			delete(entry, "http_headers")
		}
		if len(envHeaders) > 0 {
			entry["env_http_headers"] = envHeaders
		} else {
			delete(entry, "env_http_headers")
		}
		if srv.BearerTokenEnvVar != "" {
			entry["bearer_token_env_var"] = srv.BearerTokenEnvVar
		} else {
			delete(entry, "bearer_token_env_var")
		}

	default:
		return "", nil, fmt.Errorf("unknown server type %q", srv.Type)
	}

	doc := map[string]interface{}{
		"mcp_servers": map[string]interface{}{name: entry},
	}
	b, err := toml.Marshal(doc)
	if err != nil {
		return "", nil, fmt.Errorf("render codex block for %s: %w", name, err)
	}
	// go-toml/v2 emits a redundant bare "[mcp_servers]" line ahead of the
	// dotted "[mcp_servers.<name>]" header even though the dotted header
	// implies its parent table on its own. Strip it so ApplyCodexEdit
	// never has to reconcile two independent renders both declaring that
	// same bare table.
	out := strings.TrimPrefix(string(b), "[mcp_servers]\n")
	return out, warnings, nil
}
