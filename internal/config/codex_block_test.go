package config

import (
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func mustRenderBlock(t *testing.T, name string, srv Server, extras map[string]interface{}) string {
	t.Helper()
	block, _, err := renderCodexServerBlock(name, srv, extras)
	if err != nil {
		t.Fatalf("renderCodexServerBlock(%s): %v", name, err)
	}
	return block
}

func assertValidTOML(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("result is not valid TOML: %v\n---\n%s", err, raw)
	}
	return doc
}

func TestApplyCodexEdit_UpdateExistingPreservesUntouchedServer(t *testing.T) {
	orig := `# top comment
[mcp_servers.alpha]
command = "alpha-bin"

# a note about beta
[mcp_servers.beta]
command = "beta-bin"
args = ["--flag"]
`
	srv := Server{Name: "beta", Type: ServerTypeStdio, Command: "beta-bin", Args: []string{"--flag", "--new"}}
	block := mustRenderBlock(t, "beta", srv, nil)

	out, err := ApplyCodexEdit([]byte(orig), map[string]string{"beta": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit: %v", err)
	}

	if !strings.Contains(string(out), "# top comment") {
		t.Errorf("preamble comment lost:\n%s", out)
	}
	if !strings.Contains(string(out), `[mcp_servers.alpha]`) || !strings.Contains(string(out), `command = "alpha-bin"`) {
		t.Errorf("untouched alpha server lost:\n%s", out)
	}
	if strings.Contains(string(out), "a note about beta") {
		t.Errorf("expected comment inside the replaced beta block to be gone")
	}

	doc := assertValidTOML(t, out)
	servers := doc["mcp_servers"].(map[string]interface{})
	betaMap := servers["beta"].(map[string]interface{})
	args := betaMap["args"].([]interface{})
	if len(args) != 2 || args[0] != "--flag" || args[1] != "--new" {
		t.Errorf("beta args = %v, want [--flag --new]", args)
	}
	alphaMap := servers["alpha"].(map[string]interface{})
	if alphaMap["command"] != "alpha-bin" {
		t.Errorf("alpha command = %v", alphaMap["command"])
	}
}

func TestApplyCodexEdit_SiblingNamesNotConfused(t *testing.T) {
	orig := `[mcp_servers.foo]
command = "foo-bin"

[mcp_servers.foo-bar]
command = "foo-bar-bin"
`
	srv := Server{Name: "foo", Type: ServerTypeStdio, Command: "foo-bin-v2"}
	block := mustRenderBlock(t, "foo", srv, nil)

	out, err := ApplyCodexEdit([]byte(orig), map[string]string{"foo": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit: %v", err)
	}
	doc := assertValidTOML(t, out)
	servers := doc["mcp_servers"].(map[string]interface{})
	if servers["foo"].(map[string]interface{})["command"] != "foo-bin-v2" {
		t.Errorf("foo not updated: %v", servers["foo"])
	}
	if servers["foo-bar"].(map[string]interface{})["command"] != "foo-bar-bin" {
		t.Errorf("foo-bar clobbered: %v", servers["foo-bar"])
	}
}

func TestApplyCodexEdit_SeparatedNestedTablesAllRemovedOnDelete(t *testing.T) {
	orig := `[mcp_servers.gamma]
command = "gamma-bin"

[mcp_servers.gamma.env]
MODE = "prod"

[mcp_servers.delta]
command = "delta-bin"

[mcp_servers.gamma.extra_nested]
foo = "bar"
`
	out, err := ApplyCodexEdit([]byte(orig), nil, []string{"gamma"})
	if err != nil {
		t.Fatalf("ApplyCodexEdit: %v", err)
	}
	if strings.Contains(string(out), "gamma") {
		t.Errorf("expected all gamma spans removed, including the one after delta:\n%s", out)
	}
	if !strings.Contains(string(out), `command = "delta-bin"`) {
		t.Errorf("delta lost:\n%s", out)
	}
	assertValidTOML(t, out)
}

func TestApplyCodexEdit_MultilineStringWithBracketLikeTextNotMistakenForHeader(t *testing.T) {
	orig := `[mcp_servers.alpha]
command = "alpha-bin"

[mcp_servers.beta]
command = "beta-bin"
notes = """
this looks like a header but isn't:
[mcp_servers.fake]
"""
`
	headers, ok, reason := scanTomlHeaders([]byte(orig))
	if !ok {
		t.Fatalf("scanTomlHeaders failed: %s", reason)
	}
	var names []string
	for _, h := range headers {
		names = append(names, strings.Join(h.path, "."))
	}
	want := []string{"mcp_servers.alpha", "mcp_servers.beta"}
	if len(names) != len(want) {
		t.Fatalf("headers = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("headers[%d] = %q, want %q", i, names[i], want[i])
		}
	}

	srv := Server{Name: "alpha", Type: ServerTypeStdio, Command: "alpha-bin-v2"}
	block := mustRenderBlock(t, "alpha", srv, nil)
	out, err := ApplyCodexEdit([]byte(orig), map[string]string{"alpha": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit: %v", err)
	}
	if !strings.Contains(string(out), "this looks like a header but isn't") {
		t.Errorf("multiline string content in untouched beta block was lost:\n%s", out)
	}
	assertValidTOML(t, out)
}

func TestApplyCodexEdit_QuotedKeyServerName(t *testing.T) {
	orig := `["mcp_servers"."weird name"]
command = "weird-bin"

[mcp_servers.normal]
command = "normal-bin"
`
	headers, ok, reason := scanTomlHeaders([]byte(orig))
	if !ok {
		t.Fatalf("scanTomlHeaders failed: %s", reason)
	}
	if len(headers) != 2 || headers[0].path[1] != "weird name" {
		t.Fatalf("headers = %+v, want second path segment 'weird name'", headers)
	}
}

func TestApplyCodexEdit_InsertNewServerAppendsAtEnd(t *testing.T) {
	orig := `[mcp_servers.alpha]
command = "alpha-bin"
`
	srv := Server{Name: "new-one", Type: ServerTypeRemote, URL: "https://example.com/mcp"}
	block := mustRenderBlock(t, "new-one", srv, nil)

	out, err := ApplyCodexEdit([]byte(orig), map[string]string{"new-one": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit: %v", err)
	}
	doc := assertValidTOML(t, out)
	servers := doc["mcp_servers"].(map[string]interface{})
	if _, ok := servers["alpha"]; !ok {
		t.Errorf("alpha lost:\n%s", out)
	}
	newOne, ok := servers["new-one"].(map[string]interface{})
	if !ok || newOne["url"] != "https://example.com/mcp" {
		t.Errorf("new-one = %v", servers["new-one"])
	}
}

func TestApplyCodexEdit_InsertIntoEmptyFile(t *testing.T) {
	srv := Server{Name: "solo", Type: ServerTypeStdio, Command: "solo-bin"}
	block := mustRenderBlock(t, "solo", srv, nil)

	out, err := ApplyCodexEdit(nil, map[string]string{"solo": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit: %v", err)
	}
	doc := assertValidTOML(t, out)
	servers := doc["mcp_servers"].(map[string]interface{})
	if _, ok := servers["solo"]; !ok {
		t.Errorf("solo missing:\n%s", out)
	}
}

func TestApplyCodexEdit_Idempotent(t *testing.T) {
	orig := `[mcp_servers.alpha]
command = "alpha-bin"
args = ["--flag"]
`
	srv := Server{Name: "alpha", Type: ServerTypeStdio, Command: "alpha-bin", Args: []string{"--flag"}}
	block := mustRenderBlock(t, "alpha", srv, nil)

	once, err := ApplyCodexEdit([]byte(orig), map[string]string{"alpha": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit (1st): %v", err)
	}
	twice, err := ApplyCodexEdit(once, map[string]string{"alpha": block}, nil)
	if err != nil {
		t.Fatalf("ApplyCodexEdit (2nd): %v", err)
	}
	if string(once) != string(twice) {
		t.Errorf("not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestCheckCodexEditable_RefusesInlineDefinition(t *testing.T) {
	orig := `[mcp_servers]
inline-name = { command = "inline-bin" }
`
	err := CheckCodexEditable([]byte(orig), map[string]bool{"inline-name": true})
	if err == nil {
		t.Fatalf("expected CheckCodexEditable to refuse an inline-defined server")
	}
}

func TestCheckCodexEditable_AllowsNewServerWithNoExistingSpan(t *testing.T) {
	orig := `[mcp_servers.alpha]
command = "alpha-bin"
`
	if err := CheckCodexEditable([]byte(orig), map[string]bool{"brand-new": false}); err != nil {
		t.Fatalf("unexpected error for a genuinely new server: %v", err)
	}
}
