package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mdfranz/mcpctl/internal/status"
)

func (m Model) renderList() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", headerStyle.Render(fmt.Sprintf("mcpctl — %s", m.dir)))
	if m.loadingStat {
		fmt.Fprintf(&b, "%s\n\n", dimStyle.Render("checking live status…"))
	} else if m.statusErr != nil {
		fmt.Fprintf(&b, "%s\n\n", errorStyle.Render("status check failed: "+m.statusErr.Error()))
	} else {
		b.WriteString("\n")
	}

	if len(m.names) == 0 {
		b.WriteString(dimStyle.Render("no MCP servers configured in this project") + "\n\n")
		b.WriteString(helpStyle.Render(helpLine()))
		return b.String()
	}

	nameWidth := len("SERVER")
	for _, n := range m.names {
		if len(n) > nameWidth {
			nameWidth = len(n)
		}
	}

	header := lipgloss.NewStyle().Bold(true).Render(
		padRight("SERVER", nameWidth) + "  " +
			padRight("CLAUDE", cellWidth) + "  " +
			padRight("OPENCODE", cellWidth) + "  " +
			padRight("CODEX", cellWidth),
	)
	b.WriteString(header + "\n")

	for i, name := range m.names {
		row := padRight(name, nameWidth)
		for _, c := range clientOrder {
			row += "  " + padRight(m.cellFor(c, name), cellWidth)
		}
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("> "+row) + "\n")
		} else {
			b.WriteString("  " + row + "\n")
		}
	}

	b.WriteString("\n" + helpStyle.Render(helpLine()))
	return b.String()
}

const cellWidth = 16

func helpLine() string {
	return "↑/↓ move · enter detail · (a)dd · (e)dit · (d)elete · (l)ogin · (r)eload · refre(s)h · (q)uit"
}

func padRight(s string, width int) string {
	visLen := lipgloss.Width(s)
	if visLen >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visLen)
}

// cellFor renders a compact, colored status cell for one server/client
// pair. Before a status.Report has loaded, it falls back to raw config
// presence so the list isn't blank while the live check runs.
func (m Model) cellFor(clientName, serverName string) string {
	if r, ok := m.resultFor(clientName, serverName); ok {
		return formatResultCell(r)
	}
	if m.report != nil {
		// A report loaded and simply has nothing for this server/client:
		// genuinely not configured there.
		return dimStyle.Render("·")
	}
	if _, present := m.fileServer(clientName, serverName); present {
		return dimStyle.Render("…")
	}
	return dimStyle.Render("·")
}

func formatResultCell(r status.Result) string {
	if r.CheckState != status.CheckComplete {
		return unknownStyle.Render(string(r.CheckState))
	}
	switch r.ConfigState {
	case status.ConfigPendingApproval:
		return warnStyle.Render("⏸ pending")
	case status.ConfigRejected:
		return failStyle.Render("✘ rejected")
	case status.ConfigDisabled:
		return unknownStyle.Render("− disabled")
	}
	switch r.Connection {
	case status.ConnectionConnected:
		return okStyle.Render("✓ connected")
	case status.ConnectionFailed:
		return failStyle.Render("✘ failed")
	case status.ConnectionTimedOut:
		return failStyle.Render("⏱ timeout")
	}
	return unknownStyle.Render("· present")
}
