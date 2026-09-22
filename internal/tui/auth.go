package tui

import (
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) renderAuth() string {
	if len(m.authClients) == 0 {
		return "no client available for authentication\n\n" + helpLine()
	}
	text := fmt.Sprintf("Authenticate %s\n\n", m.authServer)
	for i, clientName := range m.authClients {
		prefix := "  "
		if i == m.authCursor {
			prefix = "> "
		}
		text += prefix + clientName + "\n"
	}
	if m.authErr != nil {
		text += "\n" + errorStyle.Render("authentication failed: "+m.authErr.Error()) + "\n"
	}
	text += "\n" + helpStyle.Render("↑/↓ choose · enter authenticate · esc cancel")
	return text
}

func (m Model) updateAuth(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "q":
		m.view = viewList
	case "up", "k":
		if m.authCursor > 0 {
			m.authCursor--
		}
	case "down", "j":
		if m.authCursor+1 < len(m.authClients) {
			m.authCursor++
		}
	case "enter":
		m.authClient = m.authClients[m.authCursor]
		m.authErr = nil
		return m, authCmd(m.dir, m.authClient, m.authServer)
	}
	return m, nil
}

func authCmd(dir, clientName, serverName string) tea.Cmd {
	return tea.ExecProcess(authProcess(dir, clientName, serverName), func(err error) tea.Msg {
		return authFinishedMsg{err: err}
	})
}

func authProcess(dir, clientName, serverName string) *exec.Cmd {
	args := map[string][]string{
		"claude":   {"mcp", "login", serverName},
		"codex":    {"mcp", "login", serverName},
		"opencode": {"mcp", "auth", serverName},
	}[clientName]
	cmd := exec.Command(clientName, args...)
	cmd.Dir = dir
	return cmd
}
