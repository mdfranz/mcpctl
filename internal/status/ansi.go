package status

import "regexp"

var ansiSequence = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// stripANSI removes SGR/cursor escape sequences (e.g. "\x1b[90m") from
// CLI output so line-based parsing doesn't have to account for them.
func stripANSI(s string) string {
	return ansiSequence.ReplaceAllString(s, "")
}
