// Package redact applies a best-effort mask over obvious secret-shaped
// substrings in diagnostic text before it's stored as Evidence or shown
// to the user. It is not a substitute for not capturing secrets in the
// first place, and it is not exhaustive: callers should still prefer
// summarizing subprocess output over embedding it verbatim.
package redact

import "regexp"

var (
	bearerAuth = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	longToken  = regexp.MustCompile(`[A-Za-z0-9_-]{32,}`)
)

// Summary masks bearer-token-shaped and other long opaque-token-shaped
// substrings in s.
func Summary(s string) string {
	out := bearerAuth.ReplaceAllString(s, "${1}[REDACTED]")
	out = longToken.ReplaceAllString(out, "[REDACTED]")
	return out
}
