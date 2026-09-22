package status

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mdfranz/mcpctl/internal/client"
	"github.com/mdfranz/mcpctl/internal/config"
	"github.com/mdfranz/mcpctl/internal/redact"
)

const defaultTimeout = 15 * time.Second

// ClientReport is one client's status check outcome: its availability,
// every Result obtained (including synthesized entries for servers the
// project defines but the client didn't report), and Err if the list
// command itself couldn't be run or parsed at all.
type ClientReport struct {
	Availability client.Availability
	Results      []Result
	Err          error
}

// Report is the full outcome of Check.
type Report struct {
	Dir      string
	Claude   ClientReport
	OpenCode ClientReport
	Codex    ClientReport
}

// Check runs status checks for all three clients against dir concurrently
// (one goroutine per client) and reconciles each client's live results
// against that client's own project config file, so a server defined in
// the file but not reported live is surfaced as an incomplete check
// rather than silently omitted.
func Check(ctx context.Context, dir string, timeout time.Duration) (*Report, error) {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	snap, err := config.Load(dir)
	if err != nil {
		return nil, err
	}

	report := &Report{Dir: dir}
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		report.Claude = checkClaude(ctx, dir, timeout, snap.Claude.Servers)
	}()
	go func() {
		defer wg.Done()
		report.OpenCode = checkOpenCode(ctx, dir, timeout, snap.OpenCode.Servers)
	}()
	go func() {
		defer wg.Done()
		report.Codex = checkCodex(ctx, dir, timeout, snap.Codex.Servers)
	}()
	wg.Wait()

	return report, nil
}

func checkClaude(ctx context.Context, dir string, timeout time.Duration, fileServers map[string]config.Server) ClientReport {
	avail := client.DetectAvailability(ctx, "claude", "--version")
	cr := ClientReport{Availability: avail}
	if !avail.Present {
		reconcile(&cr, "claude", fileServers)
		return cr
	}

	res := client.Run(ctx, dir, timeout, avail.Path, "mcp", "list")
	checkedAt := time.Now()
	ev := toEvidence(res)
	if res.Err != nil {
		cr.Err = res.Err
		reconcile(&cr, "claude", fileServers)
		return cr
	}

	getFor := func(name string) (string, Evidence, error) {
		getRes := client.Run(ctx, dir, timeout, avail.Path, "mcp", "get", name)
		return string(getRes.Stdout), toEvidence(getRes), getRes.Err
	}
	cr.Results = BuildClaudeResults(string(res.Stdout), avail.Version, checkedAt, ev, getFor)
	reconcile(&cr, "claude", fileServers)
	return cr
}

func checkCodex(ctx context.Context, dir string, timeout time.Duration, fileServers map[string]config.Server) ClientReport {
	avail := client.DetectAvailability(ctx, "codex", "--version")
	cr := ClientReport{Availability: avail}
	if !avail.Present {
		reconcile(&cr, "codex", fileServers)
		return cr
	}

	res := client.Run(ctx, dir, timeout, avail.Path, "mcp", "list", "--json")
	checkedAt := time.Now()
	ev := toEvidence(res)
	if res.Err != nil {
		cr.Err = res.Err
		reconcile(&cr, "codex", fileServers)
		return cr
	}

	results, err := BuildCodexResults(res.Stdout, avail.Version, checkedAt, ev, fileServers)
	if err != nil {
		cr.Err = err
		reconcile(&cr, "codex", fileServers)
		return cr
	}
	cr.Results = results
	reconcile(&cr, "codex", fileServers)
	return cr
}

func checkOpenCode(ctx context.Context, dir string, timeout time.Duration, fileServers map[string]config.Server) ClientReport {
	avail := client.DetectAvailability(ctx, "opencode", "--version")
	cr := ClientReport{Availability: avail}
	if !avail.Present {
		reconcile(&cr, "opencode", fileServers)
		return cr
	}

	res := client.Run(ctx, dir, timeout, avail.Path, "mcp", "list")
	checkedAt := time.Now()
	ev := toEvidence(res)
	if res.Err != nil {
		cr.Err = res.Err
		reconcile(&cr, "opencode", fileServers)
		return cr
	}

	cr.Results = BuildOpenCodeResults(string(res.Stdout), avail.Version, checkedAt, ev, fileServers)
	reconcile(&cr, "opencode", fileServers)
	return cr
}

// reconcile adds a synthesized Result for every server in fileServers
// that cr.Results didn't already cover, so a server defined in this
// client's own project config file is never silently dropped just
// because the live command didn't mention it (e.g. an untrusted Codex
// project, or a client that failed to run at all).
func reconcile(cr *ClientReport, clientName string, fileServers map[string]config.Server) {
	have := make(map[string]bool, len(cr.Results))
	for _, r := range cr.Results {
		have[r.ServerName] = true
	}

	for name := range fileServers {
		if have[name] {
			continue
		}
		res := Result{
			ServerName:  name,
			Client:      clientName,
			ConfigState: ConfigPresent,
			Connection:  ConnectionUnchecked,
			AuthState:   AuthUnknown,
			AuthMethod:  AuthMethodUnknown,
			Scope:       ScopeProject,
			CheckedAt:   time.Now(),
		}
		if cr.Availability.Present {
			res.ClientVersion = cr.Availability.Version
		}

		switch {
		case !cr.Availability.Present:
			res.CheckState = CheckUnavailable
			res.Evidence = []Evidence{{Summary: clientName + " is not installed (not found on PATH)"}}
		case cr.Err != nil:
			res.CheckState = CheckFailed
			res.Evidence = []Evidence{{Summary: "list command failed: " + redact.Summary(cr.Err.Error())}}
		default:
			res.CheckState = CheckUnsupported
			res.Evidence = []Evidence{{Summary: "defined in the project config but not reported by `" + clientName + " mcp list`"}}
		}
		cr.Results = append(cr.Results, res)
	}
}

func toEvidence(res client.Result) Evidence {
	summary := strings.TrimSpace(string(res.Stdout))
	if summary == "" {
		summary = strings.TrimSpace(string(res.Stderr))
	}
	// Collapse to a single line: this is a compact diagnostic summary,
	// not a place to persist raw multi-line/multi-server CLI output.
	summary = strings.Join(strings.Fields(summary), " ")
	summary = redact.Summary(summary)
	const maxSummary = 300
	if len(summary) > maxSummary {
		summary = summary[:maxSummary] + "…"
	}
	return Evidence{
		Command:  res.Argv,
		ExitCode: res.ExitCode,
		Summary:  summary,
		Duration: res.Duration,
	}
}
