package config

import "regexp"

// claudeVarPattern matches Claude Code's "${VAR}" / "${VAR:-default}" env
// and header value syntax.
var claudeVarPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)(:-(.*))?\}$`)

// parseClaudeValue parses a single Claude-style string value into its
// literal or passthrough parts.
func parseClaudeValue(s string) (kind EnvVarKind, envName string, def *string, literal string) {
	if m := claudeVarPattern.FindStringSubmatch(s); m != nil {
		envName = m[1]
		kind = EnvVarPassthrough
		if m[2] != "" {
			d := m[3]
			def = &d
		}
		return
	}
	return EnvVarLiteral, "", nil, s
}

// renderClaudeValue is the inverse of parseClaudeValue.
func renderClaudeValue(kind EnvVarKind, envName string, def *string, literal string) string {
	if kind == EnvVarLiteral {
		return literal
	}
	if def != nil {
		return "${" + envName + ":-" + *def + "}"
	}
	return "${" + envName + "}"
}

// opencodeVarPattern matches OpenCode's "{env:VAR}" passthrough syntax.
// OpenCode has no default-value syntax; a passthrough default can't be
// represented and must surface as a portability warning on save.
var opencodeVarPattern = regexp.MustCompile(`^\{env:([A-Za-z_][A-Za-z0-9_]*)\}$`)

func parseOpenCodeValue(s string) (kind EnvVarKind, envName string, literal string) {
	if m := opencodeVarPattern.FindStringSubmatch(s); m != nil {
		return EnvVarPassthrough, m[1], ""
	}
	return EnvVarLiteral, "", s
}

func renderOpenCodeValue(kind EnvVarKind, envName string, literal string) string {
	if kind == EnvVarLiteral {
		return literal
	}
	return "{env:" + envName + "}"
}
