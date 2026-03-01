package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImport_GenerateIDFailure(t *testing.T) {
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

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "generate id") {
		t.Fatalf("expected generate id error, got: %q", errOut.String())
	}
}

func TestImport_InvalidGeneratedIDFailsRefFormat(t *testing.T) {
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

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "bad id", nil }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "invalid id") {
		t.Fatalf("expected invalid id error, got: %q", errOut.String())
	}
}

func TestImport_FailsWhenTempIndexWriteFails(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			return errors.New("boom")
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "temp index") {
		t.Fatalf("expected temp index error, got: %q", errOut.String())
	}
}

func TestImport_FailsWhenReadTreeFailsDueToIndexDir(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git read-tree") {
		t.Fatalf("expected git read-tree error, got: %q", errOut.String())
	}
}

func TestImport_FailsWhenCommitTreeCannotReadMessageFile(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "msg" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git commit-tree") {
		t.Fatalf("expected git commit-tree error, got: %q", errOut.String())
	}
}

func TestImport_FailsWhenWriteMessageFails(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "msg" {
			return errors.New("boom")
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "write message") {
		t.Fatalf("expected write message error, got: %q", errOut.String())
	}
}

func TestCompose_GenerateIDFailure(t *testing.T) {
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

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "generate id") {
		t.Fatalf("expected generate id error, got: %q", errOut.String())
	}
}

func TestCompose_InvalidGeneratedIDFailsRefFormat(t *testing.T) {
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

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "bad id", nil }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "invalid id") {
		t.Fatalf("expected invalid id error, got: %q", errOut.String())
	}
}

func TestCompose_FailsWhenReadTreeFailsDueToIndexDir(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git read-tree") {
		t.Fatalf("expected git read-tree error, got: %q", errOut.String())
	}
}

func TestCompose_FailsWhenCommitTreeCannotReadMessageFile(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "msg" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git commit-tree") {
		t.Fatalf("expected git commit-tree error, got: %q", errOut.String())
	}
}

func TestSnapshot_GenerateIDFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "generate id") {
		t.Fatalf("expected generate id error, got: %q", errOut.String())
	}
}

func TestSnapshot_InvalidGeneratedIDFailsRefFormat(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "bad id", nil }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "invalid ref") {
		t.Fatalf("expected invalid ref error, got: %q", errOut.String())
	}
}

func TestSnapshot_RejectsGeneratedIDThatAlreadyExists(t *testing.T) {
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
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/snapshots/fixed", base)

	orig := vwtGenerateID
	vwtGenerateID = func(time.Time) (string, error) { return "fixed", nil }
	t.Cleanup(func() { vwtGenerateID = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "already exists") {
		t.Fatalf("expected already-exists error, got: %q", errOut.String())
	}
}

func TestSnapshot_FailsWhenReadTreeFailsDueToIndexDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git read-tree") {
		t.Fatalf("expected git read-tree error, got: %q", errOut.String())
	}
}

func TestSnapshot_FailsWhenReadTreeEmptyFailsDueToIndexDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "read-tree --empty") {
		t.Fatalf("expected read-tree --empty error, got: %q", errOut.String())
	}
}

func TestCompose_FailsWhenTempIndexWriteFails(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			return errors.New("boom")
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "temp index") {
		t.Fatalf("expected temp index error, got: %q", errOut.String())
	}
}

func TestCompose_FailsWhenWriteMessageFails(t *testing.T) {
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

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "msg" {
			return errors.New("boom")
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "write message") {
		t.Fatalf("expected write message error, got: %q", errOut.String())
	}
}

func TestSnapshot_FailsWhenTempIndexWriteFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "index" {
			return errors.New("boom")
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "temp index") {
		t.Fatalf("expected temp index error, got: %q", errOut.String())
	}
}

func TestSnapshot_FailsWhenWriteMessageFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "msg" {
			return errors.New("boom")
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "write message") {
		t.Fatalf("expected write message error, got: %q", errOut.String())
	}
}

func TestSnapshot_FailsWhenCommitTreeCannotReadMessageFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	orig := osWriteFile
	osWriteFile = func(name string, data []byte, perm fs.FileMode) error {
		if filepath.Base(name) == "msg" {
			if err := os.Mkdir(name, 0o700); err != nil {
				return err
			}
			return nil
		}
		return orig(name, data, perm)
	}
	t.Cleanup(func() { osWriteFile = orig })

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git commit-tree") {
		t.Fatalf("expected git commit-tree error, got: %q", errOut.String())
	}
}
