package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mdfranz/mcpctl/internal/config"
)

// Compact line grammar used by the env/header form fields, one entry per
// line as "NAME=VALUE":
//
//	NAME=value      literal value
//	NAME=$          passthrough from the OS env var of the same name
//	NAME=$:default  passthrough with a default
//	NAME=\$literal  a literal value that itself starts with "$"
//
// (config.EnvVar's passthrough always reads the OS var matching its own
// Name -- there's no separate source-name field -- so unlike headers, env
// lines can't redirect to a differently-named var.)

func parseEnvLines(lines []string) ([]config.EnvVar, error) {
	var out []config.EnvVar
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, err := splitLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		ev := config.EnvVar{Name: name}
		switch {
		case value == "$":
			ev.Kind = config.EnvVarPassthrough
		case strings.HasPrefix(value, "$:"):
			def := value[2:]
			ev.Kind = config.EnvVarPassthrough
			ev.Default = &def
		case strings.HasPrefix(value, `\$`):
			ev.Kind = config.EnvVarLiteral
			ev.Value = value[1:]
		default:
			ev.Kind = config.EnvVarLiteral
			ev.Value = value
		}
		out = append(out, ev)
	}
	return out, nil
}

func renderEnvLines(env []config.EnvVar) []string {
	names := make([]string, len(env))
	byName := make(map[string]config.EnvVar, len(env))
	for i, ev := range env {
		names[i] = ev.Name
		byName[ev.Name] = ev
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		ev := byName[name]
		switch {
		case ev.Kind == config.EnvVarPassthrough && ev.Default != nil:
			lines = append(lines, fmt.Sprintf("%s=$:%s", name, *ev.Default))
		case ev.Kind == config.EnvVarPassthrough:
			lines = append(lines, name+"=$")
		case strings.HasPrefix(ev.Value, "$"):
			lines = append(lines, name+`=\`+ev.Value)
		default:
			lines = append(lines, name+"="+ev.Value)
		}
	}
	return lines
}

// Header lines use the same "$" convention, but passthrough can name a
// different OS env var than the header ("HEADERNAME=$ENV_VAR_NAME"),
// matching config.HeaderValue's separate EnvName field. Headers have no
// default-value concept.
func parseHeaderLines(lines []string) (map[string]config.HeaderValue, error) {
	out := map[string]config.HeaderValue{}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, err := splitLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		switch {
		case strings.HasPrefix(value, `\$`):
			out[name] = config.HeaderValue{Kind: config.EnvVarLiteral, Value: value[1:]}
		case strings.HasPrefix(value, "$"):
			envName := value[1:]
			if envName == "" {
				return nil, fmt.Errorf("line %d: header passthrough needs an env var name after $", i+1)
			}
			out[name] = config.HeaderValue{Kind: config.EnvVarPassthrough, EnvName: envName}
		default:
			out[name] = config.HeaderValue{Kind: config.EnvVarLiteral, Value: value}
		}
	}
	return out, nil
}

func renderHeaderLines(headers map[string]config.HeaderValue) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)

	lines := make([]string, 0, len(names))
	for _, name := range names {
		h := headers[name]
		switch {
		case h.Kind == config.EnvVarPassthrough:
			lines = append(lines, name+"=$"+h.EnvName)
		case strings.HasPrefix(h.Value, "$"):
			lines = append(lines, name+`=\`+h.Value)
		default:
			lines = append(lines, name+"="+h.Value)
		}
	}
	return lines
}

func splitLine(line string) (name, value string, err error) {
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", fmt.Errorf("expected NAME=VALUE, got %q", line)
	}
	name = strings.TrimSpace(line[:idx])
	if name == "" {
		return "", "", fmt.Errorf("empty name in %q", line)
	}
	return name, line[idx+1:], nil
}
