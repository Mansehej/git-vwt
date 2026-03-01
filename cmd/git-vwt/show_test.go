package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowFailsOutsideGitRepo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"show", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "not a git repository") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestShowRootPatchPrintsBaseDash(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")
	root := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/root", root)

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"show", "root"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(out.String(), "base: -\n") {
		t.Fatalf("expected base dash, got: %q", out.String())
	}
}
