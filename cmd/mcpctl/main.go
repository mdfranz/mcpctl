// Command mcpctl manages MCP server definitions shared across Claude
// Code, Codex CLI, and OpenCode project configuration files.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/mdfranz/mcpctl/internal/client"
	"github.com/mdfranz/mcpctl/internal/status"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "mcpctl: TUI not implemented yet; try `mcpctl status`")
		return 2
	}

	switch args[0] {
	case "status":
		return runStatus(args[1:])
	case "doctor":
		return runDoctor(args[1:])
	case "-h", "--help", "help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "mcpctl: unknown command %q\n", args[0])
		printUsage()
		return 2
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: mcpctl <command> [flags]

commands:
  status    show live project MCP server status across all clients
  doctor    local preflight: config syntax, executables, env vars, URLs
  help      show this message`)
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
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report, err := status.RunDoctor(context.Background(), *dir)
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
		return exitCodeForDoctor(report)
	}

	printDoctorReport(report)
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
