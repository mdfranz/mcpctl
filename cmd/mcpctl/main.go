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
	"strings"
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
  status    show project MCP server definitions across all clients
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

	var report *status.Report
	var err error
	if *asJSON {
		report, err = status.Check(context.Background(), *dir, *timeout)
	} else {
		progressDone := make(chan struct{})
		go showStatusProgress(progressDone)
		report, err = status.Check(context.Background(), *dir, *timeout)
		close(progressDone)
		// Finish the progress line before printing the report.
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
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

func showStatusProgress(done <-chan struct{}) {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	i := 0
	fmt.Fprintf(os.Stderr, "%s Checking Claude, OpenCode, and Codex...", frames[i])
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			i = (i + 1) % len(frames)
			fmt.Fprintf(os.Stderr, "\r%s Checking Claude, OpenCode, and Codex...", frames[i])
		}
	}
}

func printReport(report *status.Report) {
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
				if r.Target != "" {
					fmt.Printf("  %s %-30s %-30s %s\n", symbol, r.ServerName, text, r.Target)
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
