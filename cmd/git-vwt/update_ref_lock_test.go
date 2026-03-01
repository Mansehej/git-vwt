package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportFailsWhenPatchRefIsLockedOnCreate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "hello\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	// Lock the ref so update-ref fails.
	lockPath := filepath.Join(dir, ".git", "refs", "vwt", "patches", "p1.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite(t, lockPath, "lock")

	patch := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello world",
		"",
	}, "\n")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "import: git update-ref") {
		t.Fatalf("expected update-ref error, got: %q", errOut.String())
	}
}

func TestComposeFailsWhenComposeRefIsLockedOnCreate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "hello\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))

	patch := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello world",
		"",
	}, "\n")
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// Lock the compose ref so update-ref fails.
	lockPath := filepath.Join(dir, ".git", "refs", "vwt", "patches", "c1.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite(t, lockPath, "lock")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "compose: git update-ref") {
		t.Fatalf("expected update-ref error, got: %q", errOut.String())
	}
}

func TestSnapshotFailsWhenSnapshotRefIsLockedOnCreate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "s1", nil }
	t.Cleanup(func() { vwtGenerateID = orig })

	// Lock the snapshot ref so update-ref fails.
	lockPath := filepath.Join(dir, ".git", "refs", "vwt", "snapshots", "s1.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite(t, lockPath, "lock")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "snapshot: git update-ref") {
		t.Fatalf("expected update-ref error, got: %q", errOut.String())
	}
}
