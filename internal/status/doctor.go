package status

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mdfranz/mcpctl/internal/client"
	"github.com/mdfranz/mcpctl/internal/config"
)

// DoctorSeverity ranks a Finding for display and exit-code purposes.
type DoctorSeverity string

const (
	SeverityInfo    DoctorSeverity = "info"
	SeverityWarning DoctorSeverity = "warning"
	SeverityError   DoctorSeverity = "error"
)

// Finding is one local preflight observation. It never claims more than
// doctor actually checked: a present executable or env var is evidence
// of configuration, not proof a server can initialize (that's what
// `status`'s live checks are for).
type Finding struct {
	Category   string // "client" | "syntax" | "conflict" | "executable" | "env" | "remote"
	Client     string // "" when not client-specific
	ServerName string // "" when not server-specific
	Severity   DoctorSeverity
	Message    string
	NextStep   string // actionable suggestion; may be empty
}

// DoctorSummary gives --json consumers coverage counts without having to
// infer them from the Findings list (which only contains problems, not
// every passing check).
type DoctorSummary struct {
	ClientsAvailable int
	ClientsTotal     int
	ServersChecked   int
	Errors           int
	Warnings         int
}

type DoctorReport struct {
	Dir      string
	Findings []Finding
	Summary  DoctorSummary
}

// RunDoctor performs local preflight only: it never launches an MCP
// server, starts a login flow, or shells out to a client's `mcp
// list`/`get` (that's `status`'s job). It checks client
// availability/version, project config syntax, stdio executable
// resolution, referenced env var presence (never their values), remote
// URL/header sanity, and divergent definitions across clients.
func RunDoctor(ctx context.Context, dir string) (*DoctorReport, error) {
	snap, err := config.Load(dir)
	if err != nil {
		return nil, err
	}

	report := &DoctorReport{Dir: dir}
	add := func(f Finding) {
		report.Findings = append(report.Findings, f)
		switch f.Severity {
		case SeverityError:
			report.Summary.Errors++
		case SeverityWarning:
			report.Summary.Warnings++
		}
	}

	// 1. Client availability.
	avails := map[string]client.Availability{
		"claude":   client.DetectAvailability(ctx, "claude", "--version"),
		"codex":    client.DetectAvailability(ctx, "codex", "--version"),
		"opencode": client.DetectAvailability(ctx, "opencode", "--version"),
	}
	fileServersByClient := map[string]map[string]config.Server{
		"claude":   snap.Claude.Servers,
		"opencode": snap.OpenCode.Servers,
		"codex":    snap.Codex.Servers,
	}
	report.Summary.ClientsTotal = len(avails)
	for _, name := range []string{"claude", "codex", "opencode"} {
		a := avails[name]
		if a.Present {
			report.Summary.ClientsAvailable++
			continue
		}
		if n := len(fileServersByClient[name]); n > 0 {
			add(Finding{
				Category: "client", Client: name, Severity: SeverityWarning,
				Message:  fmt.Sprintf("%s is not installed, but this project configures %d server(s) for it", name, n),
				NextStep: fmt.Sprintf("install %s, or remove its entries if you don't use it here", name),
			})
		} else {
			add(Finding{
				Category: "client", Client: name, Severity: SeverityInfo,
				Message: fmt.Sprintf("%s is not installed", name),
			})
		}
	}

	// 2. Syntax / unsupported layouts already captured during Load.
	for clientName, unsupported := range map[string]map[string]string{
		"claude": snap.Claude.Unsupported, "opencode": snap.OpenCode.Unsupported, "codex": snap.Codex.Unsupported,
	} {
		names := make([]string, 0, len(unsupported))
		for n := range unsupported {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			add(Finding{
				Category: "syntax", Client: clientName, ServerName: name, Severity: SeverityError,
				Message:  unsupported[name],
				NextStep: "fix this entry manually; mcpctl can't safely load or manage it as-is",
			})
		}
	}

	// 3. Divergent definitions across clients for the same server name.
	checkConflicts(fileServersByClient, add)

	// 4/5/6. Per-server local checks: executable resolution, env var
	// presence, remote URL/header sanity.
	for clientName, servers := range fileServersByClient {
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			report.Summary.ServersChecked++
			checkServer(dir, clientName, name, servers[name], add)
		}
	}

	return report, nil
}

func checkConflicts(byClient map[string]map[string]config.Server, add func(Finding)) {
	names := map[string]bool{}
	for _, servers := range byClient {
		for name := range servers {
			names[name] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	clientOrder := []string{"claude", "opencode", "codex"}
	for _, name := range sorted {
		for i := 0; i < len(clientOrder); i++ {
			for j := i + 1; j < len(clientOrder); j++ {
				a, aok := byClient[clientOrder[i]][name]
				b, bok := byClient[clientOrder[j]][name]
				if !aok || !bok {
					continue
				}
				if diffs := describeServerDiff(a, b); len(diffs) > 0 {
					add(Finding{
						Category: "conflict", ServerName: name, Severity: SeverityWarning,
						Message:  fmt.Sprintf("%s and %s disagree on %s: %s", clientOrder[i], clientOrder[j], name, strings.Join(diffs, "; ")),
						NextStep: "decide which client's definition is canonical before syncing (mcpctl sync will support explicit resolution)",
					})
				}
			}
		}
	}
}

// describeServerDiff reports human-readable differences between two
// clients' definitions of what's nominally the same server. It's a
// diagnostic aid, not a merge algorithm.
func describeServerDiff(a, b config.Server) []string {
	var diffs []string
	if a.Type != b.Type {
		diffs = append(diffs, fmt.Sprintf("type %s vs %s", a.Type, b.Type))
		return diffs // further field comparisons don't make sense across types
	}
	switch a.Type {
	case config.ServerTypeStdio:
		if a.Command != b.Command {
			diffs = append(diffs, "command differs")
		}
		if !stringSlicesEqualDoctor(a.Args, b.Args) {
			diffs = append(diffs, "args differ")
		}
		if envDiff := diffEnvNames(a.Env, b.Env); envDiff != "" {
			diffs = append(diffs, envDiff)
		}
	case config.ServerTypeRemote:
		if a.URL != b.URL {
			diffs = append(diffs, "url differs")
		}
		if a.BearerTokenEnvVar != b.BearerTokenEnvVar {
			diffs = append(diffs, "bearer_token_env_var differs")
		}
		if headerDiff := diffHeaderNames(a.Headers, b.Headers); headerDiff != "" {
			diffs = append(diffs, headerDiff)
		}
	}
	return diffs
}

func stringSlicesEqualDoctor(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diffEnvNames(a, b []config.EnvVar) string {
	an, bn := envNameSet(a), envNameSet(b)
	var onlyA, onlyB []string
	for n := range an {
		if !bn[n] {
			onlyA = append(onlyA, n)
		}
	}
	for n := range bn {
		if !an[n] {
			onlyB = append(onlyB, n)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	var parts []string
	if len(onlyA) > 0 {
		parts = append(parts, "env only on first: "+strings.Join(onlyA, ","))
	}
	if len(onlyB) > 0 {
		parts = append(parts, "env only on second: "+strings.Join(onlyB, ","))
	}
	return strings.Join(parts, "; ")
}

func envNameSet(env []config.EnvVar) map[string]bool {
	m := make(map[string]bool, len(env))
	for _, e := range env {
		m[e.Name] = true
	}
	return m
}

func diffHeaderNames(a, b map[string]config.HeaderValue) string {
	var onlyA, onlyB []string
	for n := range a {
		if _, ok := b[n]; !ok {
			onlyA = append(onlyA, n)
		}
	}
	for n := range b {
		if _, ok := a[n]; !ok {
			onlyB = append(onlyB, n)
		}
	}
	sort.Strings(onlyA)
	sort.Strings(onlyB)
	var parts []string
	if len(onlyA) > 0 {
		parts = append(parts, "headers only on first: "+strings.Join(onlyA, ","))
	}
	if len(onlyB) > 0 {
		parts = append(parts, "headers only on second: "+strings.Join(onlyB, ","))
	}
	return strings.Join(parts, "; ")
}

func checkServer(dir, clientName, name string, srv config.Server, add func(Finding)) {
	switch srv.Type {
	case config.ServerTypeStdio:
		checkExecutable(dir, clientName, name, srv.Command, add)
		for _, ev := range srv.Env {
			if ev.Kind != config.EnvVarPassthrough {
				continue
			}
			checkEnvVar(clientName, name, ev.Name, ev.Default != nil, add)
		}
	case config.ServerTypeRemote:
		checkRemoteURL(clientName, name, srv.URL, add)
		for _, h := range srv.Headers {
			if h.Kind != config.EnvVarPassthrough {
				continue
			}
			checkEnvVar(clientName, name, h.EnvName, false, add)
		}
		if srv.BearerTokenEnvVar != "" {
			checkEnvVar(clientName, name, srv.BearerTokenEnvVar, false, add)
		}
	}
}

// checkExecutable resolves srv's command the way it would actually be
// launched: a path (contains a separator, or absolute) is resolved
// relative to the project directory; a bare name is looked up on PATH.
// This proves the command resolves to something, not that it's a valid
// MCP server.
func checkExecutable(dir, clientName, serverName, cmd string, add func(Finding)) {
	if cmd == "" {
		add(Finding{
			Category: "executable", Client: clientName, ServerName: serverName, Severity: SeverityError,
			Message: "command is empty",
		})
		return
	}

	var err error
	if filepath.IsAbs(cmd) || strings.ContainsRune(cmd, filepath.Separator) || strings.Contains(cmd, "/") {
		p := cmd
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		info, statErr := os.Stat(p)
		if statErr != nil {
			err = statErr
		} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
			err = fmt.Errorf("found but not executable: %s", p)
		}
	} else {
		_, err = exec.LookPath(cmd)
	}

	if err != nil {
		add(Finding{
			Category: "executable", Client: clientName, ServerName: serverName, Severity: SeverityError,
			Message:  fmt.Sprintf("command %q not found (%v)", cmd, err),
			NextStep: fmt.Sprintf("install %q or fix the configured command/path", cmd),
		})
	}
}

// checkEnvVar reports whether an OS env var this server's config
// references is set, without ever including its value. hasDefault
// suppresses the "not set" finding severity down to informational,
// since a passthrough default covers it at runtime.
func checkEnvVar(clientName, serverName, name string, hasDefault bool, add func(Finding)) {
	val, ok := os.LookupEnv(name)
	switch {
	case !ok && hasDefault:
		add(Finding{
			Category: "env", Client: clientName, ServerName: serverName, Severity: SeverityInfo,
			Message: fmt.Sprintf("%s is not set; will use its configured default", name),
		})
	case !ok:
		add(Finding{
			Category: "env", Client: clientName, ServerName: serverName, Severity: SeverityWarning,
			Message:  fmt.Sprintf("%s is not set and has no default", name),
			NextStep: fmt.Sprintf("export %s before running this server", name),
		})
	case val == "":
		add(Finding{
			Category: "env", Client: clientName, ServerName: serverName, Severity: SeverityInfo,
			Message: fmt.Sprintf("%s is set but empty", name),
		})
	}
}

func checkRemoteURL(clientName, serverName, rawURL string, add func(Finding)) {
	if rawURL == "" {
		add(Finding{
			Category: "remote", Client: clientName, ServerName: serverName, Severity: SeverityError,
			Message: "url is empty",
		})
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		add(Finding{
			Category: "remote", Client: clientName, ServerName: serverName, Severity: SeverityError,
			Message: fmt.Sprintf("url does not parse: %v", err),
		})
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		add(Finding{
			Category: "remote", Client: clientName, ServerName: serverName, Severity: SeverityError,
			Message: fmt.Sprintf("url scheme %q is not http/https", u.Scheme),
		})
		return
	}
	if u.Host == "" {
		add(Finding{
			Category: "remote", Client: clientName, ServerName: serverName, Severity: SeverityError,
			Message: "url has no host",
		})
	}
}
