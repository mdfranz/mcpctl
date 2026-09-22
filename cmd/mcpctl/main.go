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
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	printProjectContext(report.Dir)
	fmt.Println()

	clients := []struct {
		name   string
		report status.ClientReport
	}{
		{"Claude", report.Claude},
		{"OpenCode", report.OpenCode},
		{"Codex", report.Codex},
	}
	for i, c := range clients {
		fmt.Printf("%s (%s)\n", c.name, compactVersion(c.report.Availability))
		results := append([]status.Result(nil), c.report.Results...)
		sort.Slice(results, func(i, j int) bool { return results[i].ServerName < results[j].ServerName })
		if len(results) == 0 {
			fmt.Println("  No project servers")
		} else {
			for _, r := range results {
				symbol, text := resultDisplay(r)
				if source := sourceDisplay(r); source != "" {
					text += " · " + source
				}
				if r.Target != "" {
					fmt.Printf("  %s %-30s %-32s %s\n", symbol, r.ServerName, text, r.Target)
				} else {
					fmt.Printf("  %s %-30s %s\n", symbol, r.ServerName, text)
				}
			}
		}
		if i < len(clients)-1 {
			fmt.Println()
		}
	}
}

func printProjectContext(dir string) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	fmt.Printf("Project: %s\n", absDir)

	gitState := readGitState(dir)
	if gitState == "" {
		fmt.Println("Git:     not a repository")
		return
	}
	parts := strings.SplitN(gitState, "\n", 2)
	branch := strings.TrimPrefix(parts[0], "## ")
	if len(parts) == 1 || strings.TrimSpace(parts[1]) == "" {
		fmt.Printf("Git:     %s (clean)\n", branch)
		return
	}
	fmt.Printf("Git:     %s (modified)\n", branch)
}

func readGitState(dir string) string {
	cmd := exec.Command("git", "-C", dir, "status", "--short", "--branch")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sourceDisplay(r status.Result) string {
	if r.Source == status.SourceUnknown || r.Source == "" {
		return "source=unknown"
	}
	label := "source=" + string(r.Source)
	if r.SourceConfidence == status.SourceInferred {
		label += " (inferred)"
	}
	if r.SourcePath != "" {
		path := r.SourcePath
		if home, err := os.UserHomeDir(); err == nil {
			if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				path = "~/" + filepath.ToSlash(rel)
			}
		}
		label += " (" + path + ")"
	}
	return label
}

func resultDisplay(r status.Result) (string, string) {
	if r.CheckState != status.CheckComplete {
		return "?", "Status unavailable"
	}
	switch {
	case r.ConfigState == status.ConfigDisabled:
		return "⊘", "Disabled for this project"
	case r.ConfigState == status.ConfigRejected:
		return "⊘", "Rejected"
	case r.ConfigState == status.ConfigPendingApproval:
		return "⏸", "Pending approval"
	case r.Connection == status.ConnectionConnected:
		return "✔", "Connected"
	case r.Connection == status.ConnectionTimedOut:
		return "✘", "Connection timed out"
	case r.Connection == status.ConnectionFailed:
		return "✘", "Connection failed"
	case r.AuthState == status.AuthRequired:
		return "!", "Authentication required"
	case r.Connection == status.ConnectionUnchecked:
		return "?", "Configured; connection unchecked"
	default:
		return "?", "Status unknown"
	}
}

func compactVersion(a client.Availability) string {
	if !a.Present {
		return "not installed"
	}
	fields := strings.Fields(a.Version)
	if len(fields) == 0 {
		return "available"
	}
	if a.Name == "claude" || a.Name == "opencode" {
		return fields[0]
	}
	return fields[len(fields)-1]
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
