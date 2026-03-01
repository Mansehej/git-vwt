package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDropFailsWhenRefIsLocked(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/p1", base)

	// Make update-ref -d fail.
	lockPath := filepath.Join(dir, ".git", "refs", "vwt", "patches", "p1.lock")
	mustWrite(t, lockPath, "lock")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"drop", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "cannot lock ref") {
		t.Fatalf("expected lock error, got: %q", errOut.String())
	}
}

func TestGCFailsWhenRefIsLocked(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	tree := strings.TrimSpace(git(t, dir, "rev-parse", base+"^{tree}"))

	// Old commit for a patch ref.
	t.Setenv("GIT_AUTHOR_DATE", "2000-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2000-01-01T00:00:00Z")
	oldCommit := strings.TrimSpace(git(t, dir, "commit-tree", tree, "-p", base, "-m", "old"))
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/old", oldCommit)

	// Make update-ref -d fail for old.
	lockPath := filepath.Join(dir, ".git", "refs", "vwt", "patches", "old.lock")
	mustWrite(t, lockPath, "lock")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"gc", "--keep-days", "1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "gc: drop refs/vwt/patches/old") {
		t.Fatalf("expected gc drop error, got: %q", errOut.String())
	}
}

func TestGCDefaultNoop(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"gc"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
}

func TestCatReadsHeadByDefault(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"cat", "a.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if got := out.String(); got != "a\n" {
		t.Fatalf("unexpected cat output: %q", got)
	}
}

func TestCatFailsWhenPathMissingInCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"cat", "missing.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git show") {
		t.Fatalf("expected git show error, got: %q", errOut.String())
	}
}
