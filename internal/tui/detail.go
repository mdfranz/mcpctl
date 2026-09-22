package tui

import (
	"fmt"
	"strings"

	"github.com/mdfranz/mcpctl/internal/config"
	"github.com/mdfranz/mcpctl/internal/redact"
)

func (m Model) renderDetail() string {
	if m.cursor >= len(m.names) {
		return dimStyle.Render("no server selected") + "\n\n" + helpStyle.Render("esc back · q quit")
	}
	name := m.names[m.cursor]

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render(name))

	for _, c := range clientOrder {
		b.WriteString(labelStyle.Render(strings.ToUpper(c)) + "\n")

		srv, hasFile := m.fileServer(c, name)
		if !hasFile {
			b.WriteString("  " + dimStyle.Render("not configured for this client") + "\n\n")
			continue
		}
		writeServerFields(&b, srv)

		if r, ok := m.resultFor(c, name); ok {
			fmt.Fprintf(&b, "  config=%s  conn=%s  auth=%s (%s)\n",
				r.ConfigState, r.Connection, r.AuthState, r.AuthMethod)
			fmt.Fprintf(&b, "  check=%s  scope=%s", r.CheckState, r.Scope)
			if r.ClientVersion != "" {
				fmt.Fprintf(&b, "  version=%s", r.ClientVersion)
			}
			b.WriteString("\n")
			if len(r.Evidence) > 0 {
				if ev := r.Evidence[len(r.Evidence)-1]; ev.Summary != "" {
					fmt.Fprintf(&b, "  %s\n", dimStyle.Render(ev.Summary))
				}
			}
		} else if m.loadingStat {
			b.WriteString("  " + dimStyle.Render("checking…") + "\n")
		} else {
			b.WriteString("  " + dimStyle.Render("no live status available") + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("esc back · q quit"))
	return b.String()
}

// writeServerFields prints a server's canonical definition. Literal env
// values are redacted as a safety net: this is a display surface, not a
// place to echo back anything that might be a secret. Passthrough names
// and defaults are shown since they're identifiers/config, not secrets.
func writeServerFields(b *strings.Builder, srv config.Server) {
	switch srv.Type {
	case config.ServerTypeStdio:
		fmt.Fprintf(b, "  command: %s", srv.Command)
		if len(srv.Args) > 0 {
			fmt.Fprintf(b, " %s", strings.Join(srv.Args, " "))
		}
		b.WriteString("\n")
		if len(srv.Env) > 0 {
			b.WriteString("  env:\n")
			for _, ev := range srv.Env {
				b.WriteString("    " + formatEnvVar(ev) + "\n")
			}
		}
	case config.ServerTypeRemote:
		fmt.Fprintf(b, "  url: %s\n", srv.URL)
		if srv.BearerTokenEnvVar != "" {
			fmt.Fprintf(b, "  bearer_token_env_var: %s\n", srv.BearerTokenEnvVar)
		}
		if len(srv.Headers) > 0 {
			b.WriteString("  headers:\n")
			names := make([]string, 0, len(srv.Headers))
			for n := range srv.Headers {
				names = append(names, n)
			}
			for _, n := range names {
				h := srv.Headers[n]
				if h.Kind == config.EnvVarPassthrough {
					fmt.Fprintf(b, "    %s = $%s\n", n, h.EnvName)
				} else {
					fmt.Fprintf(b, "    %s = %s\n", n, redact.Summary(h.Value))
				}
			}
		}
	}
}

func formatEnvVar(ev config.EnvVar) string {
	if ev.Kind == config.EnvVarPassthrough {
		if ev.Default != nil {
			return fmt.Sprintf("%s = $%s (default %q)", ev.Name, ev.Name, *ev.Default)
		}
		return fmt.Sprintf("%s = $%s", ev.Name, ev.Name)
	}
	return fmt.Sprintf("%s = %s", ev.Name, redact.Summary(ev.Value))
}
