package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareOpenCodeEnvFix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, OpenCodeConfigFile)
	original := `{"mcp":{"oneleet":{"type":"remote","url":"https://api.oneleet.com/mcp","headers":{"Authorization":"Bearer ${ONELEET_API_KEY}"}}}}` + "\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	gotPath, oldRaw, newRaw, changed, err := PrepareOpenCodeEnvFix(dir)
	if err != nil {
		t.Fatalf("PrepareOpenCodeEnvFix: %v", err)
	}
	if gotPath != path || !changed || string(oldRaw) != original {
		t.Fatalf("path/changed/old = %q/%v/%q", gotPath, changed, oldRaw)
	}
	if !strings.Contains(string(newRaw), "Bearer {env:ONELEET_API_KEY}") {
		t.Fatalf("new config did not contain OpenCode interpolation: %s", newRaw)
	}
	backup, err := ApplyOpenCodeEnvFix(path, oldRaw, newRaw)
	if err != nil {
		t.Fatalf("ApplyOpenCodeEnvFix: %v", err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup %q: %v", backup, err)
	}
	written, _ := os.ReadFile(path)
	if string(written) != string(newRaw) {
		t.Errorf("written config differs from prepared config")
	}
}
