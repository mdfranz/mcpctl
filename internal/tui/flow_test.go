package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mdfranz/mcpctl/internal/config"
)

// drive feeds msg through Update, asserts the result is a Model, and
// returns it along with any follow-up Cmd (unexecuted).
func drive(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	mm, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update did not return a Model")
	}
	return mm, cmd
}

// runCmd executes cmd (as the Bubble Tea runtime would) and feeds its
// message back through Update, returning the new model and any further
// Cmd it produced.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected a non-nil Cmd")
	}
	return drive(t, m, cmd())
}

// loadInitial runs Init() and its configLoadedMsg -> statusLoadedMsg
// chain to completion, as the real runtime would on startup.
func loadInitial(t *testing.T, dir string) Model {
	t.Helper()
	m := New(dir, 10*time.Second)
	m, cmd := runCmd(t, m, m.Init())
	if cmd != nil {
		m, _ = runCmd(t, m, cmd)
	}
	return m
}

func writeFlowFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestTUIFlow_AddServerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	m := loadInitial(t, dir)
	if len(m.names) != 0 {
		t.Fatalf("expected an empty project, got %v", m.names)
	}

	m, _ = drive(t, m, keyMsg("a"))
	if m.view != viewForm {
		t.Fatalf("expected viewForm after 'a', got %v", m.view)
	}

	m.form.nameInput.SetValue("newsvc")
	m.form.next()
	m.form.commandInput.SetValue("my-command")
	m.form.next()
	m.form.argsInput.SetValue("--flag value")

	m, cmd := drive(t, m, keyMsg("ctrl+s"))
	if cmd != nil {
		t.Fatalf("submitForm should not itself return a Cmd")
	}
	if m.view != viewPreview {
		t.Fatalf("expected viewPreview, form error = %q", m.form.errMsg)
	}
	if !m.plan.Claude.Changed || !m.plan.OpenCode.Changed || !m.plan.Codex.Changed {
		t.Fatalf("expected all three clients to change: %+v", m.plan)
	}

	m, cmd = drive(t, m, keyMsg("y"))
	if cmd == nil {
		t.Fatalf("expected an apply Cmd")
	}
	m, cmd = runCmd(t, m, cmd)
	if m.applyErr != nil {
		t.Fatalf("apply failed: %v", m.applyErr)
	}
	if m.view != viewList {
		t.Fatalf("expected to land back on viewList, got %v", m.view)
	}
	if cmd != nil {
		m, cmd = runCmd(t, m, cmd) // configLoadedMsg
		if cmd != nil {
			m, _ = runCmd(t, m, cmd) // statusLoadedMsg
		}
	}

	if len(m.names) != 1 || m.names[0] != "newsvc" {
		t.Fatalf("names after reload = %v", m.names)
	}

	snap, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	srv, ok := snap.Claude.Servers["newsvc"]
	if !ok {
		t.Fatalf("newsvc not persisted to .mcp.json")
	}
	if srv.Command != "my-command" || len(srv.Args) != 2 || srv.Args[0] != "--flag" || srv.Args[1] != "value" {
		t.Errorf("persisted server = %+v", srv)
	}
	if _, ok := snap.OpenCode.Servers["newsvc"]; !ok {
		t.Errorf("newsvc not persisted to opencode.json")
	}
	if _, ok := snap.Codex.Servers["newsvc"]; !ok {
		t.Errorf("newsvc not persisted to .codex/config.toml")
	}
}

func TestTUIFlow_EditServerChangesCommand(t *testing.T) {
	dir := t.TempDir()
	writeFlowFixture(t, dir+"/.mcp.json", `{"mcpServers": {"svc": {"command": "old-command"}}}`)

	m := loadInitial(t, dir)
	if len(m.names) != 1 {
		t.Fatalf("names = %v", m.names)
	}

	m, _ = drive(t, m, keyMsg("e"))
	if m.view != viewForm {
		t.Fatalf("expected viewForm after 'e', got %v", m.view)
	}
	if m.form.commandInput.Value() != "old-command" {
		t.Fatalf("edit form not prefilled: %q", m.form.commandInput.Value())
	}
	m.form.commandInput.SetValue("new-command")

	m, _ = drive(t, m, keyMsg("ctrl+s"))
	if m.view != viewPreview {
		t.Fatalf("expected viewPreview, form error = %q", m.form.errMsg)
	}

	m, cmd := drive(t, m, keyMsg("y"))
	m, cmd = runCmd(t, m, cmd)
	if m.applyErr != nil {
		t.Fatalf("apply failed: %v", m.applyErr)
	}
	if cmd != nil {
		m, _ = runCmd(t, m, cmd)
	}

	snap, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if snap.Claude.Servers["svc"].Command != "new-command" {
		t.Errorf("command not updated: %+v", snap.Claude.Servers["svc"])
	}
}

func TestTUIFlow_DeleteServerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFlowFixture(t, dir+"/.mcp.json", `{"mcpServers": {"svc": {"command": "cmd"}}}`)

	m := loadInitial(t, dir)
	m, _ = drive(t, m, keyMsg("d"))
	if m.view != viewConfirmDelete {
		t.Fatalf("expected viewConfirmDelete after 'd', got %v", m.view)
	}
	if m.pendingDelete != "svc" {
		t.Fatalf("pendingDelete = %q", m.pendingDelete)
	}

	m, _ = drive(t, m, keyMsg("y"))
	if m.view != viewPreview {
		t.Fatalf("expected viewPreview after confirming delete, got %v", m.view)
	}

	m, cmd := drive(t, m, keyMsg("y"))
	m, cmd = runCmd(t, m, cmd)
	if m.applyErr != nil {
		t.Fatalf("apply failed: %v", m.applyErr)
	}
	if cmd != nil {
		m, _ = runCmd(t, m, cmd)
	}

	snap, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if _, ok := snap.Claude.Servers["svc"]; ok {
		t.Errorf("svc should have been deleted")
	}
}

func TestTUIFlow_DeleteCanBeCancelled(t *testing.T) {
	dir := t.TempDir()
	writeFlowFixture(t, dir+"/.mcp.json", `{"mcpServers": {"svc": {"command": "cmd"}}}`)

	m := loadInitial(t, dir)
	m, _ = drive(t, m, keyMsg("d"))
	m, _ = drive(t, m, keyMsg("n"))
	if m.view != viewList {
		t.Fatalf("expected back to viewList after cancelling delete, got %v", m.view)
	}

	snap, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if _, ok := snap.Claude.Servers["svc"]; !ok {
		t.Errorf("svc should NOT have been deleted after cancel")
	}
}

func TestTUIFlow_FormEscCancelsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	m := loadInitial(t, dir)

	m, _ = drive(t, m, keyMsg("a"))
	m.form.nameInput.SetValue("shouldnotpersist")
	m, _ = drive(t, m, keyMsg("esc"))
	if m.view != viewList {
		t.Fatalf("expected viewList after esc, got %v", m.view)
	}

	snap, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(snap.Claude.Servers) != 0 {
		t.Errorf("expected no servers written, got %v", snap.Claude.Servers)
	}
}

func TestTUIFlow_InvalidNameShowsErrorAndStaysOnForm(t *testing.T) {
	dir := t.TempDir()
	m := loadInitial(t, dir)

	m, _ = drive(t, m, keyMsg("a"))
	m.form.nameInput.SetValue("bad name with spaces")
	m.form.next()
	m.form.commandInput.SetValue("cmd")

	m, _ = drive(t, m, keyMsg("ctrl+s"))
	if m.view != viewForm {
		t.Fatalf("expected to stay on viewForm after invalid name, got %v", m.view)
	}
	if m.form.errMsg == "" {
		t.Errorf("expected a validation error message")
	}
}
