package config

import "bytes"

// Plan is a proposed set of file changes across all three clients,
// produced by Plan and consumed by Apply. The same Plan powers both a
// dry-run preview and the actual write.
//
// Plan takes the desired canonical state directly from the caller
// (upsert/deletes); it does not yet compute cross-client precedence or
// detect divergent-definition conflicts between clients on its own. That
// merge/conflict layer belongs to a later phase (the TUI's source
// selection); for now the caller decides what "canonical" means for each
// server it names.
type Plan struct {
	Dir string

	Claude   *ClientPlan
	OpenCode *ClientPlan
	Codex    *ClientPlan

	Warnings []PortabilityWarning
}

// ClientPlan is one client's proposed file content.
type ClientPlan struct {
	Path string
	// Exists reports whether the file existed when Plan loaded it.
	Exists bool
	// ExpectHash is the sha256 hex digest Apply must still observe on
	// disk immediately before writing; empty when Exists is false.
	ExpectHash string
	OldRaw     []byte
	// NewRaw is nil when Changed is false.
	NewRaw  []byte
	Changed bool
}

// BuildPlan loads dir's current state and renders the effect of
// upserting each server in upsert and deleting each name in deletes,
// across all three clients. It does not write anything.
func BuildPlan(dir string, upsert map[string]Server, deletes []string) (*Plan, error) {
	snap, err := Load(dir)
	if err != nil {
		return nil, err
	}

	plan := &Plan{Dir: dir}

	codexPlan, codexWarnings, err := planCodex(snap.Codex, effectiveUpsert(upsert, snap.Codex.Servers), deletes)
	if err != nil {
		return nil, err
	}
	plan.Codex = codexPlan
	plan.Warnings = append(plan.Warnings, codexWarnings...)

	claudeRaw, claudeWarnings, err := renderClaudeDoc(snap.Claude, effectiveUpsert(upsert, snap.Claude.Servers), deletes)
	if err != nil {
		return nil, err
	}
	plan.Claude = diffClientPlan(snap.Claude.Path, snap.Claude.Exists, snap.Claude.Hash, snap.Claude.Raw, claudeRaw)
	plan.Warnings = append(plan.Warnings, claudeWarnings...)

	openRaw, openWarnings, err := renderOpenCodeDoc(snap.OpenCode, effectiveUpsert(upsert, snap.OpenCode.Servers), deletes)
	if err != nil {
		return nil, err
	}
	plan.OpenCode = diffClientPlan(snap.OpenCode.Path, snap.OpenCode.Exists, snap.OpenCode.Hash, snap.OpenCode.Raw, openRaw)
	plan.Warnings = append(plan.Warnings, openWarnings...)

	return plan, nil
}

// effectiveUpsert drops any entry whose desired value already matches
// what this client currently has, so re-saving an unchanged server
// doesn't touch that server's bytes (needed for idempotent Apply and for
// sync --dry-run to report "no changes pending" correctly). A server
// this client doesn't have yet always passes through, since it must be
// created.
func effectiveUpsert(upsert map[string]Server, current map[string]Server) map[string]Server {
	if len(upsert) == 0 {
		return upsert
	}
	out := make(map[string]Server, len(upsert))
	for name, desired := range upsert {
		if existing, ok := current[name]; ok && serversEqual(desired, existing) {
			continue
		}
		out[name] = desired
	}
	return out
}

func planCodex(snap *CodexSnapshot, upsert map[string]Server, deletes []string) (*ClientPlan, []PortabilityWarning, error) {
	existing := make(map[string]bool, len(snap.Servers)+len(snap.Unsupported))
	for name := range snap.Servers {
		existing[name] = true
	}
	for name := range snap.Unsupported {
		existing[name] = true
	}

	touched := make(map[string]bool, len(upsert)+len(deletes))
	for name := range upsert {
		touched[name] = existing[name]
	}
	for _, name := range deletes {
		touched[name] = existing[name]
	}
	if snap.Exists && len(touched) > 0 {
		if err := CheckCodexEditable(snap.Raw, touched); err != nil {
			return nil, nil, err
		}
	}

	blocks := make(map[string]string, len(upsert))
	var warnings []PortabilityWarning
	for name, srv := range upsert {
		block, w, err := renderCodexServerBlock(name, srv, snap.Extras[name])
		if err != nil {
			return nil, nil, err
		}
		blocks[name] = block
		warnings = append(warnings, w...)
	}

	newRaw := snap.Raw
	if len(blocks) > 0 || len(deletes) > 0 {
		edited, err := ApplyCodexEdit(snap.Raw, blocks, deletes)
		if err != nil {
			return nil, nil, err
		}
		newRaw = edited
	}

	return diffClientPlan(snap.Path, snap.Exists, snap.Hash, snap.Raw, newRaw), warnings, nil
}

func diffClientPlan(path string, existed bool, hash string, oldRaw, newRaw []byte) *ClientPlan {
	changed := !existed || !bytes.Equal(oldRaw, newRaw)
	cp := &ClientPlan{Path: path, Exists: existed, ExpectHash: hash, OldRaw: oldRaw, Changed: changed}
	if changed {
		cp.NewRaw = newRaw
	}
	return cp
}
