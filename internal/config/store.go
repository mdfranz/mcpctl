package config

import "sort"

// Snapshot is the combined, as-loaded state of a project's MCP
// configuration across all supported clients. It holds canonical
// candidates, original documents and hashes, and unsupported entries
// per client. Merge/precedence/conflict resolution into a single
// per-server canonical candidate is built on top of this in a later
// phase (Plan).
type Snapshot struct {
	Dir string

	Claude   *ClaudeSnapshot
	OpenCode *OpenCodeSnapshot
	Codex    *CodexSnapshot
}

// Load reads all three clients' project files under dir. Missing files
// are not errors; each client's snapshot reports Exists == false.
func Load(dir string) (*Snapshot, error) {
	claudeSnap, err := LoadClaude(dir)
	if err != nil {
		return nil, err
	}
	openCodeSnap, err := LoadOpenCode(dir)
	if err != nil {
		return nil, err
	}
	codexSnap, err := LoadCodex(dir)
	if err != nil {
		return nil, err
	}

	return &Snapshot{
		Dir:      dir,
		Claude:   claudeSnap,
		OpenCode: openCodeSnap,
		Codex:    codexSnap,
	}, nil
}

// ServerNames returns the sorted union of server names known to any
// client, for iteration by callers that need a stable order.
func (s *Snapshot) ServerNames() []string {
	seen := map[string]bool{}
	for name := range s.Claude.Servers {
		seen[name] = true
	}
	for name := range s.OpenCode.Servers {
		seen[name] = true
	}
	for name := range s.Codex.Servers {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
