package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdfranz/mcpctl/internal/config"
	"github.com/mdfranz/mcpctl/internal/status"
)

func keyMsg(name string) tea.KeyMsg {
	switch name {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	}
}

func testModel() Model {
	m := New("/tmp/project", 15*time.Second)
	m.loadingCfg = false
	m.snap = &config.Snapshot{
		Dir: "/tmp/project",
		Claude: &config.ClaudeSnapshot{
			Servers: map[string]config.Server{
				"files": {Name: "files", Type: config.ServerTypeStdio, Command: "mcp-server-files",
					Env: []config.EnvVar{{Name: "API_KEY", Kind: config.EnvVarPassthrough}}},
			},
			Unsupported: map[string]string{},
		},
		OpenCode: &config.OpenCodeSnapshot{
			Servers:     map[string]config.Server{},
			Unsupported: map[string]string{},
		},
		Codex: &config.CodexSnapshot{
			Servers:     map[string]config.Server{},
			Unsupported: map[string]string{},
		},
	}
	m.names = m.snap.ServerNames()
	return m
}

func TestRenderList_BeforeStatusLoaded(t *testing.T) {
	m := testModel()
	m.loadingStat = true
	out := m.renderList()
	if !strings.Contains(out, "files") {
		t.Errorf("expected server name in list output:\n%s", out)
	}
	if !strings.Contains(out, "checking live status") {
		t.Errorf("expected loading indicator:\n%s", out)
	}
}

func TestRenderList_WithStatus(t *testing.T) {
	m := testModel()
	m.report = &status.Report{
		Claude: status.ClientReport{
			Results: []status.Result{
				{ServerName: "files", Client: "claude", ConfigState: status.ConfigPresent,
					Connection: status.ConnectionConnected, CheckState: status.CheckComplete},
			},
		},
	}
	out := m.renderList()
	if !strings.Contains(out, "connected") {
		t.Errorf("expected connected status in list output:\n%s", out)
	}
}

func TestRenderList_EmptyProject(t *testing.T) {
	m := testModel()
	m.snap.Claude.Servers = map[string]config.Server{}
	m.names = nil
	out := m.renderList()
	if !strings.Contains(out, "no MCP servers configured") {
		t.Errorf("expected empty-state message:\n%s", out)
	}
}

func TestRenderDetail_ShowsFieldsAndRedactsLiteralEnv(t *testing.T) {
	m := testModel()
	m.snap.Claude.Servers["files"] = config.Server{
		Name: "files", Type: config.ServerTypeStdio, Command: "mcp-server-files",
		Env: []config.EnvVar{
			{Name: "API_KEY", Kind: config.EnvVarPassthrough},
			{Name: "SECRET_LITERAL", Kind: config.EnvVarLiteral, Value: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}
	m.cursor = 0
	out := m.renderDetail()
	if !strings.Contains(out, "command: mcp-server-files") {
		t.Errorf("expected command in detail output:\n%s", out)
	}
	if strings.Contains(out, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Errorf("literal env value leaked unredacted:\n%s", out)
	}
	if !strings.Contains(out, "not configured for this client") {
		t.Errorf("expected opencode/codex to show as not configured:\n%s", out)
	}
}

func TestRenderDetail_NoServersDoesNotPanic(t *testing.T) {
	m := testModel()
	m.names = nil
	out := m.renderDetail()
	if out == "" {
		t.Errorf("expected non-empty fallback output")
	}
}

func TestHandleKey_Navigation(t *testing.T) {
	m := testModel()
	m.snap.Claude.Servers["other"] = config.Server{Name: "other", Type: config.ServerTypeStdio, Command: "x"}
	m.names = m.snap.ServerNames()
	if len(m.names) != 2 {
		t.Fatalf("names = %v", m.names)
	}

	updated, _ := m.handleKey(keyMsg("down"))
	m2 := updated.(Model)
	if m2.cursor != 1 {
		t.Errorf("cursor after down = %d, want 1", m2.cursor)
	}

	updated, _ = m2.handleKey(keyMsg("down"))
	m3 := updated.(Model)
	if m3.cursor != 1 {
		t.Errorf("cursor should clamp at last index, got %d", m3.cursor)
	}

	updated, _ = m3.handleKey(keyMsg("enter"))
	m4 := updated.(Model)
	if m4.view != viewDetail {
		t.Errorf("expected enter to switch to detail view")
	}

	updated, _ = m4.handleKey(keyMsg("esc"))
	m5 := updated.(Model)
	if m5.view != viewList {
		t.Errorf("expected esc to return to list view")
	}
}
