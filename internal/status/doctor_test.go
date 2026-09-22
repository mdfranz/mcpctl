package status

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoctorFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func findingsIn(report *DoctorReport, category string) []Finding {
	var out []Finding
	for _, f := range report.Findings {
		if f.Category == category {
			out = append(out, f)
		}
	}
	return out
}

func TestRunDoctor_MissingExecutable(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "broken": {"command": "/definitely/not/a/real/command-xyz"}
  }
}`)
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	execFindings := findingsIn(report, "executable")
	if len(execFindings) != 1 {
		t.Fatalf("executable findings = %+v, want 1", execFindings)
	}
	if execFindings[0].Severity != SeverityError {
		t.Errorf("severity = %v, want error", execFindings[0].Severity)
	}
	if report.Summary.Errors != 1 {
		t.Errorf("Summary.Errors = %d, want 1", report.Summary.Errors)
	}
}

func TestRunDoctor_RelativeExecutableResolvedAgainstProjectDir(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "my-server.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "ok": {"command": "./my-server.sh"}
  }
}`)
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	if execFindings := findingsIn(report, "executable"); len(execFindings) != 0 {
		t.Errorf("unexpected executable findings for a real, executable script: %+v", execFindings)
	}
}

func TestRunDoctor_EnvVarPresenceWithoutLeakingValue(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "svc": {
      "command": "cat",
      "env": {
        "UNSET_VAR": "${UNSET_VAR}",
        "SET_VAR": "${SET_VAR}",
        "EMPTY_VAR": "${EMPTY_VAR}"
      }
    }
  }
}`)
	t.Setenv("SET_VAR", "super-secret-value-should-not-appear")
	t.Setenv("EMPTY_VAR", "")
	os.Unsetenv("UNSET_VAR")

	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	envFindings := findingsIn(report, "env")
	if len(envFindings) != 2 {
		t.Fatalf("env findings = %+v, want 2 (unset + empty; set-and-nonempty should produce none)", envFindings)
	}
	for _, f := range envFindings {
		if strings.Contains(f.Message, "super-secret") {
			t.Fatalf("finding leaked the env var value: %+v", f)
		}
	}

	var sawUnset, sawEmpty bool
	for _, f := range envFindings {
		if f.Severity == SeverityWarning {
			sawUnset = true
		}
		if f.Severity == SeverityInfo {
			sawEmpty = true
		}
	}
	if !sawUnset {
		t.Errorf("expected a warning for the unset var with no default: %+v", envFindings)
	}
	if !sawEmpty {
		t.Errorf("expected an info finding for the set-but-empty var: %+v", envFindings)
	}
}

func TestRunDoctor_EnvVarWithDefaultIsInfoNotWarning(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "svc": {"command": "cat", "env": {"REGION": "${REGION:-us-east-1}"}}
  }
}`)
	os.Unsetenv("REGION")
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	envFindings := findingsIn(report, "env")
	if len(envFindings) != 1 || envFindings[0].Severity != SeverityInfo {
		t.Fatalf("env findings = %+v, want a single info finding (default covers it)", envFindings)
	}
}

func TestRunDoctor_InvalidRemoteURL(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "bad-scheme": {"type": "ftp", "url": "ftp://example.com/mcp"}
  }
}`)
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	remoteFindings := findingsIn(report, "remote")
	if len(remoteFindings) != 1 || remoteFindings[0].Severity != SeverityError {
		t.Fatalf("remote findings = %+v, want one error", remoteFindings)
	}
}

func TestRunDoctor_SyntaxFindingForUnsupportedEntry(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "broken": {"neither": "command-nor-url"}
  }
}`)
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	syntaxFindings := findingsIn(report, "syntax")
	if len(syntaxFindings) != 1 || syntaxFindings[0].ServerName != "broken" {
		t.Fatalf("syntax findings = %+v", syntaxFindings)
	}
}

func TestRunDoctor_ConflictAcrossClients(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {"svc": {"command": "server-a"}}
}`)
	writeDoctorFixture(t, dir+"/opencode.json", `{
  "mcp": {"svc": {"type": "local", "command": ["server-b"]}}
}`)
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	conflicts := findingsIn(report, "conflict")
	if len(conflicts) != 1 || conflicts[0].ServerName != "svc" {
		t.Fatalf("conflict findings = %+v, want one for 'svc'", conflicts)
	}
}

func TestRunDoctor_NoConflictWhenDefinitionsMatch(t *testing.T) {
	dir := t.TempDir()
	writeDoctorFixture(t, dir+"/.mcp.json", `{
  "mcpServers": {"svc": {"command": "same-cmd"}}
}`)
	writeDoctorFixture(t, dir+"/opencode.json", `{
  "mcp": {"svc": {"type": "local", "command": ["same-cmd"]}}
}`)
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	if conflicts := findingsIn(report, "conflict"); len(conflicts) != 0 {
		t.Errorf("unexpected conflicts for matching definitions: %+v", conflicts)
	}
}

func TestRunDoctor_EmptyProjectHasNoFindings(t *testing.T) {
	dir := t.TempDir()
	report, err := RunDoctor(context.Background(), dir)
	if err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}
	// Client-availability info findings are allowed, but nothing else
	// should fire on a project with no MCP servers configured at all.
	for _, f := range report.Findings {
		if f.Category != "client" {
			t.Errorf("unexpected finding on an empty project: %+v", f)
		}
	}
	if report.Summary.ServersChecked != 0 {
		t.Errorf("ServersChecked = %d, want 0", report.Summary.ServersChecked)
	}
}
