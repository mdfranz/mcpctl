// Package tui is mcpctl's interactive terminal UI: a project server list
// with per-client status columns, and a detail view with the full,
// independent status dimensions for one server.
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdfranz/mcpctl/internal/config"
	"github.com/mdfranz/mcpctl/internal/status"
)

type viewKind int

const (
	viewList viewKind = iota
	viewDetail
)

// clientOrder is the fixed column/section order used throughout the TUI.
var clientOrder = []string{"claude", "opencode", "codex"}

// Model is the Bubble Tea model for the whole app.
type Model struct {
	dir     string
	timeout time.Duration

	width, height int

	view viewKind

	snap       *config.Snapshot
	configErr  error
	loadingCfg bool

	report      *status.Report
	statusErr   error
	loadingStat bool

	names  []string
	cursor int

	quitting bool
}

// New builds the initial model for project directory dir. Call Run to
// actually start the program.
func New(dir string, timeout time.Duration) Model {
	return Model{dir: dir, timeout: timeout, loadingCfg: true}
}

// Run starts the TUI program and blocks until the user quits.
func Run(dir string, timeout time.Duration) error {
	_, err := tea.NewProgram(New(dir, timeout), tea.WithAltScreen()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return loadConfigCmd(m.dir)
}

type configLoadedMsg struct {
	snap *config.Snapshot
	err  error
}

type statusLoadedMsg struct {
	report *status.Report
	err    error
}

func loadConfigCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		snap, err := config.Load(dir)
		return configLoadedMsg{snap: snap, err: err}
	}
}

func loadStatusCmd(dir string, timeout time.Duration) tea.Cmd {
	return func() tea.Msg {
		report, err := status.Check(context.Background(), dir, timeout)
		return statusLoadedMsg{report: report, err: err}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case configLoadedMsg:
		m.loadingCfg = false
		m.snap = msg.snap
		m.configErr = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.names = msg.snap.ServerNames()
		if m.cursor >= len(m.names) {
			m.cursor = 0
		}
		m.loadingStat = true
		return m, loadStatusCmd(m.dir, m.timeout)

	case statusLoadedMsg:
		m.loadingStat = false
		m.report = msg.report
		m.statusErr = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		m.quitting = true
		return m, tea.Quit
	}

	switch m.view {
	case viewDetail:
		switch key {
		case "esc", "q", "backspace":
			m.view = viewList
		}
		return m, nil
	}

	switch key {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.names)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.names) > 0 {
			m.view = viewDetail
		}
	case "r":
		m.loadingCfg = true
		return m, loadConfigCmd(m.dir)
	case "s":
		if m.snap != nil {
			m.loadingStat = true
			return m, loadStatusCmd(m.dir, m.timeout)
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.configErr != nil {
		return errorStyle.Render(fmt.Sprintf("error loading %s: %v", m.dir, m.configErr)) + "\n" + helpStyle.Render("q to quit")
	}
	if m.loadingCfg || m.snap == nil {
		return "loading project config…"
	}

	switch m.view {
	case viewDetail:
		return m.renderDetail()
	default:
		return m.renderList()
	}
}

// resultsFor returns clientName's Results from the current report, or
// nil if no report has loaded yet.
func (m Model) resultsFor(clientName string) []status.Result {
	if m.report == nil {
		return nil
	}
	switch clientName {
	case "claude":
		return m.report.Claude.Results
	case "opencode":
		return m.report.OpenCode.Results
	case "codex":
		return m.report.Codex.Results
	}
	return nil
}

// resultFor finds serverName's Result within clientName's Results, if any.
func (m Model) resultFor(clientName, serverName string) (status.Result, bool) {
	for _, r := range m.resultsFor(clientName) {
		if r.ServerName == serverName {
			return r, true
		}
	}
	return status.Result{}, false
}

func (m Model) fileServer(clientName, serverName string) (config.Server, bool) {
	switch clientName {
	case "claude":
		srv, ok := m.snap.Claude.Servers[serverName]
		return srv, ok
	case "opencode":
		srv, ok := m.snap.OpenCode.Servers[serverName]
		return srv, ok
	case "codex":
		srv, ok := m.snap.Codex.Servers[serverName]
		return srv, ok
	}
	return config.Server{}, false
}
