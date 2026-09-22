package tui

import (
	"testing"

	"github.com/mdfranz/mcpctl/internal/config"
)

func TestParseEnvLines_AllForms(t *testing.T) {
	env, err := parseEnvLines([]string{
		"MODE=debug",
		"API_KEY=$",
		"REGION=$:us-east-1",
		`LITERAL_DOLLAR=\$5.00`,
		"",
		"  ",
	})
	if err != nil {
		t.Fatalf("parseEnvLines: %v", err)
	}
	byName := map[string]config.EnvVar{}
	for _, e := range env {
		byName[e.Name] = e
	}
	if len(byName) != 4 {
		t.Fatalf("got %d entries, want 4: %+v", len(byName), byName)
	}

	mode := byName["MODE"]
	if mode.Kind != config.EnvVarLiteral || mode.Value != "debug" {
		t.Errorf("MODE = %+v", mode)
	}
	apiKey := byName["API_KEY"]
	if apiKey.Kind != config.EnvVarPassthrough || apiKey.Default != nil {
		t.Errorf("API_KEY = %+v", apiKey)
	}
	region := byName["REGION"]
	if region.Kind != config.EnvVarPassthrough || region.Default == nil || *region.Default != "us-east-1" {
		t.Errorf("REGION = %+v", region)
	}
	lit := byName["LITERAL_DOLLAR"]
	if lit.Kind != config.EnvVarLiteral || lit.Value != "$5.00" {
		t.Errorf("LITERAL_DOLLAR = %+v, want literal $5.00", lit)
	}
}

func TestParseEnvLines_ValueMayContainEquals(t *testing.T) {
	env, err := parseEnvLines([]string{"CONN=host=localhost;port=5432"})
	if err != nil {
		t.Fatalf("parseEnvLines: %v", err)
	}
	if len(env) != 1 || env[0].Value != "host=localhost;port=5432" {
		t.Fatalf("env = %+v", env)
	}
}

func TestParseEnvLines_MissingEqualsIsError(t *testing.T) {
	if _, err := parseEnvLines([]string{"NOEQUALS"}); err == nil {
		t.Fatalf("expected an error for a line with no '='")
	}
}

func TestEnvLines_RoundTrip(t *testing.T) {
	def := "us-east-1"
	original := []config.EnvVar{
		{Name: "MODE", Kind: config.EnvVarLiteral, Value: "debug"},
		{Name: "API_KEY", Kind: config.EnvVarPassthrough},
		{Name: "REGION", Kind: config.EnvVarPassthrough, Default: &def},
		{Name: "WEIRD", Kind: config.EnvVarLiteral, Value: "$not-a-passthrough"},
	}
	lines := renderEnvLines(original)
	roundTripped, err := parseEnvLines(lines)
	if err != nil {
		t.Fatalf("parseEnvLines(render(...)): %v", err)
	}
	if !envVarsEqualForTest(original, roundTripped) {
		t.Errorf("round trip mismatch:\noriginal:      %+v\nround-tripped: %+v\nlines: %v", original, roundTripped, lines)
	}
}

func envVarsEqualForTest(a, b []config.EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	am := map[string]config.EnvVar{}
	for _, e := range a {
		am[e.Name] = e
	}
	for _, e := range b {
		o, ok := am[e.Name]
		if !ok || o.Kind != e.Kind || o.Value != e.Value {
			return false
		}
		if (o.Default == nil) != (e.Default == nil) {
			return false
		}
		if o.Default != nil && *o.Default != *e.Default {
			return false
		}
	}
	return true
}

func TestParseHeaderLines_AllForms(t *testing.T) {
	headers, err := parseHeaderLines([]string{
		"X-Client=mcpctl",
		"Authorization=$SEARCH_TOKEN",
		`X-Weird=\$5`,
	})
	if err != nil {
		t.Fatalf("parseHeaderLines: %v", err)
	}
	if len(headers) != 3 {
		t.Fatalf("headers = %+v", headers)
	}
	if h := headers["X-Client"]; h.Kind != config.EnvVarLiteral || h.Value != "mcpctl" {
		t.Errorf("X-Client = %+v", h)
	}
	if h := headers["Authorization"]; h.Kind != config.EnvVarPassthrough || h.EnvName != "SEARCH_TOKEN" {
		t.Errorf("Authorization = %+v", h)
	}
	if h := headers["X-Weird"]; h.Kind != config.EnvVarLiteral || h.Value != "$5" {
		t.Errorf("X-Weird = %+v", h)
	}
}

func TestParseHeaderLines_BareDollarIsError(t *testing.T) {
	if _, err := parseHeaderLines([]string{"Authorization=$"}); err == nil {
		t.Fatalf("expected an error for passthrough with no env var name")
	}
}

func TestHeaderLines_RoundTrip(t *testing.T) {
	original := map[string]config.HeaderValue{
		"X-Client":      {Kind: config.EnvVarLiteral, Value: "mcpctl"},
		"Authorization": {Kind: config.EnvVarPassthrough, EnvName: "SEARCH_TOKEN"},
	}
	lines := renderHeaderLines(original)
	roundTripped, err := parseHeaderLines(lines)
	if err != nil {
		t.Fatalf("parseHeaderLines(render(...)): %v", err)
	}
	if len(roundTripped) != len(original) {
		t.Fatalf("round trip mismatch: %+v vs %+v", original, roundTripped)
	}
	for name, want := range original {
		got, ok := roundTripped[name]
		if !ok || got != want {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
		}
	}
}
