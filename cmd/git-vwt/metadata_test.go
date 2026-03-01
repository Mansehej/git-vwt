package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSetsAuthorAndSubjectFromAgentAndTitle(t *testing.T) {
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

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--agent", "bot", "--title", "Update greeting", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
		}
	})
	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected import output: %q", out.String())
	}
	commit := fields[1]

	if got := strings.TrimSpace(git(t, dir, "show", "-s", "--format=%an", commit)); got != "bot" {
		t.Fatalf("unexpected author: %q", got)
	}
	if got := strings.TrimSpace(git(t, dir, "show", "-s", "--format=%s", commit)); got != "vwt(bot): Update greeting" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestComposeSetsAuthorAndSubjectFromAgentAndTitle(t *testing.T) {
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

	composeOut, composeErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "--agent", "bot", "--title", "Shadow", "p1"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 0 {
			t.Fatalf("compose exit=%d stderr=%s", code, composeErr.String())
		}
	})
	fields := strings.Split(strings.TrimSpace(composeOut.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected compose output: %q", composeOut.String())
	}
	commit := fields[1]

	if got := strings.TrimSpace(git(t, dir, "show", "-s", "--format=%an", commit)); got != "bot" {
		t.Fatalf("unexpected author: %q", got)
	}
	if got := strings.TrimSpace(git(t, dir, "show", "-s", "--format=%s", commit)); got != "vwt(bot): compose Shadow" {
		t.Fatalf("unexpected subject: %q", got)
	}
}

func TestSnapshotMessageFlagSetsSubject(t *testing.T) {
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
		code := run(ctx, []string{"snapshot", "-m", "my snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("snapshot exit=%d stderr=%s", code, errOut.String())
		}
	})
	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected snapshot output: %q", out.String())
	}
	commit := fields[1]
	if got := strings.TrimSpace(git(t, dir, "show", "-s", "--format=%s", commit)); got != "vwt snapshot: my snapshot" {
		t.Fatalf("unexpected subject: %q", got)
	}
}
