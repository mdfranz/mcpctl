// Command mcpctl manages MCP server definitions shared across Claude
// Code, Codex CLI, and OpenCode project configuration files.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mdfranz/mcpctl/internal/client"
	"github.com/mdfranz/mcpctl/internal/config"
	"github.com/mdfranz/mcpctl/internal/logging"
	"github.com/mdfranz/mcpctl/internal/status"
	"github.com/mdfranz/mcpctl/internal/tui"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) (exitCode int) {
	start := time.Now()
	closeLog, _ := logging.Init()
	defer closeLog.Close()
	slog.Info("command_start", "args", args)
	defer func() {
		slog.Info("command_end", "exit_code", exitCode, "duration_ms", time.Since(start).Milliseconds())
	}()
	if len(args) == 0 {
		return runTUI(nil)
	}

	switch args[0] {
	case "status":
		return runStatus(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "sync":
		return runSync(args[1:])
	case "tui":
		return runTUI(args[1:])
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "mcpctl: unknown command %q\n", args[0])
		printUsage()
		return 2
	}
}

func runTUI(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	timeout := fs.Duration("timeout", 15*time.Second, "per-command timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := tui.Run(*dir, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "mcpctl: %v\n", err)
		return 2
	}
	return 0
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: mcpctl [command] [flags]

no command launches the interactive TUI in the current directory.

commands:
  status    show live project MCP server status across all clients
	doctor    local preflight: config syntax, executables, env vars, URLs
  sync      preview or apply a full project configuration resync
  tui       explicitly launch the interactive TUI
  help      show this message`)
}

func runSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	dryRun := fs.Bool("dry-run", false, "preview changes without writing files")
	yes := fs.Bool("yes", false, "apply without prompting")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	syncPlan, err := config.BuildSyncPlan(*dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcpctl: %v\n", err)
		return 2
	}
	printSyncPreview(syncPlan)
	if len(syncPlan.Conflicts) > 0 {
		fmt.Fprintln(os.Stderr, "mcpctl: unresolved conflicts block sync; resolve the definitions or use the TUI")
		return 2
	}
	if !syncPlanChanged(syncPlan) {
		return 0
	}
	if *dryRun {
		return 1
	}
	if !*yes {
		fmt.Fprint(os.Stderr, "Apply these changes? [y/N] ")
		var answer string
		if _, err := fmt.Fscan(os.Stdin, &answer); err != nil || strings.ToLower(answer) != "y" {
			fmt.Fprintln(os.Stderr, "sync cancelled")
			return 0
		}
	}
	if _, err := config.Apply(syncPlan.Plan); err != nil {
		fmt.Fprintf(os.Stderr, "mcpctl: sync failed: %v\n", err)
		return 2
	}
	fmt.Println("sync applied")
	return 0
}

func syncPlanChanged(s *config.SyncPlan) bool {
	return s.Plan.Claude.Changed || s.Plan.OpenCode.Changed || s.Plan.Codex.Changed
}

func printSyncPreview(syncPlan *config.SyncPlan) {
	for _, conflict := range syncPlan.Conflicts {
		fmt.Printf("conflict: %s (%s)\n", conflict.Name, strings.Join(conflict.Clients, ", "))
	}
	for _, cp := range []*config.ClientPlan{syncPlan.Plan.Claude, syncPlan.Plan.OpenCode, syncPlan.Plan.Codex} {
		if cp.Changed {
			fmt.Printf("would update %s\n", cp.Path)
		} else {
			fmt.Printf("unchanged %s\n", cp.Path)
		}
	}
	for _, warning := range syncPlan.Plan.Warnings {
		fmt.Printf("warning: %s/%s: %s\n", warning.Client, warning.ServerName, warning.Detail)
	}
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	asJSON := fs.Bool("json", false, "print machine-readable JSON")
	timeout := fs.Duration("timeout", 15*time.Second, "per-command timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report, err := status.Check(context.Background(), *dir, *timeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcpctl: %v\n", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "mcpctl: %v\n", err)
			return 2
		}
		return exitCodeFor(report)
	}

	printReport(report)
	return exitCodeFor(report)
}

func printReport(report *status.Report) {
	fmt.Printf("claude:   %s\n", availabilityLine(report.Claude.Availability))
	fmt.Printf("opencode: %s\n", availabilityLine(report.OpenCode.Availability))
	fmt.Printf("codex:    %s\n", availabilityLine(report.Codex.Availability))
	fmt.Println()

	byServer := map[string]map[string]status.Result{}
	order := []string{}
	addResults := func(clientName string, results []status.Result) {
		for _, r := range results {
			if _, ok := byServer[r.ServerName]; !ok {
				byServer[r.ServerName] = map[string]status.Result{}
				order = append(order, r.ServerName)
			}
			byServer[r.ServerName][clientName] = r
		}
	}
	addResults("claude", report.Claude.Results)
	addResults("opencode", report.OpenCode.Results)
	addResults("codex", report.Codex.Results)
	sort.Strings(order)

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, name := range order {
		fmt.Fprintf(tw, "%s\n", name)
		for _, clientName := range []string{"claude", "opencode", "codex"} {
			r, ok := byServer[name][clientName]
			if !ok {
				continue
			}
			fmt.Fprintf(tw, "  %s\tconfig=%s\tconn=%s\tauth=%s\tcheck=%s\tscope=%s\n",
				clientName, r.ConfigState, r.Connection, r.AuthState, r.CheckState, r.Scope)
			if len(r.Evidence) > 0 {
				if ev := r.Evidence[len(r.Evidence)-1]; ev.Summary != "" {
					fmt.Fprintf(tw, "    \t%s\n", ev.Summary)
				}
			}
		}
	}
	tw.Flush()
}

func availabilityLine(a client.Availability) string {
	if !a.Present {
		return "not installed"
	}
	return a.Version
}

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project directory")
	asJSON := fs.Bool("json", false, "print machine-readable JSON")
	fix := fs.Bool("fix", false, "offer safe automatic fixes")
	yes := fs.Bool("yes", false, "apply fixes without prompting")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report, err := status.RunDoctor(context.Background(), *dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcpctl: %v\n", err)
		return 2
	}

	if *asJSON {
		if *fix {
			fmt.Fprintln(os.Stderr, "mcpctl: --fix cannot be combined with --json")
			return 2
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "mcpctl: %v\n", err)
			return 2
		}
		return exitCodeForDoctor(report)
	}

	printDoctorReport(report)
	if *fix {
		path, oldRaw, newRaw, changed, err := config.PrepareOpenCodeEnvFix(*dir)
		if err == nil && changed {
			fmt.Printf("\nSafe fix available for %s:\n", path)
			fmt.Println("  replace shell-style ${VAR} references with OpenCode {env:VAR} references")
			if !*yes {
				fmt.Fprint(os.Stderr, "Apply this fix? [y/N] ")
				var answer string
				if _, scanErr := fmt.Fscan(os.Stdin, &answer); scanErr != nil || strings.ToLower(answer) != "y" {
					fmt.Fprintln(os.Stderr, "fix cancelled")
					return exitCodeForDoctor(report)
				}
			}
			backup, applyErr := config.ApplyOpenCodeEnvFix(path, oldRaw, newRaw)
			if applyErr != nil {
				fmt.Fprintf(os.Stderr, "mcpctl: fix failed: %v\n", applyErr)
				return 2
			}
			fmt.Printf("fixed; backup saved to %s\n", backup)
			updated, checkErr := status.RunDoctor(context.Background(), *dir)
			if checkErr != nil {
				fmt.Fprintf(os.Stderr, "mcpctl: recheck failed: %v\n", checkErr)
				return 2
			}
			fmt.Println("\nRecheck:")
			printDoctorReport(updated)
			return exitCodeForDoctor(updated)
		}
	}
	return exitCodeForDoctor(report)
}

func printDoctorReport(report *status.DoctorReport) {
	s := report.Summary
	fmt.Printf("clients: %d/%d available · servers checked: %d · errors: %d · warnings: %d\n\n",
		s.ClientsAvailable, s.ClientsTotal, s.ServersChecked, s.Errors, s.Warnings)

	if len(report.Findings) == 0 {
		fmt.Println("no issues found")
		return
	}

	for _, f := range report.Findings {
		loc := f.Client
		if f.ServerName != "" {
			if loc != "" {
				loc += "/"
			}
			loc += f.ServerName
		}
		if loc != "" {
			loc = " [" + loc + "]"
		}
		fmt.Printf("%-7s %s%s: %s\n", severityLabel(f.Severity), f.Category, loc, f.Message)
		if f.NextStep != "" {
			fmt.Printf("        -> %s\n", f.NextStep)
		}
	}
}

func severityLabel(s status.DoctorSeverity) string {
	switch s {
	case status.SeverityError:
		return "ERROR"
	case status.SeverityWarning:
		return "WARN"
	default:
		return "info"
	}
}

// exitCodeForDoctor returns 1 if doctor found any error- or
// warning-level issue, else 0. RunDoctor itself only fails (handled by
// the caller as exit 2) on an invocation-level problem such as an
// unreadable directory.
func exitCodeForDoctor(report *status.DoctorReport) int {
	if report.Summary.Errors > 0 || report.Summary.Warnings > 0 {
		return 1
	}
	return 0
}

// exitCodeFor maps a Report to mcpctl's documented exit codes: 2 for any
// incomplete check (it takes precedence over diagnosed issues), else 1
// if any result shows a diagnosed problem, else 0.
func exitCodeFor(report *status.Report) int {
	all := append(append(append([]status.Result{}, report.Claude.Results...), report.OpenCode.Results...), report.Codex.Results...)

	sawIssue := false
	for _, r := range all {
		if r.CheckState != status.CheckComplete {
			return 2
		}
		switch {
		case r.ConfigState == status.ConfigRejected, r.ConfigState == status.ConfigDisabled:
			sawIssue = true
		case r.Connection == status.ConnectionFailed, r.Connection == status.ConnectionTimedOut:
			sawIssue = true
		}
	}
	if sawIssue {
		return 1
	}
	return 0
}
