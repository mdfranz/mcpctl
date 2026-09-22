package logging

import "testing"

func TestCommandNameRedactsSensitiveArguments(t *testing.T) {
	got := CommandName([]string{"codex", "-c", `api_key="secret-value"`, "mcp", "list"})
	want := "codex -c <redacted> mcp list"
	if got != want {
		t.Fatalf("CommandName() = %q, want %q", got, want)
	}
}
