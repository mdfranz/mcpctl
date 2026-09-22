package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdfranz/mcpctl/internal/config"
)

func (m Model) renderConfirmDelete() string {
	return fmt.Sprintf("%s\n\n%s\n\n%s",
		headerStyle.Render("Delete "+m.pendingDelete+"?"),
		dimStyle.Render("This removes it from every client's project config that currently defines it."),
		helpStyle.Render("y confirm · n/esc cancel"))
}

func (m Model) updateConfirmDelete(msg tea.Msg) (Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc", "n":
		m.view = viewList
		m.pendingDelete = ""
		return m, nil
	case "y":
		plan, err := config.BuildPlan(m.dir, nil, []string{m.pendingDelete})
		if err != nil {
			m.view = viewList
			m.pendingDelete = ""
			m.configErr = err
			return m, nil
		}
		m.plan = plan
		m.previewAction = "delete " + m.pendingDelete
		m.applyErr = nil
		m.pendingDelete = ""
		m.view = viewPreview
		return m, nil
	}
	return m, nil
}
