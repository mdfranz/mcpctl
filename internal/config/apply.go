package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// FileApplyResult reports what happened to one client's file during
// Apply, regardless of whether Apply as a whole succeeded.
type FileApplyResult struct {
	Path       string
	Attempted  bool
	Written    bool
	RolledBack bool
	Err        error
}

// ApplyResult reports, per client, what Apply did.
type ApplyResult struct {
	Claude   FileApplyResult
	OpenCode FileApplyResult
	Codex    FileApplyResult
}

// Apply writes plan's changed files to disk, one client at a time in a
// fixed order (Claude, OpenCode, Codex). Per-file writes are atomic
// (temp file + rename in the destination directory); the three-file
// operation as a whole is not.
//
// Immediately before writing each file, Apply re-reads it and refuses if
// its hash no longer matches what BuildPlan observed -- a concurrent
// edit (by another mcpctl invocation or an external editor) is reported
// rather than overwritten. On any failure, Apply attempts to restore
// files it had already written to their original content, but only if
// nothing has touched them again since; it will not clobber a newer
// external change during rollback. The returned ApplyResult always
// reports exactly which files were written, restored, or left alone, so
// callers can tell the user precisely what state they're in.
func Apply(plan *Plan) (*ApplyResult, error) {
	unlock, err := acquireProjectLock(plan.Dir)
	if err != nil {
		return nil, fmt.Errorf("acquire project lock: %w", err)
	}
	defer unlock()

	result := &ApplyResult{}
	steps := []struct {
		name string
		cp   *ClientPlan
		res  *FileApplyResult
	}{
		{"claude", plan.Claude, &result.Claude},
		{"opencode", plan.OpenCode, &result.OpenCode},
		{"codex", plan.Codex, &result.Codex},
	}

	var applied []int
	var stepErr error
	var failedAt string

	for i, s := range steps {
		s.res.Path = s.cp.Path
		if !s.cp.Changed {
			continue
		}
		s.res.Attempted = true

		if err := checkHashUnchanged(s.cp); err != nil {
			s.res.Err = err
			stepErr, failedAt = err, s.name
			break
		}
		if err := atomicWriteFile(s.cp.Path, s.cp.NewRaw); err != nil {
			s.res.Err = err
			stepErr, failedAt = err, s.name
			break
		}
		s.res.Written = true
		applied = append(applied, i)
	}

	if stepErr == nil {
		return result, nil
	}

	for _, i := range applied {
		s := steps[i]
		if rerr := restoreOriginal(s.cp); rerr != nil {
			s.res.Err = fmt.Errorf("wrote new content but rollback failed, file may be left in the new state: %w", rerr)
			continue
		}
		s.res.Written = false
		s.res.RolledBack = true
	}

	return result, fmt.Errorf("%s: %w", failedAt, stepErr)
}

func checkHashUnchanged(cp *ClientPlan) error {
	raw, err := os.ReadFile(cp.Path)
	if err != nil {
		if os.IsNotExist(err) {
			if cp.Exists {
				return fmt.Errorf("file was deleted externally after it was loaded; reload and retry")
			}
			return nil
		}
		return err
	}
	if !cp.Exists {
		return fmt.Errorf("file was created externally after it was loaded; reload and retry")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != cp.ExpectHash {
		return fmt.Errorf("file changed externally after it was loaded; reload and retry")
	}
	return nil
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mcpctl-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// restoreOriginal reverts one client's file to cp.OldRaw, but only if the
// file still holds exactly what Apply just wrote (cp.NewRaw); if it has
// been changed again by something else in the meantime, it's left alone
// and an error is returned so the caller can report the discrepancy
// instead of silently destroying a newer edit.
func restoreOriginal(cp *ClientPlan) error {
	cur, err := os.ReadFile(cp.Path)
	if err != nil {
		return err
	}
	if string(cur) != string(cp.NewRaw) {
		return fmt.Errorf("file changed again after our write; not overwriting during rollback")
	}
	if !cp.Exists {
		return os.Remove(cp.Path)
	}
	return atomicWriteFile(cp.Path, cp.OldRaw)
}

func acquireProjectLock(dir string) (unlock func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".mcpctl.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another mcpctl process appears to be writing this project (lock file %s exists); remove it if it's stale", path)
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return func() { os.Remove(path) }, nil
}
