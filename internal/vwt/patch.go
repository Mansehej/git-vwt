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
	// - remove extra leading/trailing blank newlines
	// - ensure the final byte is a newline (git apply treats missing final newline as a corrupt patch)
	out := bytes.ReplaceAll(in, []byte("\r\n"), []byte("\n"))
	out = bytes.ReplaceAll(out, []byte("\r"), []byte("\n"))
	out = bytes.Trim(out, "\n")
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

type PatchPathPolicy struct {
	AllowDotGit bool
}

func ValidatePatchPaths(diff []byte, policy PatchPathPolicy) error {
	paths := touchedPaths(diff)
	for _, p := range paths {
		if p == "" || p == "/dev/null" {
			continue
		}

		// Normalize common prefixes.
		if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
			p = p[2:]
		}
		p = strings.TrimPrefix(p, "./")
		p = strings.TrimPrefix(p, "//")

		// Git may quote paths; try to unquote for checks.
		p = unquoteMaybe(p)

		if p == ".git" || strings.HasPrefix(p, ".git/") {
			if policy.AllowDotGit {
				continue
			}
			return fmt.Errorf("refusing patch that touches .git/** (use --allow-dot-git to override)")
		}
		if strings.HasPrefix(p, "/") {
			return fmt.Errorf("refusing patch with absolute path: %s", p)
		}
		if p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
			return fmt.Errorf("refusing patch with unsafe path: %s", p)
		}
	}
	return nil
}

func touchedPaths(diff []byte) []string {
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
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				addUnique(&out, seen, fields[2])
				addUnique(&out, seen, fields[3])
			}
			continue
		}
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				addUnique(&out, seen, fields[1])
			}
			continue
		}
	}
	return out
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
