package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdfranz/mcpctl/internal/config"
)

func (m Model) renderPreview() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Preview: "+m.previewAction) + "\n\n")

	plans := []struct {
		name string
		cp   *config.ClientPlan
	}{
		{"claude", m.plan.Claude},
		{"opencode", m.plan.OpenCode},
		{"codex", m.plan.Codex},
	}
	anyChange := false
	for _, p := range plans {
		if !p.cp.Changed {
			fmt.Fprintf(&b, "%s: %s\n\n", labelStyle.Render(p.name), dimStyle.Render("no change"))
			continue
		}
		anyChange = true
		verb := "will be updated"
		if !p.cp.Exists {
			verb = "will be created"
		}
		fmt.Fprintf(&b, "%s: %s\n", selectedStyle.Render(p.name), verb)
		for _, line := range diffLines(string(p.cp.OldRaw), string(p.cp.NewRaw)) {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\n")
	}

	if len(m.plan.Warnings) > 0 {
		b.WriteString(warnStyle.Render("Portability warnings:") + "\n")
		for _, w := range m.plan.Warnings {
			fmt.Fprintf(&b, "  - [%s] %s: %s\n", w.Client, w.ServerName, w.Detail)
		}
		b.WriteString("\n")
	}

	if !anyChange {
		b.WriteString(dimStyle.Render("Nothing to apply.") + "\n\n")
	}

	if m.applyErr != nil {
		fmt.Fprintf(&b, "%s\n\n", errorStyle.Render("apply failed: "+m.applyErr.Error()))
	}
	if m.applying {
		b.WriteString(dimStyle.Render("applying…") + "\n\n")
	}

	b.WriteString(helpStyle.Render("y/ctrl+s apply · n/esc cancel"))
	return b.String()
}

// diffLines renders a minimal unified-style diff between old and new by
// trimming their common leading and trailing lines and showing only the
// changed span with "-"/"+" markers. It's a readability aid for the
// typically small, single-server changes this preview shows, not a full
// diff algorithm.
func diffLines(oldText, newText string) []string {
	if oldText == newText {
		return nil
	}
	oldLines := splitNonEmptyLines(oldText)
	newLines := splitNonEmptyLines(newText)

	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		oldLines[len(oldLines)-1-suffix] == newLines[len(newLines)-1-suffix] {
		suffix++
	}

	var out []string
	for _, l := range oldLines[prefix : len(oldLines)-suffix] {
		out = append(out, failStyle.Render("- "+l))
	}
	for _, l := range newLines[prefix : len(newLines)-suffix] {
		out = append(out, okStyle.Render("+ "+l))
	}
	if len(out) == 0 {
		out = append(out, dimStyle.Render("(formatting/whitespace change only)"))
	}
	return out
}

func splitNonEmptyLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

func (m Model) updatePreview(msg tea.Msg) (Model, tea.Cmd) {
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
		m.plan = nil
		return m, nil
	case "y", "ctrl+s":
		if m.applying {
			return m, nil
		}
		m.applying = true
		m.applyErr = nil
		return m, applyCmd(m.plan)
	}
	return m, nil
}

type applyResultMsg struct {
	result *config.ApplyResult
	err    error
}

func applyCmd(plan *config.Plan) tea.Cmd {
	return func() tea.Msg {
		result, err := config.Apply(plan)
		return applyResultMsg{result: result, err: err}
	}
}
