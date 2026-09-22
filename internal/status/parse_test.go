package status

import (
	"os"
	"testing"
	"time"

	"github.com/mdfranz/mcpctl/internal/config"
)

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return string(b)
}

func TestParseClaudeList_PendingApproval(t *testing.T) {
	out := readFixture(t, "../../testdata/clients/claude/list_pending_approval.txt")
	entries := parseClaudeList(out)
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want 2", entries)
	}
	byName := map[string]claudeListEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	stdio, ok := byName["fake-stdio"]
	if !ok || stdio.Descriptor != "cat" {
		t.Errorf("fake-stdio = %+v", stdio)
	}
	remote, ok := byName["fake-remote"]
	if !ok || remote.Descriptor != "https://example.invalid/mcp (HTTP)" {
		t.Errorf("fake-remote = %+v", remote)
	}

	for name, e := range byName {
		cs, conn, auth, check := classifyClaudeStatus(e.StatusText)
		if cs != ConfigPendingApproval {
			t.Errorf("%s: ConfigState = %v, want pending_approval", name, cs)
		}
		if conn != ConnectionUnchecked {
			t.Errorf("%s: Connection = %v, want unchecked", name, conn)
		}
		if auth != AuthUnknown {
			t.Errorf("%s: AuthState = %v, want unknown", name, auth)
		}
		if check != CheckComplete {
			t.Errorf("%s: CheckState = %v, want complete", name, check)
		}
	}
}

func TestParseClaudeList_PreservesNamespacedNames(t *testing.T) {
	entries := parseClaudeList("plugin:logfire:logfire: https://logfire-us.pydantic.dev/mcp (HTTP) - ⊘ Disabled for this project\n")
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want one", entries)
	}
	if entries[0].Name != "plugin:logfire:logfire" {
		t.Errorf("name = %q, want complete namespaced name", entries[0].Name)
	}
}

func TestParseClaudeGet_ProjectScope(t *testing.T) {
	out := readFixture(t, "../../testdata/clients/claude/get_stdio_pending_approval.txt")
	if scope := parseClaudeScope(out); scope != ScopeProject {
		t.Errorf("scope = %v, want project", scope)
	}
}

func TestBuildClaudeResults_UsesGetForScope(t *testing.T) {
	listOut := readFixture(t, "../../testdata/clients/claude/list_pending_approval.txt")
	getOutputs := map[string]string{
		"fake-stdio":  readFixture(t, "../../testdata/clients/claude/get_stdio_pending_approval.txt"),
		"fake-remote": readFixture(t, "../../testdata/clients/claude/get_remote_pending_approval.txt"),
	}
	getFor := func(name string) (string, Evidence, error) {
		return getOutputs[name], Evidence{}, nil
	}
	results := BuildClaudeResults(listOut, "2.1.278", time.Now(), Evidence{}, getFor)
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	for _, r := range results {
		if r.Scope != ScopeProject {
			t.Errorf("%s: Scope = %v, want project", r.ServerName, r.Scope)
		}
		if r.ConfigState != ConfigPendingApproval {
			t.Errorf("%s: ConfigState = %v", r.ServerName, r.ConfigState)
		}
	}
}

func TestParseCodexList_JSON(t *testing.T) {
	raw := []byte(readFixture(t, "../../testdata/clients/codex/list_json.json"))
	projectServers := map[string]config.Server{
		"fake-stdio":  {Name: "fake-stdio", Type: config.ServerTypeStdio, Command: "cat"},
		"fake-remote": {Name: "fake-remote", Type: config.ServerTypeRemote, URL: "https://example.invalid/mcp"},
	}
	results, err := BuildCodexResults(raw, "0.155.1", time.Now(), Evidence{}, projectServers)
	if err != nil {
		t.Fatalf("BuildCodexResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v", results)
	}
	byName := map[string]Result{}
	for _, r := range results {
		byName[r.ServerName] = r
	}

	stdio := byName["fake-stdio"]
	if stdio.ConfigState != ConfigPresent {
		t.Errorf("fake-stdio ConfigState = %v", stdio.ConfigState)
	}
	if stdio.Connection != ConnectionUnchecked {
		t.Errorf("fake-stdio Connection = %v, want unchecked (codex list is config-only)", stdio.Connection)
	}
	if stdio.AuthState != AuthNotRequired || stdio.AuthMethod != AuthMethodNone {
		t.Errorf("fake-stdio auth = %v/%v, want not_required/none", stdio.AuthState, stdio.AuthMethod)
	}
	if stdio.Scope != ScopeProject {
		t.Errorf("fake-stdio Scope = %v, want project (command matches project definition)", stdio.Scope)
	}

	remote := byName["fake-remote"]
	if remote.AuthState != AuthUnknown {
		t.Errorf("fake-remote AuthState = %v, want unknown (codex reported auth_status=unknown)", remote.AuthState)
	}
	if remote.Scope != ScopeProject {
		t.Errorf("fake-remote Scope = %v, want project (url matches project definition)", remote.Scope)
	}
}

func TestCodexScope_MismatchIsOther(t *testing.T) {
	raw := []byte(readFixture(t, "../../testdata/clients/codex/list_json.json"))
	// Project definition differs from what codex reported: this should
	// not be attributed to the project.
	projectServers := map[string]config.Server{
		"fake-stdio": {Name: "fake-stdio", Type: config.ServerTypeStdio, Command: "totally-different-command"},
	}
	results, err := BuildCodexResults(raw, "0.155.1", time.Now(), Evidence{}, projectServers)
	if err != nil {
		t.Fatalf("BuildCodexResults: %v", err)
	}
	for _, r := range results {
		if r.ServerName == "fake-stdio" && r.Scope != ScopeOther {
			t.Errorf("fake-stdio Scope = %v, want other on command mismatch", r.Scope)
		}
		if r.ServerName == "fake-remote" && r.Scope != ScopeOther {
			t.Errorf("fake-remote Scope = %v, want other (not in projectServers at all)", r.Scope)
		}
	}
}

func TestParseOpenCodeList_Failed(t *testing.T) {
	raw := readFixture(t, "../../testdata/clients/opencode/list_failed.raw")
	entries := parseOpenCodeList(raw)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2", entries)
	}
	byName := map[string]opencodeEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	stdio, ok := byName["fake-stdio"]
	if !ok {
		t.Fatalf("fake-stdio not found")
	}
	if stdio.StatusText != "failed" {
		t.Errorf("fake-stdio StatusText = %q", stdio.StatusText)
	}
	if len(stdio.Detail) != 2 || stdio.Detail[1] != "cat" {
		t.Errorf("fake-stdio Detail = %v", stdio.Detail)
	}

	remote, ok := byName["fake-remote"]
	if !ok {
		t.Fatalf("fake-remote not found")
	}
	if len(remote.Detail) != 2 || remote.Detail[1] != "https://example.invalid/mcp" {
		t.Errorf("fake-remote Detail = %v", remote.Detail)
	}
}

func TestBuildOpenCodeResults_FailedWithScope(t *testing.T) {
	raw := readFixture(t, "../../testdata/clients/opencode/list_failed.raw")
	projectServers := map[string]config.Server{
		"fake-stdio":  {Name: "fake-stdio", Type: config.ServerTypeStdio, Command: "cat"},
		"fake-remote": {Name: "fake-remote", Type: config.ServerTypeRemote, URL: "https://example.invalid/mcp"},
	}
	results := BuildOpenCodeResults(raw, "1.18.18", time.Now(), Evidence{}, projectServers)
	if len(results) != 2 {
		t.Fatalf("results = %+v", results)
	}
	for _, r := range results {
		if r.Connection != ConnectionFailed {
			t.Errorf("%s: Connection = %v, want failed", r.ServerName, r.Connection)
		}
		if r.ConfigState != ConfigPresent {
			t.Errorf("%s: ConfigState = %v, want present", r.ServerName, r.ConfigState)
		}
		if r.Scope != ScopeProject {
			t.Errorf("%s: Scope = %v, want project", r.ServerName, r.Scope)
		}
		if len(r.Evidence) == 0 {
			t.Errorf("%s: expected detail evidence", r.ServerName)
		}
	}
}

func TestParseOpenCodeList_EmptyState(t *testing.T) {
	entries := parseOpenCodeList("┌  MCP Servers\n│\n▲  No MCP servers configured\n│\n└  Add servers with: opencode mcp add\n")
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want none", entries)
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[90mfailed\x1b[0m"
	if got := stripANSI(in); got != "failed" {
		t.Errorf("stripANSI(%q) = %q, want %q", in, got, "failed")
	}
}
