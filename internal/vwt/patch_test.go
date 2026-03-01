package vwt

import "testing"

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
