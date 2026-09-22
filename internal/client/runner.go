// Package client runs the claude/codex/opencode CLIs as subprocesses and
// captures their output for the status package to parse. It never uses a
// shell, never provides interactive stdin, and always bounds output size
// and execution time.
package client

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// maxOutputBytes bounds how much of a subprocess's stdout/stderr is kept,
// per stream, so a runaway or chatty client process can't exhaust memory.
const maxOutputBytes = 1 << 20 // 1 MiB

// Result is a bounded, redaction-ready record of one subprocess
// invocation. It doubles as status Evidence once summarized.
type Result struct {
	Argv     []string
	Dir      string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Err is set on start failure or a timed-out/cancelled context; a
	// nonzero ExitCode from a normally-exited process is not an Err.
	Err       error
	TimedOut  bool
	Duration  time.Duration
	Truncated bool
}

// Run executes argv[0] with argv[1:] in dir (the project directory being
// inspected), bounded by timeout, with no stdin attached. Process
// cleanup on timeout/cancellation is handled by exec.CommandContext.
func Run(ctx context.Context, dir string, timeout time.Duration, argv ...string) Result {
	if len(argv) == 0 {
		return Result{Err: fmt.Errorf("no command given")}
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = nil

	var stdout, stderr boundedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start)

	res := Result{
		Argv:      argv,
		Dir:       dir,
		Stdout:    stdout.buf.Bytes(),
		Stderr:    stderr.buf.Bytes(),
		Duration:  dur,
		Truncated: stdout.truncated || stderr.truncated,
	}

	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.Err = fmt.Errorf("%s: timed out after %s", argv[0], timeout)
		return res
	}
	var exitErr *exec.ExitError
	if err != nil {
		if ok := asExitError(err, &exitErr); ok {
			res.ExitCode = exitErr.ExitCode()
			return res
		}
		res.Err = err
		return res
	}
	return res
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// boundedBuffer caps how many bytes it retains; writes past the limit are
// silently discarded (not errored, so the subprocess is never blocked or
// killed by a failing write) and Truncated is set.
type boundedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.truncated {
		return len(p), nil
	}
	remaining := maxOutputBytes - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}
