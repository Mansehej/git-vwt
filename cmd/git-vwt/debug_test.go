package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugFlagEnablesGitCommandLogging(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	initTestRepo(t, dir)

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"--debug", "--ws", "dbg", "open"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "+ git ") {
		t.Fatalf("expected debug git logging, got: %q", errOut.String())
	}
}
