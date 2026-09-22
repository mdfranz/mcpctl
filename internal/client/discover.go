package client

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Availability reports whether a client binary was found on PATH and,
// if so, its self-reported version string. A missing client is not an
// error: callers report it as individually unavailable and keep
// checking the others.
type Availability struct {
	Name    string
	Present bool
	Path    string
	Version string
}

// DetectAvailability looks up name on PATH and, if found, runs it with
// versionArgs (typically "--version") to record a version string.
func DetectAvailability(ctx context.Context, name string, versionArgs ...string) Availability {
	path, err := exec.LookPath(name)
	if err != nil {
		return Availability{Name: name}
	}
	res := Run(ctx, "", 5*time.Second, append([]string{path}, versionArgs...)...)
	version := strings.TrimSpace(string(res.Stdout))
	if version == "" {
		version = strings.TrimSpace(string(res.Stderr))
	}
	return Availability{Name: name, Present: true, Path: path, Version: version}
}
