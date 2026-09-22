package config

import (
	"os"
	"path/filepath"
	"testing"
)

func copyFixtureProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, rel := range []string{".mcp.json", "opencode.json", filepath.Join(".codex", "config.toml")} {
		data, err := os.ReadFile(filepath.Join(basicFixtureDir, rel))
		if err != nil {
			t.Fatalf("read fixture %s: %v", rel, err)
		}
		writeFile(t, filepath.Join(dir, rel), string(data))
	}
	return dir
}

func TestBuildPlanAndApply_AddServerAcrossAllThreeClients(t *testing.T) {
	dir := copyFixtureProject(t)

	newSrv := Server{
		Name:    "new-tool",
		Type:    ServerTypeStdio,
		Command: "mcp-new-tool",
	}
	plan, err := BuildPlan(dir, map[string]Server{"new-tool": newSrv}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if !plan.Claude.Changed || !plan.OpenCode.Changed || !plan.Codex.Changed {
		t.Fatalf("expected all three clients to change: claude=%v opencode=%v codex=%v",
			plan.Claude.Changed, plan.OpenCode.Changed, plan.Codex.Changed)
	}

	if _, err := Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	snap, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	for _, client := range []struct {
		name    string
		servers map[string]Server
	}{
		{"claude", snap.Claude.Servers},
		{"opencode", snap.OpenCode.Servers},
		{"codex", snap.Codex.Servers},
	} {
		srv, ok := client.servers["new-tool"]
		if !ok {
			t.Errorf("%s: new-tool not found after apply", client.name)
			continue
		}
		if srv.Command != "mcp-new-tool" {
			t.Errorf("%s: new-tool.Command = %q", client.name, srv.Command)
		}
	}
	// Pre-existing servers must be untouched.
	if _, ok := snap.Claude.Servers["files"]; !ok {
		t.Errorf("claude: existing 'files' server lost")
	}

	if _, err := os.Stat(filepath.Join(dir, ".mcpctl.lock")); !os.IsNotExist(err) {
		t.Errorf("lock file should be removed after Apply, stat err = %v", err)
	}
}

func TestBuildPlanAndApply_DeleteServerRemovesFromAllThreeClients(t *testing.T) {
	dir := copyFixtureProject(t)

	plan, err := BuildPlan(dir, nil, []string{"remote-search"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if _, err := Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	snap, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := snap.Claude.Servers["remote-search"]; ok {
		t.Errorf("claude: remote-search still present")
	}
	if _, ok := snap.OpenCode.Servers["remote-search"]; ok {
		t.Errorf("opencode: remote-search still present")
	}
	if _, ok := snap.Codex.Servers["remote-search"]; ok {
		t.Errorf("codex: remote-search still present")
	}
	if _, ok := snap.Codex.Servers["files"]; !ok {
		t.Errorf("codex: unrelated 'files' server lost")
	}
}

func TestBuildPlanAndApply_NoOpWhenNothingChanges(t *testing.T) {
	dir := copyFixtureProject(t)

	// Use a server whose fields render identically in all three formats
	// (a single literal env var, no defaults, no bearer/header asymmetry),
	// so a second BuildPlan with the same canonical value should be a
	// true no-op everywhere, not just semantically consistent for one
	// client.
	solo := Server{
		Name:    "solo",
		Type:    ServerTypeStdio,
		Command: "mcp-solo",
		Env:     []EnvVar{{Name: "MODE", Kind: EnvVarLiteral, Value: "prod"}},
	}
	firstPlan, err := BuildPlan(dir, map[string]Server{"solo": solo}, nil)
	if err != nil {
		t.Fatalf("BuildPlan (create): %v", err)
	}
	if _, err := Apply(firstPlan); err != nil {
		t.Fatalf("Apply (create): %v", err)
	}

	secondPlan, err := BuildPlan(dir, map[string]Server{"solo": solo}, nil)
	if err != nil {
		t.Fatalf("BuildPlan (reapply): %v", err)
	}
	if secondPlan.Claude.Changed || secondPlan.OpenCode.Changed || secondPlan.Codex.Changed {
		t.Errorf("expected a no-op re-apply of an unchanged server: claude=%v opencode=%v codex=%v",
			secondPlan.Claude.Changed, secondPlan.OpenCode.Changed, secondPlan.Codex.Changed)
	}
}

func TestApply_RefusesOnConcurrentExternalEdit(t *testing.T) {
	dir := copyFixtureProject(t)

	newSrv := Server{Name: "new-tool", Type: ServerTypeStdio, Command: "mcp-new-tool"}
	plan, err := BuildPlan(dir, map[string]Server{"new-tool": newSrv}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	// Simulate an external edit to the Claude file after the plan was
	// built but before Apply runs.
	claudePath := filepath.Join(dir, ".mcp.json")
	external, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	writeFile(t, claudePath, string(external)+"\n")

	_, err = Apply(plan)
	if err == nil {
		t.Fatalf("expected Apply to refuse due to a concurrent external edit")
	}

	if _, err := os.Stat(filepath.Join(dir, ".mcpctl.lock")); !os.IsNotExist(err) {
		t.Errorf("lock file should still be removed after a failed Apply, stat err = %v", err)
	}
}

func TestApply_RollsBackEarlierWritesOnLaterFailure(t *testing.T) {
	dir := copyFixtureProject(t)

	newSrv := Server{Name: "new-tool", Type: ServerTypeStdio, Command: "mcp-new-tool"}
	plan, err := BuildPlan(dir, map[string]Server{"new-tool": newSrv}, nil)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	claudeBefore, err := os.ReadFile(plan.Claude.Path)
	if err != nil {
		t.Fatalf("read claude before: %v", err)
	}

	// Force the codex step (applied last) to fail its hash check by
	// externally modifying the codex file after the plan was built.
	codexExternal, err := os.ReadFile(plan.Codex.Path)
	if err != nil {
		t.Fatalf("read codex: %v", err)
	}
	writeFile(t, plan.Codex.Path, string(codexExternal)+"\n# external edit\n")

	_, err = Apply(plan)
	if err == nil {
		t.Fatalf("expected Apply to fail on the codex step")
	}

	claudeAfter, err := os.ReadFile(plan.Claude.Path)
	if err != nil {
		t.Fatalf("read claude after: %v", err)
	}
	if string(claudeAfter) != string(claudeBefore) {
		t.Errorf("claude write was not rolled back after codex step failed:\nbefore:\n%s\nafter:\n%s", claudeBefore, claudeAfter)
	}
}
