package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mdfranz/mcpctl/internal/config"
)

func (m Model) conflictFor(name string) []string {
	var clients []string
	var first config.Server
	set := false
	divergent := false
	for _, clientName := range clientOrder {
		server, ok := m.fileServer(clientName, name)
		if !ok {
			continue
		}
		if !set {
			first, set = server, true
		} else if !serversEqualForTUI(first, server) {
			divergent = true
		}
		clients = append(clients, clientName)
	}
	if len(clients) < 2 || !divergent {
		return nil
	}
	return clients
}

func serversEqualForTUI(a, b config.Server) bool {
	if a.Type != b.Type || a.Command != b.Command || a.URL != b.URL || a.BearerTokenEnvVar != b.BearerTokenEnvVar || a.Transport != b.Transport {
		return false
	}
	if len(a.Args) != len(b.Args) || len(a.Env) != len(b.Env) || len(a.Headers) != len(b.Headers) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	for i := range a.Env {
		if a.Env[i].Name != b.Env[i].Name || a.Env[i].Kind != b.Env[i].Kind || a.Env[i].Value != b.Env[i].Value {
			return false
		}
		if (a.Env[i].Default == nil) != (b.Env[i].Default == nil) {
			return false
		}
		if a.Env[i].Default != nil && *a.Env[i].Default != *b.Env[i].Default {
			return false
		}
	}
	for name, value := range a.Headers {
		if b.Headers[name] != value {
			return false
		}
	}
	return true
}

func (m Model) renderConflict() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", headerStyle.Render("Choose canonical source for "+m.conflictName))
	for i, clientName := range m.conflictClients {
		prefix := "  "
		if i == m.conflictCursor {
			prefix = "> "
		}
		server, _ := m.fileServer(clientName, m.conflictName)
		detail := server.Command
		if server.Type == config.ServerTypeRemote {
			detail = server.URL
		}
		fmt.Fprintf(&b, "%s%s: %s\n", prefix, clientName, detail)
	}
	b.WriteString("\n" + helpStyle.Render("↑/↓ choose · enter edit from source · esc cancel"))
	return b.String()
}

func (m Model) updateConflict(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc", "q":
		m.view = viewList
	case "up", "k":
		if m.conflictCursor > 0 {
			m.conflictCursor--
		}
	case "down", "j":
		if m.conflictCursor+1 < len(m.conflictClients) {
			m.conflictCursor++
		}
	case "enter":
		clientName := m.conflictClients[m.conflictCursor]
		server, ok := m.fileServer(clientName, m.conflictName)
		if !ok {
			return m, nil
		}
		m.form = newEditForm(m.conflictName, server)
		m.view = viewForm
	}
	return m, nil
}
