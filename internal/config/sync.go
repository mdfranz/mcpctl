package config

import "sort"

// SyncConflict reports a server whose client definitions disagree.
type SyncConflict struct {
	Name    string
	Clients []string
}

// SyncPlan contains the proposed full project resynchronization and any
// divergent definitions that must be resolved before applying it.
type SyncPlan struct {
	Plan      *Plan
	Servers   map[string]Server
	Conflicts []SyncConflict
}

// BuildSyncPlan selects the first definition in the documented precedence
// order (Claude, OpenCode, Codex), while refusing to authorize divergent
// definitions implicitly. The generated Plan is still useful for preview.
func BuildSyncPlan(dir string) (*SyncPlan, error) {
	snap, err := Load(dir)
	if err != nil {
		return nil, err
	}

	servers := make(map[string]Server)
	clients := map[string]map[string]Server{
		"claude":   snap.Claude.Servers,
		"opencode": snap.OpenCode.Servers,
		"codex":    snap.Codex.Servers,
	}
	namesSet := map[string]bool{}
	for _, entries := range clients {
		for name := range entries {
			namesSet[name] = true
		}
	}
	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}
	sort.Strings(names)

	var conflicts []SyncConflict
	for _, name := range names {
		var selected Server
		selectedSet := false
		var present []string
		for _, clientName := range []string{"claude", "opencode", "codex"} {
			if server, ok := clients[clientName][name]; ok {
				present = append(present, clientName)
				if !selectedSet {
					selected, selectedSet = server, true
				} else if !serversEqual(selected, server) {
					conflicts = append(conflicts, SyncConflict{Name: name, Clients: append([]string(nil), present...)})
					break
				}
			}
		}
		if selectedSet {
			servers[name] = selected
		}
	}

	plan, err := BuildPlan(dir, servers, nil)
	if err != nil {
		return nil, err
	}
	return &SyncPlan{Plan: plan, Servers: servers, Conflicts: conflicts}, nil
}
