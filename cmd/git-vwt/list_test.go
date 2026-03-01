package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestListShowsBaseDashForRootCommitRef(t *testing.T) {
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
		code := run(ctx, []string{"list"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("list exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(out.String(), "root\t") {
		t.Fatalf("expected root id in list output: %q", out.String())
	}
	if !strings.Contains(out.String(), "\t-\tbase") {
		t.Fatalf("expected base dash field in list output: %q", out.String())
	}
}
