package vwt

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrNoDiffFound = errors.New("no diff content found")

func ExtractDiff(in []byte) ([]byte, error) {
	// Accept:
	// - raw unified diffs
	// - Markdown with a single fenced ```diff block
	// - leading prose before the first diff header

	if len(in) == 0 {
		return nil, ErrNoDiffFound
	}

	// First: fenced ```diff ... ```
	if diff := extractFencedDiff(in); diff != nil {
		diff = normalizeDiff(diff)
		if len(bytes.Trim(diff, "\n")) == 0 {
			return nil, ErrNoDiffFound
		}
		return diff, nil
	}

	// Second: trim leading content until first diff header.
	lines := bytes.Split(in, []byte("\n"))
	start := -1
	for i, ln := range lines {
		l := bytes.TrimSpace(ln)
		if bytes.HasPrefix(l, []byte("diff --git ")) || bytes.HasPrefix(l, []byte("--- ")) {
			start = i
			break
		}
	}
	if start >= 0 {
		out := bytes.Join(lines[start:], []byte("\n"))
		out = normalizeDiff(out)
		if len(bytes.Trim(out, "\n")) == 0 {
			return nil, ErrNoDiffFound
		}
		return out, nil
	}

	// Fallback: return trimmed input.
	out := normalizeDiff(in)
	if len(bytes.Trim(out, "\n")) == 0 {
		return nil, ErrNoDiffFound
	}
	return out, nil
}

func normalizeDiff(in []byte) []byte {
	// Ensure patch parses reliably:
	// - normalize CRLF to LF
	// - remove extra leading blank newlines
	// - ensure the final byte is a newline (git apply treats missing final newline as a corrupt patch)
	out := bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n"))
	out = bytes.ReplaceAll(out, []byte("\r"), []byte("\n"))
	out = bytes.TrimLeft(out, "\n")
	if len(out) == 0 {
		return out
	}
	if out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out
}

func PatchSHA256Hex(diff []byte) string {
	h := sha256.Sum256(diff)
	return hex.EncodeToString(h[:])
}

func ValidatePatchPaths(diff []byte) error {
	paths, err := touchedPaths(diff)
	if err != nil {
		return err
	}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" || p == "/dev/null" {
			continue
		}
		if strings.ContainsAny(p, "\x00\r\n") {
			return fmt.Errorf("refusing patch with invalid path characters: %q", p)
		}

		// Normalize common diff prefixes.
		if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
			p = p[2:]
		}
		for strings.HasPrefix(p, "./") {
			p = strings.TrimPrefix(p, "./")
		}

		if strings.HasPrefix(p, "/") {
			return fmt.Errorf("refusing patch with absolute path: %s", p)
		}
		for _, seg := range strings.Split(p, "/") {
			if seg == ".." {
				return fmt.Errorf("refusing patch with unsafe path: %s", p)
			}
		}
		if p == ".git" || strings.HasPrefix(p, ".git/") {
			return fmt.Errorf("refusing patch that touches .git/**")
		}
	}
	return nil
}

func touchedPaths(diff []byte) ([]string, error) {
	// Best-effort scan for file paths in a unified diff.
	// Primary source: `diff --git a/X b/Y` lines.
	// Secondary: `--- a/X` and `+++ b/Y` lines.

	out := make([]string, 0, 16)
	seen := make(map[string]struct{}, 16)

	s := bufio.NewScanner(bytes.NewReader(diff))
	buf := make([]byte, 0, 128*1024)
	s.Buffer(buf, 1024*1024)

	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "diff --git ") {
			// diff --git a/foo b/foo
			toks, err := scanPatchTokens(line[len("diff --git "):], 2)
			if err != nil {
				return nil, err
			}
			addUnique(&out, seen, toks[0])
			addUnique(&out, seen, toks[1])
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			toks, err := scanPatchTokens(line[4:], 1)
			if err != nil {
				return nil, err
			}
			addUnique(&out, seen, toks[0])
			continue
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanPatchTokens(s string, n int) ([]string, error) {
	// Parse N header tokens from a patch header line.
	// Supports Git-style double-quoted C-escaped strings.
	toks := make([]string, 0, n)
	i := 0
	skipWS := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
	}

	for len(toks) < n {
		skipWS()
		if i >= len(s) {
			break
		}

		if s[i] == '"' {
			start := i
			i++ // opening quote
			closed := false
			for i < len(s) {
				if s[i] == '\\' {
					if i+1 >= len(s) {
						return nil, fmt.Errorf("unterminated escape in quoted token: %q", s[start:])
					}
					i += 2
					continue
				}
				if s[i] == '"' {
					i++
					raw := s[start:i]
					u, err := strconv.Unquote(raw)
					if err != nil {
						return nil, fmt.Errorf("invalid quoted token %q: %w", raw, err)
					}
					toks = append(toks, u)
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated quoted token: %q", s[start:])
			}
			continue
		}

		start := i
		for i < len(s) && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		tok := s[start:i]
		tok = unquoteMaybe(tok)
		toks = append(toks, tok)
	}

	if len(toks) < n {
		return nil, fmt.Errorf("expected %d token(s), found %d", n, len(toks))
	}
	return toks, nil
}

func addUnique(out *[]string, seen map[string]struct{}, p string) {
	if _, ok := seen[p]; ok {
		return
	}
	seen[p] = struct{}{}
	*out = append(*out, p)
}

func extractFencedDiff(in []byte) []byte {
	// Find first line exactly ```diff (allow leading/trailing spaces) and extract until closing ```.
	// This is intentionally conservative.
	lines := bytes.Split(in, []byte("\n"))
	start := -1
	for i, ln := range lines {
		t := bytes.TrimSpace(ln)
		if bytes.Equal(t, []byte("```diff")) {
			start = i + 1
			break
		}
	}
	if start == -1 {
		return nil
	}
	end := -1
	for i := start; i < len(lines); i++ {
		if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("```")) {
			end = i
			break
		}
	}
	if end == -1 {
		return nil
	}
	return bytes.Join(lines[start:end], []byte("\n"))
}

func unquoteMaybe(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	// Git often uses C-style quoting without wrapping in quotes (e.g., path\040with\040space).
	// Wrap and try Unquote anyway.
	if strings.Contains(s, "\\") {
		if u, err := strconv.Unquote("\"" + s + "\""); err == nil {
			return u
		}
	}
	return s
}
