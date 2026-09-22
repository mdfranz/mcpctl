package main

import (
	"testing"

	"github.com/mdfranz/mcpctl/internal/status"
)

func TestResultDisplay(t *testing.T) {
	tests := []struct {
		name   string
		result status.Result
		want   string
		text   string
	}{
		{"connected", status.Result{ConfigState: status.ConfigPresent, Connection: status.ConnectionConnected, CheckState: status.CheckComplete}, "✔", "Connected"},
		{"disabled", status.Result{ConfigState: status.ConfigDisabled, CheckState: status.CheckComplete}, "⊘", "Disabled for this project"},
		{"failed", status.Result{ConfigState: status.ConfigPresent, Connection: status.ConnectionFailed, CheckState: status.CheckComplete}, "✘", "Connection failed"},
		{"unchecked", status.Result{ConfigState: status.ConfigPresent, Connection: status.ConnectionUnchecked, CheckState: status.CheckComplete}, "?", "Configured; connection unchecked"},
		{"incomplete", status.Result{ConfigState: status.ConfigPresent, Connection: status.ConnectionFailed, CheckState: status.CheckFailed}, "?", "Status unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotText := resultDisplay(tt.result)
			if got != tt.want || gotText != tt.text {
				t.Fatalf("resultDisplay() = %q, %q; want %q, %q", got, gotText, tt.want, tt.text)
			}
		})
	}
}

func TestSourceDisplay(t *testing.T) {
	if got := sourceDisplay(status.Result{Source: status.SourceProject, SourceConfidence: status.SourceConfirmed}); got != "source=project" {
		t.Errorf("sourceDisplay(confirmed) = %q", got)
	}
	if got := sourceDisplay(status.Result{Source: status.SourceProject, SourceConfidence: status.SourceInferred}); got != "source=project (inferred)" {
		t.Errorf("sourceDisplay(inferred) = %q", got)
	}
	if got := sourceDisplay(status.Result{Source: status.SourceUnknown}); got != "source=unknown" {
		t.Errorf("sourceDisplay(unknown) = %q", got)
	}
}
