package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSyncPlan_EmptyProjectCreatesAllClientFiles(t *testing.T) {
	dir := t.TempDir()
	syncPlan, err := BuildSyncPlan(dir)
	if err != nil {
		t.Fatalf("BuildSyncPlan: %v", err)
	}
	if len(syncPlan.Conflicts) != 0 {
		t.Fatalf("Conflicts = %+v, want none", syncPlan.Conflicts)
	}
	if !syncPlan.Plan.Claude.Changed || !syncPlan.Plan.OpenCode.Changed || !syncPlan.Plan.Codex.Changed {
		t.Fatalf("expected all client files to change: %+v", syncPlan.Plan)
	}
}

func TestBuildSyncPlanReportsDivergentDefinitions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"svc":{"command":"claude-svc"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"mcp":{"svc":{"type":"local","command":["opencode-svc"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	syncPlan, err := BuildSyncPlan(dir)
	if err != nil {
		t.Fatalf("BuildSyncPlan: %v", err)
	}
	if len(syncPlan.Conflicts) != 1 {
		t.Fatalf("Conflicts = %+v, want one conflict", syncPlan.Conflicts)
	}
	if syncPlan.Conflicts[0].Name != "svc" {
		t.Errorf("conflict name = %q, want svc", syncPlan.Conflicts[0].Name)
	}
	if len(syncPlan.Conflicts[0].Clients) != 2 {
		t.Errorf("conflict clients = %v, want two clients", syncPlan.Conflicts[0].Clients)
	}
}
