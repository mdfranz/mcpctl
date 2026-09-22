package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Span is a half-open byte range [Start, End) in a source document.
type Span struct{ Start, End int }

// tomlHeader is one `[a.b.c]` table header line found in a Codex config
// file, outside any multiline string.
type tomlHeader struct {
	path      []string // dot-split, unquoted key path, e.g. ["mcp_servers", "files", "env"]
	lineStart int      // byte offset of the start of this header's line
	blockEnd  int      // byte offset where this header's owned block ends:
	// the start of the next header's line, or len(raw). Header blocks
	// partition the file with no gaps between them, which is what lets
	// ApplyCodexEdit keep, drop, or replace each one independently.
}

type rawLine struct {
	start, end int
	text       string
}

func splitKeepOffsets(raw []byte) []rawLine {
	var lines []rawLine
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			lines = append(lines, rawLine{start: start, end: i + 1, text: string(raw[start : i+1])})
			start = i + 1
		}
	}
	if start < len(raw) {
		lines = append(lines, rawLine{start: start, end: len(raw), text: string(raw[start:len(raw)])})
	}
	return lines
}

// scanTomlHeaders walks raw and returns every single-bracket `[...]` table
// header found outside multiline strings, in document order. It is a
// line-oriented scanner, not a full TOML parser: it tracks triple-quoted
// multiline string state across lines so header-shaped text inside a
// multiline string isn't mistaken for a real header. `[[...]]`
// array-of-tables headers are left unrecognized (ok stays true, they're
// just never reported as headers) since mcp_servers is never modeled as
// one.
func scanTomlHeaders(raw []byte) (headers []tomlHeader, ok bool, reason string) {
	lines := splitKeepOffsets(raw)
	inMultiline := false
	var marker string

	var headerLineIdx []int
	var headerPaths [][]string

	for idx, ln := range lines {
		line := strings.TrimRight(ln.text, "\r\n")

		if inMultiline {
			if strings.Contains(line, marker) {
				inMultiline = false
			}
			continue
		}

		path, isHeader, err := parseHeaderLine(line)
		if err != nil {
			return nil, false, fmt.Sprintf("line %d: %v", idx+1, err)
		}
		if isHeader {
			headerLineIdx = append(headerLineIdx, idx)
			headerPaths = append(headerPaths, path)
			continue
		}

		if strings.Count(line, `"""`)%2 == 1 {
			inMultiline, marker = true, `"""`
		} else if strings.Count(line, `'''`)%2 == 1 {
			inMultiline, marker = true, `'''`
		}
	}

	// A header's owned block starts at its `[...]` line by default, but a
	// contiguous run of blank/comment lines immediately above it is
	// pulled in too: those lines document the table that follows, so
	// replacing that table should replace its leading comment as well.
	// The walk-back is bounded by the previous header's own line so it
	// never eats into the previous table's content.
	headers = make([]tomlHeader, len(headerLineIdx))
	for i, idx := range headerLineIdx {
		lowerBound := 0
		if i > 0 {
			lowerBound = headerLineIdx[i-1] + 1
		}
		start := idx
		for start > lowerBound {
			prev := strings.TrimSpace(strings.TrimRight(lines[start-1].text, "\r\n"))
			if prev == "" || strings.HasPrefix(prev, "#") {
				start--
				continue
			}
			break
		}
		headers[i] = tomlHeader{path: headerPaths[i], lineStart: lines[start].start}
	}

	for i := range headers {
		if i+1 < len(headers) {
			headers[i].blockEnd = headers[i+1].lineStart
		} else {
			headers[i].blockEnd = len(raw)
		}
	}
	return headers, true, ""
}

// parseHeaderLine reports whether line is a simple `[a.b.c]` table header
// (optionally followed only by a trailing comment), and if so its dotted
// path. Lines with any other trailing content, or `[[...]]`
// array-of-table headers, are reported as isHeader == false so the caller
// treats them as ordinary content rather than guessing.
func parseHeaderLine(line string) (path []string, isHeader bool, err error) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "[[") {
		return nil, false, nil
	}

	i := 1
	inQuote := byte(0)
	for i < len(trimmed) {
		c := trimmed[i]
		if inQuote != 0 {
			if c == '\\' && inQuote == '"' && i+1 < len(trimmed) {
				i += 2
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			i++
			continue
		}
		if c == ']' {
			break
		}
		i++
	}
	if inQuote != 0 || i >= len(trimmed) || trimmed[i] != ']' {
		return nil, false, nil
	}

	inner := trimmed[1:i]
	rest := strings.TrimSpace(trimmed[i+1:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return nil, false, nil
	}

	p, err := splitTomlKeyPath(inner)
	if err != nil {
		return nil, true, fmt.Errorf("malformed table header %q: %w", trimmed, err)
	}
	return p, true, nil
}

func splitTomlKeyPath(s string) ([]string, error) {
	var parts []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			cur.WriteByte(c)
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
			cur.WriteByte(c)
		case c == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if inQuote != 0 {
		return nil, fmt.Errorf("unterminated quote in key path %q", s)
	}
	parts = append(parts, strings.TrimSpace(cur.String()))

	out := make([]string, len(parts))
	for i, p := range parts {
		unq, err := unquoteTomlKey(p)
		if err != nil {
			return nil, err
		}
		out[i] = unq
	}
	return out, nil
}

func unquoteTomlKey(p string) (string, error) {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		return strings.ReplaceAll(p[1:len(p)-1], `\"`, `"`), nil
	}
	if len(p) >= 2 && p[0] == '\'' && p[len(p)-1] == '\'' {
		return p[1 : len(p)-1], nil
	}
	if p == "" {
		return "", fmt.Errorf("empty key segment")
	}
	return p, nil
}

func headerBelongsToServer(h tomlHeader, name string) bool {
	return len(h.path) >= 2 && h.path[0] == "mcp_servers" && h.path[1] == name
}

func codexServerSpans(headers []tomlHeader, name string) []Span {
	var spans []Span
	for _, h := range headers {
		if headerBelongsToServer(h, name) {
			spans = append(spans, Span{h.lineStart, h.blockEnd})
		}
	}
	return spans
}

// CheckCodexEditable reports an error if any name in names that is marked
// existing has no locatable `[mcp_servers.name...]` header span in raw —
// meaning it's defined through a layout (e.g. an inline table under a
// bare [mcp_servers] table) that ApplyCodexEdit cannot safely edit
// without risking silent data loss. Report its location and refuse
// rather than guess.
func CheckCodexEditable(raw []byte, existing map[string]bool) error {
	headers, ok, reason := scanTomlHeaders(raw)
	if !ok {
		return fmt.Errorf("cannot parse TOML structure: %s", reason)
	}
	names := make([]string, 0, len(existing))
	for name, isExisting := range existing {
		if isExisting {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if len(codexServerSpans(headers, name)) == 0 {
			return fmt.Errorf("server %q is defined in a layout mcpctl cannot safely edit (e.g. an inline table under [mcp_servers]); resolve it manually before saving", name)
		}
	}
	return nil
}

// ApplyCodexEdit returns the new file bytes for raw after upserting each
// server named in upsert (rendered block text, as produced by
// renderCodexServerBlock) and deleting each server named in deletes.
//
// Because table-header blocks partition the file with no gaps between
// them, this walks headers once in document order, keeping each
// untouched block byte-for-byte, dropping every span owned by a deleted
// or replaced server, and splicing in each replacement once at the
// position of its first owned span (or appending it at the end of the
// mcp_servers region for a brand-new server). Comments and formatting
// inside a table being replaced are not preserved — only the surrounding,
// untouched document is guaranteed byte-identical.
func ApplyCodexEdit(raw []byte, upsert map[string]string, deletes []string) ([]byte, error) {
	headers, ok, reason := scanTomlHeaders(raw)
	if !ok {
		return nil, fmt.Errorf("cannot parse TOML structure: %s", reason)
	}

	del := make(map[string]bool, len(deletes))
	for _, n := range deletes {
		del[n] = true
	}

	var out []byte
	if len(headers) > 0 {
		out = append(out, raw[:headers[0].lineStart]...)
	} else {
		out = append(out, raw...)
	}

	inserted := make(map[string]bool, len(upsert))
	emit := func(name string) {
		block := upsert[name]
		if len(out) > 0 {
			if !bytes.HasSuffix(out, []byte("\n")) {
				out = append(out, '\n')
			}
			if !bytes.HasSuffix(out, []byte("\n\n")) {
				out = append(out, '\n')
			}
		}
		out = append(out, []byte(block)...)
		if !strings.HasSuffix(block, "\n") {
			out = append(out, '\n')
		}
		inserted[name] = true
	}

	for _, h := range headers {
		owner := ""
		if len(h.path) >= 2 && h.path[0] == "mcp_servers" {
			owner = h.path[1]
		}
		if owner != "" && del[owner] {
			continue
		}
		if owner != "" {
			if _, isUpsert := upsert[owner]; isUpsert {
				if !inserted[owner] {
					emit(owner)
				}
				continue
			}
		}
		out = append(out, raw[h.lineStart:h.blockEnd]...)
	}

	var newNames []string
	for name := range upsert {
		if !inserted[name] {
			newNames = append(newNames, name)
		}
	}
	sort.Strings(newNames)
	for _, name := range newNames {
		emit(name)
	}

	var check map[string]interface{}
	if err := toml.Unmarshal(out, &check); err != nil {
		return nil, fmt.Errorf("internal error: edited TOML failed to reparse: %w", err)
	}
	return out, nil
}
