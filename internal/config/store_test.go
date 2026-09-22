package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

const basicFixtureDir = "../../testdata/config/basic"

func TestLoadBasicFixture(t *testing.T) {
	snap, err := Load(basicFixtureDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, c := range []struct {
		name   string
		exists bool
	}{
		{"claude", snap.Claude.Exists},
		{"opencode", snap.OpenCode.Exists},
		{"codex", snap.Codex.Exists},
	} {
		if !c.exists {
			t.Errorf("%s: expected Exists == true", c.name)
		}
	}

	wantNames := []string{"files", "remote-search"}
	if got := snap.ServerNames(); !equalStrings(got, wantNames) {
		t.Errorf("ServerNames() = %v, want %v", got, wantNames)
	}

	for client, unsupported := range map[string]map[string]string{
		"claude":   snap.Claude.Unsupported,
		"opencode": snap.OpenCode.Unsupported,
		"codex":    snap.Codex.Unsupported,
	} {
		if len(unsupported) != 0 {
			t.Errorf("%s: unexpected unsupported entries: %v", client, unsupported)
		}
	}
}

func TestLoadClaudeFilesServer(t *testing.T) {
	snap, err := LoadClaude(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadClaude: %v", err)
	}
	srv, ok := snap.Servers["files"]
	if !ok {
		t.Fatalf("server %q not loaded", "files")
	}
	if srv.Type != ServerTypeStdio {
		t.Errorf("Type = %v, want stdio", srv.Type)
	}
	if srv.Command != "mcp-server-files" {
		t.Errorf("Command = %q", srv.Command)
	}

	env := envByName(srv.Env)
	apiKey, ok := env["API_KEY"]
	if !ok || apiKey.Kind != EnvVarPassthrough || apiKey.Default != nil {
		t.Errorf("API_KEY = %+v, want passthrough with no default", apiKey)
	}
	region, ok := env["REGION"]
	if !ok || region.Kind != EnvVarPassthrough || region.Default == nil || *region.Default != "us-east-1" {
		t.Errorf("REGION = %+v, want passthrough with default us-east-1", region)
	}
	mode, ok := env["MODE"]
	if !ok || mode.Kind != EnvVarLiteral || mode.Value != "debug" {
		t.Errorf("MODE = %+v, want literal debug", mode)
	}
}

func TestLoadClaudeRemoteSearchServer(t *testing.T) {
	snap, err := LoadClaude(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadClaude: %v", err)
	}
	srv, ok := snap.Servers["remote-search"]
	if !ok {
		t.Fatalf("server %q not loaded", "remote-search")
	}
	if srv.Type != ServerTypeRemote {
		t.Errorf("Type = %v, want remote", srv.Type)
	}
	if srv.URL != "https://search.example.com/mcp" {
		t.Errorf("URL = %q", srv.URL)
	}
	auth, ok := srv.Headers["Authorization"]
	if !ok || auth.Kind != EnvVarPassthrough || auth.EnvName != "SEARCH_TOKEN" {
		t.Errorf("Authorization header = %+v", auth)
	}
	client, ok := srv.Headers["X-Client"]
	if !ok || client.Kind != EnvVarLiteral || client.Value != "mcpctl" {
		t.Errorf("X-Client header = %+v", client)
	}
}

func TestLoadOpenCodeFilesServer(t *testing.T) {
	snap, err := LoadOpenCode(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadOpenCode: %v", err)
	}
	srv, ok := snap.Servers["files"]
	if !ok {
		t.Fatalf("server %q not loaded", "files")
	}
	if srv.Command != "mcp-server-files" || len(srv.Args) != 2 || srv.Args[0] != "--root" || srv.Args[1] != "." {
		t.Errorf("Command/Args = %q %v", srv.Command, srv.Args)
	}
	extras, ok := snap.Extras["files"]
	if !ok {
		t.Fatalf("expected extras for files (enabled)")
	}
	if _, ok := extras["enabled"]; !ok {
		t.Errorf("expected 'enabled' to survive as extra, got %v", extras)
	}
}

func TestLoadCodexServers(t *testing.T) {
	snap, err := LoadCodex(basicFixtureDir)
	if err != nil {
		t.Fatalf("LoadCodex: %v", err)
	}
	files, ok := snap.Servers["files"]
	if !ok {
		t.Fatalf("server %q not loaded", "files")
	}
	env := envByName(files.Env)
	if apiKey, ok := env["API_KEY"]; !ok || apiKey.Kind != EnvVarPassthrough {
		t.Errorf("API_KEY = %+v, want passthrough", apiKey)
	}
	if mode, ok := env["MODE"]; !ok || mode.Kind != EnvVarLiteral || mode.Value != "debug" {
		t.Errorf("MODE = %+v, want literal debug", mode)
	}

	remote, ok := snap.Servers["remote-search"]
	if !ok {
		t.Fatalf("server %q not loaded", "remote-search")
	}
	if remote.BearerTokenEnvVar != "SEARCH_TOKEN" {
		t.Errorf("BearerTokenEnvVar = %q", remote.BearerTokenEnvVar)
	}
	if h, ok := remote.Headers["X-Client"]; !ok || h.Value != "mcpctl" {
		t.Errorf("X-Client header = %+v", h)
	}
}

func TestLoadMissingFilesIsNotError(t *testing.T) {
	dir := t.TempDir()
	snap, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Claude.Exists || snap.OpenCode.Exists || snap.Codex.Exists {
		t.Errorf("expected no files to exist in empty dir")
	}
	if len(snap.ServerNames()) != 0 {
		t.Errorf("expected no servers")
	}
}

func TestCodexAuthorizationBearerCollisionRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/.codex/config.toml", `
[mcp_servers.bad]
url = "https://example.com"
bearer_token_env_var = "TOKEN"

[mcp_servers.bad.http_headers]
Authorization = "literal-token"
`)
	snap, err := LoadCodex(dir)
	if err != nil {
		t.Fatalf("LoadCodex: %v", err)
	}
	if _, ok := snap.Servers["bad"]; ok {
		t.Errorf("expected 'bad' to be rejected as unsupported, not loaded")
	}
	if _, ok := snap.Unsupported["bad"]; !ok {
		t.Errorf("expected 'bad' to be reported as unsupported")
	}
}

func envByName(env []EnvVar) map[string]EnvVar {
	out := make(map[string]EnvVar, len(env))
	for _, e := range env {
		out[e.Name] = e
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
