package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var shellEnvReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// PrepareOpenCodeEnvFix returns a formatted replacement for the affected
// OpenCode config. Only headers and environment values under mcp are changed.
func PrepareOpenCodeEnvFix(dir string) (path string, oldRaw, newRaw []byte, changed bool, err error) {
	path = filepath.Join(dir, OpenCodeConfigFile)
	oldRaw, err = os.ReadFile(path)
	if err != nil {
		return path, nil, nil, false, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(oldRaw, &top); err != nil {
		return path, nil, nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	var mcp map[string]json.RawMessage
	if raw, ok := top["mcp"]; ok {
		if err := json.Unmarshal(raw, &mcp); err != nil {
			return path, nil, nil, false, fmt.Errorf("parse %s mcp: %w", path, err)
		}
	}
	for name, rawServer := range mcp {
		var server map[string]json.RawMessage
		if json.Unmarshal(rawServer, &server) != nil {
			continue
		}
		changed = replaceOpenCodeStringMap(server, "headers") || changed
		changed = replaceOpenCodeStringMap(server, "environment") || changed
		if updated, marshalErr := json.Marshal(server); marshalErr == nil {
			mcp[name] = updated
		}
	}
	if !changed {
		return path, oldRaw, nil, false, nil
	}
	updatedMCP, err := json.Marshal(mcp)
	if err != nil {
		return path, nil, nil, false, err
	}
	top["mcp"] = updatedMCP
	newRaw, err = json.MarshalIndent(top, "", "  ")
	if err != nil {
		return path, nil, nil, false, err
	}
	newRaw = append(newRaw, '\n')
	return path, oldRaw, newRaw, !bytes.Equal(oldRaw, newRaw), nil
}

func replaceOpenCodeStringMap(server map[string]json.RawMessage, key string) bool {
	raw, ok := server[key]
	if !ok {
		return false
	}
	var values map[string]string
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	changed := false
	for name, value := range values {
		fixed := shellEnvReference.ReplaceAllString(value, "{env:$1}")
		if fixed != value {
			values[name] = fixed
			changed = true
		}
	}
	if changed {
		server[key], _ = json.Marshal(values)
	}
	return changed
}

// ApplyOpenCodeEnvFix writes a prepared fix after checking that the source
// file has not changed, and leaves a timestamped backup beside it.
func ApplyOpenCodeEnvFix(path string, oldRaw, newRaw []byte) (string, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(current)
	expected := sha256.Sum256(oldRaw)
	if hex.EncodeToString(hash[:]) != hex.EncodeToString(expected[:]) {
		return "", fmt.Errorf("%s changed while preparing the fix; reload and retry", path)
	}
	backup := fmt.Sprintf("%s.mcpctl-backup-%s", path, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backup, oldRaw, 0o600); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	if err := atomicWriteFile(path, newRaw); err != nil {
		return backup, err
	}
	return backup, nil
}
