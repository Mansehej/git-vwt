package vwt

import (
	"bytes"
	"errors"
	"testing"
)

func TestValidatePatchPaths_AllowsQuotedSpaces(t *testing.T) {
	diff := []byte(
		"diff --git \"a/dir with space/file.txt\" \"b/dir with space/file.txt\"\n" +
			"--- \"a/dir with space/file.txt\"\n" +
			"+++ \"b/dir with space/file.txt\"\n",
	)
	if err := ValidatePatchPaths(diff); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidatePatchPaths_BlocksDotGitEvenWhenQuoted(t *testing.T) {
	diff := []byte("diff --git \"a/.git/config\" \"b/.git/config\"\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidatePatchPaths_AllowsCEscapedSpaces(t *testing.T) {
	diff := []byte("diff --git a/path\\040with\\040space.txt b/path\\040with\\040space.txt\n")
	if err := ValidatePatchPaths(diff); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidatePatchPaths_BlocksAbsoluteDoubleSlash(t *testing.T) {
	diff := []byte("diff --git //etc/passwd //etc/passwd\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidatePatchPaths_BlocksTraversalSegment(t *testing.T) {
	diff := []byte("diff --git a/foo/.. b/foo/..\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidatePatchPaths_FailsClosedOnMalformedQuote(t *testing.T) {
	diff := []byte("diff --git \"a/bad b/good\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestValidatePatchPaths_SkipsDevNull(t *testing.T) {
	diff := []byte(
		"diff --git a/new.txt b/new.txt\n" +
			"--- /dev/null\n" +
			"+++ b/new.txt\n",
	)
	if err := ValidatePatchPaths(diff); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidatePatchPaths_AllowsDashLinesWithTimestampAndQuotes(t *testing.T) {
	diff := []byte(
		"--- \"a/foo bar.txt\"\t2026-03-01 00:00:00 +0000\n" +
			"+++ \"b/foo bar.txt\"\t2026-03-01 00:00:00 +0000\n",
	)
	if err := ValidatePatchPaths(diff); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestExtractDiff_EmptyAndWhitespace(t *testing.T) {
	if _, err := ExtractDiff(nil); !errors.Is(err, ErrNoDiffFound) {
		t.Fatalf("expected ErrNoDiffFound, got %v", err)
	}
	if _, err := ExtractDiff([]byte("\n\n\n")); !errors.Is(err, ErrNoDiffFound) {
		t.Fatalf("expected ErrNoDiffFound, got %v", err)
	}
}

func TestExtractDiff_FencedDiffBlock(t *testing.T) {
	in := []byte("hello\n\n```diff\n" +
		"diff --git a/a.txt b/a.txt\n" +
		"--- a/a.txt\n" +
		"+++ b/a.txt\n" +
		"@@ -1 +1 @@\n" +
		"-a\n" +
		"+b\n" +
		"\n" +
		"```\n")
	out, err := ExtractDiff(in)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if !bytes.HasPrefix(out, []byte("diff --git ")) {
		t.Fatalf("expected diff header, got %q", string(out))
	}
}

func TestExtractDiff_TrimsLeadingProseUntilHeader(t *testing.T) {
	in := []byte("note: hi\n\n  diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n")
	out, err := ExtractDiff(in)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	trim := bytes.TrimLeft(out, " \t")
	if !bytes.HasPrefix(trim, []byte("diff --git ")) {
		t.Fatalf("expected to start at diff header, got %q", string(out))
	}
}

func TestNormalizeDiff_PreservesTrailingBlankLines(t *testing.T) {
	in := []byte("diff --git a/a b/a\n\n")
	out := normalizeDiff(in)
	if !bytes.Equal(out, in) {
		t.Fatalf("expected unchanged, got %q", string(out))
	}
}

func TestPatchSHA256Hex_Empty(t *testing.T) {
	got := PatchSHA256Hex(nil)
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("got=%s want=%s", got, want)
	}
}

func TestUnquoteMaybe(t *testing.T) {
	if got := unquoteMaybe("\"a b\""); got != "a b" {
		t.Fatalf("got=%q", got)
	}
	if got := unquoteMaybe("path\\040with\\040space"); got != "path with space" {
		t.Fatalf("got=%q", got)
	}
	if got := unquoteMaybe("plain"); got != "plain" {
		t.Fatalf("got=%q", got)
	}
}

func TestValidatePatchPaths_FailsOnDiffLineMissingPaths(t *testing.T) {
	diff := []byte("diff --git a/only-one\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidatePatchPaths_FailsOnDashLineMissingPath(t *testing.T) {
	diff := []byte("--- \n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidatePatchPaths_FailsOnQuotedTokenWithUnterminatedEscape(t *testing.T) {
	diff := []byte("diff --git \"a\\\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error")
	}
}

func TestValidatePatchPaths_FailsOnInvalidQuotedToken(t *testing.T) {
	diff := []byte("diff --git \"\\q\" \"b\"\n")
	if err := ValidatePatchPaths(diff); err == nil {
		t.Fatalf("expected error")
	}
}

func TestExtractDiff_FallbackReturnsNormalizedInput(t *testing.T) {
	out, err := ExtractDiff([]byte("hello"))
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if string(out) != "hello\n" {
		t.Fatalf("unexpected output: %q", string(out))
	}
}

func TestNormalizeDiff_OnlyNewlinesBecomesEmpty(t *testing.T) {
	out := normalizeDiff([]byte("\n\n"))
	if len(out) != 0 {
		t.Fatalf("expected empty, got %q", string(out))
	}
}

func TestExtractFencedDiff_NoClosingFence(t *testing.T) {
	in := []byte("```diff\ndiff --git a/a b/a\n")
	if got := extractFencedDiff(in); got != nil {
		t.Fatalf("expected nil, got %q", string(got))
	}
}
