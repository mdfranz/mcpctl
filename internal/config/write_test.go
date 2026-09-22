package config

import (
	"encoding/json"
	"testing"
)

func TestRenderClaudeDoc_PreservesUnrelatedServerAndTopLevel(t *testing.T) {
	snap, err := LoadClaude(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadClaude: %v", err)
	}

	updated := snap.Servers["files"]
	updated.Args = append(updated.Args, "--verbose")

	out, _, err := renderClaudeDoc(snap, map[string]Server{"files": updated}, nil)
	if err != nil {
		t.Fatalf("renderClaudeDoc: %v", err)
	}

	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, out)
	}

	var files struct {
		Args []string `json:"args"`
	}
	if err := json.Unmarshal(doc.McpServers["files"], &files); err != nil {
		t.Fatalf("files: %v", err)
	}
	if len(files.Args) != 3 || files.Args[2] != "--verbose" {
		t.Errorf("files.Args = %v", files.Args)
	}

	if _, ok := doc.McpServers["remote-search"]; !ok {
		t.Errorf("remote-search server lost from output")
	}
	var remote struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(doc.McpServers["remote-search"], &remote); err != nil {
		t.Fatalf("remote-search: %v", err)
	}
	if remote.Headers["Authorization"] != "${SEARCH_TOKEN}" {
		t.Errorf("remote-search.headers.Authorization = %q, want ${SEARCH_TOKEN}", remote.Headers["Authorization"])
	}
}

func TestRenderClaudeDoc_DeleteRemovesOnlyThatServer(t *testing.T) {
	snap, err := LoadClaude(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadClaude: %v", err)
	}
	out, _, err := renderClaudeDoc(snap, nil, []string{"remote-search"})
	if err != nil {
		t.Fatalf("renderClaudeDoc: %v", err)
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := doc.McpServers["remote-search"]; ok {
		t.Errorf("remote-search should have been deleted")
	}
	if _, ok := doc.McpServers["files"]; !ok {
		t.Errorf("files should still be present")
	}
}

func TestRenderClaudeDoc_UnknownTopLevelAndExtrasPreserved(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/.mcp.json", `{
  "someOtherTool": {"enabled": true},
  "mcpServers": {
    "files": {
      "command": "mcp-server-files",
      "timeout": 5000
    }
  }
}`)
	snap, err := LoadClaude(dir)
	if err != nil {
		t.Fatalf("LoadClaude: %v", err)
	}
	updated := snap.Servers["files"]
	updated.Command = "mcp-server-files-v2"

	out, _, err := renderClaudeDoc(snap, map[string]Server{"files": updated}, nil)
	if err != nil {
		t.Fatalf("renderClaudeDoc: %v", err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := doc["someOtherTool"]; !ok {
		t.Errorf("someOtherTool top-level key lost")
	}

	var servers map[string]json.RawMessage
	if err := json.Unmarshal(doc["mcpServers"], &servers); err != nil {
		t.Fatalf("mcpServers: %v", err)
	}
	var files struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := json.Unmarshal(servers["files"], &files); err != nil {
		t.Fatalf("files: %v", err)
	}
	if files.Command != "mcp-server-files-v2" {
		t.Errorf("Command = %q", files.Command)
	}
	if files.Timeout != 5000 {
		t.Errorf("timeout extra lost: %+v", files)
	}
}

func TestRenderClaudeDoc_UnsupportedServerSurvivesUntouched(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/.mcp.json", `{
  "mcpServers": {
    "broken": {"neither": "command-nor-url"},
    "files": {"command": "mcp-server-files"}
  }
}`)
	snap, err := LoadClaude(dir)
	if err != nil {
		t.Fatalf("LoadClaude: %v", err)
	}
	if _, ok := snap.Unsupported["broken"]; !ok {
		t.Fatalf("expected 'broken' to be unsupported")
	}

	updated := snap.Servers["files"]
	updated.Command = "v2"
	out, _, err := renderClaudeDoc(snap, map[string]Server{"files": updated}, nil)
	if err != nil {
		t.Fatalf("renderClaudeDoc: %v", err)
	}
	var doc struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if _, ok := doc.McpServers["broken"]; !ok {
		t.Errorf("unsupported server 'broken' was dropped on an unrelated save")
	}
}

func TestRenderOpenCodeDoc_RoundTripsCommandArrayAndEnv(t *testing.T) {
	snap, err := LoadOpenCode(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadOpenCode: %v", err)
	}
	updated := snap.Servers["files"]
	updated.Args = append(updated.Args, "--extra")

	out, warnings, err := renderOpenCodeDoc(snap, map[string]Server{"files": updated}, nil)
	if err != nil {
		t.Fatalf("renderOpenCodeDoc: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	var doc struct {
		Mcp map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var files struct {
		Command []string `json:"command"`
	}
	if err := json.Unmarshal(doc.Mcp["files"], &files); err != nil {
		t.Fatalf("files: %v", err)
	}
	want := []string{"mcp-server-files", "--root", ".", "--extra"}
	if len(files.Command) != len(want) {
		t.Fatalf("command = %v, want %v", files.Command, want)
	}
	for i := range want {
		if files.Command[i] != want[i] {
			t.Errorf("command[%d] = %q, want %q", i, files.Command[i], want[i])
		}
	}

	// 'enabled' extra should have survived from the fixture.
	var extras map[string]json.RawMessage
	if err := json.Unmarshal(doc.Mcp["files"], &extras); err != nil {
		t.Fatalf("files raw: %v", err)
	}
	if _, ok := extras["enabled"]; !ok {
		t.Errorf("'enabled' extra lost: %s", doc.Mcp["files"])
	}
}

func TestRenderOpenCodeDoc_DefaultEnvWarnsAndDrops(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/opencode.json", `{"mcp": {}}`)
	snap, err := LoadOpenCode(dir)
	if err != nil {
		t.Fatalf("LoadOpenCode: %v", err)
	}
	def := "us-east-1"
	srv := Server{
		Name:    "files",
		Type:    ServerTypeStdio,
		Command: "mcp-server-files",
		Env:     []EnvVar{{Name: "REGION", Kind: EnvVarPassthrough, Default: &def}},
	}
	out, warnings, err := renderOpenCodeDoc(snap, map[string]Server{"files": srv}, nil)
	if err != nil {
		t.Fatalf("renderOpenCodeDoc: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one portability warning", warnings)
	}

	var doc struct {
		Mcp map[string]json.RawMessage `json:"mcp"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	var files struct {
		Environment map[string]string `json:"environment"`
	}
	if err := json.Unmarshal(doc.Mcp["files"], &files); err != nil {
		t.Fatalf("files: %v", err)
	}
	if files.Environment["REGION"] != "{env:REGION}" {
		t.Errorf("REGION = %q, want {env:REGION} (default silently dropped, not fabricated)", files.Environment["REGION"])
	}
}
