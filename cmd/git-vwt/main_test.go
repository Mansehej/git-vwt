package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git-vwt/internal/gitx"
)

func TestImportListDiffExportApplyDrop(t *testing.T) {
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

	before := git(t, dir, "status", "--porcelain")
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--title", "Update greeting", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by import:\nbefore=%q\nafter=%q", before, after)
	}

	// list
	out.Reset()
	errOut.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"list"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("list exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(out.String(), "p1\t") {
		t.Fatalf("list output missing id p1: %q", out.String())
	}

	// diff
	out.Reset()
	errOut.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"diff", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("diff exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(out.String(), "+hello world") {
		t.Fatalf("diff output missing change: %q", out.String())
	}

	// export
	out.Reset()
	errOut.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"export", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("export exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(out.String(), "diff --git a/hello.txt b/hello.txt") {
		t.Fatalf("export output missing diff header: %q", out.String())
	}

	// apply
	headBefore := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	out.Reset()
	errOut.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"apply", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("apply exit=%d stderr=%s", code, errOut.String())
		}
	})
	headAfter := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	if headAfter != headBefore {
		t.Fatalf("apply moved HEAD: before=%s after=%s", headBefore, headAfter)
	}
	got := mustRead(t, filepath.Join(dir, "hello.txt"))
	if got != "hello world\n" {
		t.Fatalf("apply did not change file: %q", got)
	}
	if cached := strings.TrimSpace(git(t, dir, "diff", "--cached", "--name-only")); cached != "" {
		t.Fatalf("apply staged files unexpectedly: %q", cached)
	}
	if status := git(t, dir, "status", "--porcelain"); status != " M hello.txt\n" {
		t.Fatalf("apply did not leave unstaged change: %q", status)
	}

	// drop
	out.Reset()
	errOut.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"drop", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("drop exit=%d stderr=%s", code, errOut.String())
		}
	})
	if ok := gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/p1"); ok {
		t.Fatalf("patch ref still exists after drop")
	}
}

func TestImportValidationAndFileInputs(t *testing.T) {
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

	// missing --base
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// --stdin with an extra arg
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--stdin", "x.patch"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// no --stdin and no patch path
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// invalid id should fail ref format check
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "bad id", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// patch file path input
	patchPath := filepath.Join(dir, "p.patch")
	mustWrite(t, patchPath, patch)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "pfile", patchPath}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
		if !strings.Contains(out.String(), "pfile\t") {
			t.Fatalf("unexpected import output: %q", out.String())
		}
	}

	// patch from stdin via "-" path
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "pstdin", "-"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
		if !strings.Contains(out.String(), "pstdin\t") {
			t.Fatalf("unexpected import output: %q", out.String())
		}
	}

	// patch that results in no changes
	noop := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello",
		"",
	}, "\n")
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "pnoop", "--stdin"}, IO{In: strings.NewReader(noop), Out: &out, Err: &errOut})
			if code != 1 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
		if !strings.Contains(errOut.String(), "no changes") {
			t.Fatalf("expected no-changes message, got: %q", errOut.String())
		}
	}
}

func TestImportFailsOnEmptyPatch(t *testing.T) {
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

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "pempty", "--stdin"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "no diff") {
		t.Fatalf("expected no-diff error, got: %q", errOut.String())
	}
}

func TestImportFailsWhenPatchDoesNotApply(t *testing.T) {
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

	badPatch := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-does not match",
		"+hello world",
		"",
	}, "\n")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "pbad", "--stdin"}, IO{In: strings.NewReader(badPatch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "git apply") {
		t.Fatalf("expected git apply error, got: %q", errOut.String())
	}
}

func TestImportRejectsDuplicateID(t *testing.T) {
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

	// first import
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// duplicate id
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "already exists") {
		t.Fatalf("expected already-exists message, got: %q", errOut.String())
	}
}

func TestImportAutoGeneratesID(t *testing.T) {
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
		code := run(ctx, []string{"import", "--base", base, "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("import exit=%d stderr=%s", code, errOut.String())
		}
	})
	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected import output: %q", out.String())
	}
	id := fields[0]
	if id == "" {
		t.Fatalf("expected generated id")
	}
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/"+id) {
		t.Fatalf("expected patch ref to exist")
	}
}

func TestImportFailsWhenBaseIsInvalid(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "hello\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")

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
		code := run(ctx, []string{"import", "--base", "nope", "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "resolve base commit") {
		t.Fatalf("expected base resolve error, got: %q", errOut.String())
	}
}

func TestImportFailsWhenPatchFileMissing(t *testing.T) {
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

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "missing.patch"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "read file") {
		t.Fatalf("expected read file error, got: %q", errOut.String())
	}
}

func TestImportFailsWhenTempDirIsInvalid(t *testing.T) {
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

	// Force os.TempDir() to return a file path, so MkdirTemp fails.
	tmpFile := filepath.Join(dir, "not-a-dir")
	mustWrite(t, tmpFile, "x")
	t.Setenv("TMPDIR", tmpFile)
	t.Setenv("TEMP", tmpFile)
	t.Setenv("TMP", tmpFile)

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "temp dir") {
		t.Fatalf("expected temp dir error, got: %q", errOut.String())
	}
}

func TestSnapshotIncludesUntrackedAndExcludesIgnored(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "tracked.txt"), "one\n")
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	git(t, dir, "add", "tracked.txt", ".gitignore")
	git(t, dir, "commit", "-m", "base")

	// dirty state
	mustWrite(t, filepath.Join(dir, "tracked.txt"), "one\ntwo\n")
	mustWrite(t, filepath.Join(dir, "new.txt"), "new\n")
	mustWrite(t, filepath.Join(dir, "ignored.txt"), "nope\n")

	before := git(t, dir, "status", "--porcelain")
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot", "-m", "test snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("snapshot exit=%d stderr=%s", code, errOut.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by snapshot:\nbefore=%q\nafter=%q", before, after)
	}

	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected snapshot output: %q", out.String())
	}
	snapID, snapCommit := fields[0], fields[1]
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/snapshots/"+snapID) {
		t.Fatalf("snapshot ref missing")
	}

	names := git(t, dir, "ls-tree", "-r", "--name-only", snapCommit)
	if !strings.Contains(names, "tracked.txt\n") {
		t.Fatalf("snapshot missing tracked.txt: %q", names)
	}
	if !strings.Contains(names, "new.txt\n") {
		t.Fatalf("snapshot missing new.txt: %q", names)
	}
	if strings.Contains(names, "ignored.txt\n") {
		t.Fatalf("snapshot included ignored.txt: %q", names)
	}
}

func TestListEmptyRepoOutputsNothing(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"list"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("list exit=%d stderr=%s", code, errOut.String())
		}
	})
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("expected empty output, got: %q", out.String())
	}
}

func TestSnapshotWorksWithoutHEAD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	before := git(t, dir, "status", "--porcelain")
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot", "-m", "empty"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("snapshot exit=%d stderr=%s", code, errOut.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by snapshot:\nbefore=%q\nafter=%q", before, after)
	}

	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected snapshot output: %q", out.String())
	}
	snapID := fields[0]
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/snapshots/"+snapID) {
		t.Fatalf("snapshot ref missing")
	}
}

func TestSnapshotCleanRepoStillCreatesCommit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	before := git(t, dir, "status", "--porcelain")
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("snapshot exit=%d stderr=%s", code, errOut.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by snapshot:\nbefore=%q\nafter=%q", before, after)
	}

	fields := strings.Split(strings.TrimSpace(out.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected snapshot output: %q", out.String())
	}
	snapID := fields[0]
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/snapshots/"+snapID) {
		t.Fatalf("snapshot ref missing")
	}
}

func TestSnapshotFailsOutsideGitRepo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "not a git repository") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestSnapshotFailsWhenTempDirIsInvalid(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	// Force os.TempDir() to return a file path, so MkdirTemp fails.
	tmpFile := filepath.Join(dir, "not-a-dir")
	mustWrite(t, tmpFile, "x")
	t.Setenv("TMPDIR", tmpFile)
	t.Setenv("TEMP", tmpFile)
	t.Setenv("TMP", tmpFile)

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "temp dir") {
		t.Fatalf("expected temp dir error, got: %q", errOut.String())
	}
}

func TestComposeAndCatProvideShadowView(t *testing.T) {
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

	patch1 := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello world",
		"",
	}, "\n")
	patch2 := strings.Join([]string{
		"diff --git a/bye.txt b/bye.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/bye.txt",
		"@@ -0,0 +1 @@",
		"+bye",
		"",
	}, "\n")

	// import p1 + p2
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch1), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p1 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p2", "--stdin"}, IO{In: strings.NewReader(patch2), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p2 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// compose without touching working tree
	before := git(t, dir, "status", "--porcelain")
	composeOut, composeErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1", "p2"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 0 {
			t.Fatalf("compose exit=%d stderr=%s", code, composeErr.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by compose:\nbefore=%q\nafter=%q", before, after)
	}

	fields := strings.Split(strings.TrimSpace(composeOut.String()), "\t")
	if len(fields) < 3 {
		t.Fatalf("unexpected compose output: %q", composeOut.String())
	}
	if fields[0] != "c1" {
		t.Fatalf("unexpected compose id: %q", fields[0])
	}
	composeCommit := fields[1]
	if fields[2] != base {
		t.Fatalf("unexpected compose base: got=%q want=%q", fields[2], base)
	}
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/c1") {
		t.Fatalf("compose ref missing")
	}

	// Shadow view via git show
	if got := git(t, dir, "show", composeCommit+":hello.txt"); got != "hello world\n" {
		t.Fatalf("compose hello.txt unexpected: %q", got)
	}
	if got := git(t, dir, "show", composeCommit+":bye.txt"); got != "bye\n" {
		t.Fatalf("compose bye.txt unexpected: %q", got)
	}

	// Shadow view via git vwt cat
	catOut, catErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"cat", "c1", "hello.txt"}, IO{In: strings.NewReader(""), Out: &catOut, Err: &catErr})
		if code != 0 {
			t.Fatalf("cat exit=%d stderr=%s", code, catErr.String())
		}
	})
	if got := catOut.String(); got != "hello world\n" {
		t.Fatalf("cat hello.txt unexpected: %q", got)
	}

	// Stacked patch compose: p3 based on p1
	patch3 := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello world",
		"+hello world!!",
		"",
	}, "\n")
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", "refs/vwt/patches/p1", "--id", "p3", "--stdin"}, IO{In: strings.NewReader(patch3), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p3 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	composeOut.Reset()
	composeErr.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c2", "p1", "p3"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 0 {
			t.Fatalf("compose c2 exit=%d stderr=%s", code, composeErr.String())
		}
	})
	fields = strings.Split(strings.TrimSpace(composeOut.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected compose c2 output: %q", composeOut.String())
	}
	composeCommit2 := fields[1]
	if got := git(t, dir, "show", composeCommit2+":hello.txt"); got != "hello world!!\n" {
		t.Fatalf("compose c2 hello.txt unexpected: %q", got)
	}
}

func TestComposeValidation(t *testing.T) {
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

	// missing --base
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"compose", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// no patch IDs
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"compose", "--base", base}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// empty patch ID
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"compose", "--base", base, ""}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// unknown patch ID
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"compose", "--base", base, "nope"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 1 {
				t.Fatalf("exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
}

func TestComposeFailsWhenBaseInvalid(t *testing.T) {
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
		code := run(ctx, []string{"compose", "--base", "nope", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "resolve base commit") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestComposeAutoGeneratesID(t *testing.T) {
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
		code := run(ctx, []string{"compose", "--base", base, "p1"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 0 {
			t.Fatalf("compose exit=%d stderr=%s", code, composeErr.String())
		}
	})
	fields := strings.Split(strings.TrimSpace(composeOut.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected compose output: %q", composeOut.String())
	}
	id := fields[0]
	if id == "" {
		t.Fatalf("expected generated id")
	}
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/"+id) {
		t.Fatalf("expected composed ref to exist")
	}
}

func TestComposeRejectsDuplicateID(t *testing.T) {
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

	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("compose exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "already exists") {
		t.Fatalf("expected already-exists message, got: %q", errOut.String())
	}
}

func TestComposeFailsWhenTempDirIsInvalid(t *testing.T) {
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

	// Force os.TempDir() to return a file path, so MkdirTemp fails.
	tmpFile := filepath.Join(dir, "not-a-dir")
	mustWrite(t, tmpFile, "x")
	t.Setenv("TMPDIR", tmpFile)
	t.Setenv("TEMP", tmpFile)
	t.Setenv("TMP", tmpFile)

	composeOut, composeErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "p1"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, composeErr.String())
		}
	})
	if !strings.Contains(composeErr.String(), "temp dir") {
		t.Fatalf("expected temp dir error, got: %q", composeErr.String())
	}
}

func TestComposeFailsWhenPatchCommitHasNoParent(t *testing.T) {
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

	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/root", base)

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c1", "root"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "no parent") {
		t.Fatalf("expected no-parent error, got: %q", errOut.String())
	}
}

func TestComposeResultsInNoChangesWhenPatchesCancel(t *testing.T) {
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

	patchUp := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello world",
		"",
	}, "\n")
	patchDown := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello world",
		"+hello",
		"",
	}, "\n")

	// import p1 on base
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patchUp), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p1 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	// import p2 on p1 that reverts back to base content
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", "refs/vwt/patches/p1", "--id", "p2", "--stdin"}, IO{In: strings.NewReader(patchDown), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p2 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	composeOut, composeErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "c0", "p1", "p2"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 1 {
			t.Fatalf("compose exit=%d stderr=%s", code, composeErr.String())
		}
	})
	if !strings.Contains(composeErr.String(), "no changes") {
		t.Fatalf("expected no-changes message, got: %q", composeErr.String())
	}
}

func TestComposeFailsWhenPatchDoesNotApply(t *testing.T) {
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

	patch1 := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello",
		"+hello world",
		"",
	}, "\n")
	patch2 := strings.Join([]string{
		"diff --git a/hello.txt b/hello.txt",
		"--- a/hello.txt",
		"+++ b/hello.txt",
		"@@ -1 +1 @@",
		"-hello world",
		"+hello world!!",
		"",
	}, "\n")

	// import p1
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", base, "--id", "p1", "--stdin"}, IO{In: strings.NewReader(patch1), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p1 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	// import p2 based on p1
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"import", "--base", "refs/vwt/patches/p1", "--id", "p2", "--stdin"}, IO{In: strings.NewReader(patch2), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("import p2 exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	// composing only p2 on base should fail to apply
	composeOut, composeErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "cbad", "p2"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 1 {
			t.Fatalf("compose exit=%d stderr=%s", code, composeErr.String())
		}
	})
	if !strings.Contains(composeErr.String(), "compose: apply") {
		t.Fatalf("expected apply error, got: %q", composeErr.String())
	}
}

func TestImportBlocksDotGitByDefault(t *testing.T) {
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

	patch := strings.Join([]string{
		"diff --git a/.git/config b/.git/config",
		"--- a/.git/config",
		"+++ b/.git/config",
		"@@ -0,0 +1 @@",
		"+nope",
		"",
	}, "\n")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "pbad", "--stdin"}, IO{In: strings.NewReader(patch), Out: &out, Err: &errOut})
		if code == 0 {
			t.Fatalf("expected import to fail")
		}
	})
	if !strings.Contains(errOut.String(), ".git") {
		t.Fatalf("expected .git error, got: %q", errOut.String())
	}
}

func TestRefExistsDoesNotSwallowRunnerFailure(t *testing.T) {
	ctx := context.Background()
	badDir := filepath.Join(t.TempDir(), "does-not-exist")
	gr := gitx.Runner{Dir: badDir, Env: os.Environ()}
	ok, err := refExists(ctx, gr, "refs/heads/main")
	if err == nil {
		t.Fatalf("expected error, got nil (ok=%v)", ok)
	}
	if ok {
		t.Fatalf("expected ok=false on runner failure")
	}
}

func TestComposeHandlesBinaryPatch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	baseBytes := []byte{0x00, 'a', 'b', 0x00, 'c', 'd', '\n'}
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), baseBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "bin.dat")
	git(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	baseBlob := strings.TrimSpace(git(t, dir, "rev-parse", base+":bin.dat"))

	wantBytes := []byte{0x00, 'x', 'y', 0x00, 'z', '!', '\n'}
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), wantBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	patch := git(t, dir, "diff",
		"--no-color",
		"--binary",
		"--full-index",
		"--no-renames",
		"--no-ext-diff",
		"--no-textconv",
		base,
	)

	// import pbin
	importOut, importErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"import", "--base", base, "--id", "pbin", "--stdin"}, IO{In: strings.NewReader(patch), Out: &importOut, Err: &importErr})
		if code != 0 {
			t.Fatalf("import pbin exit=%d stderr=%s", code, importErr.String())
		}
	})
	fields := strings.Split(strings.TrimSpace(importOut.String()), "\t")
	if len(fields) < 3 {
		t.Fatalf("unexpected import output: %q", importOut.String())
	}
	patchCommit := fields[1]
	patchBlob := strings.TrimSpace(git(t, dir, "rev-parse", patchCommit+":bin.dat"))
	if patchBlob == baseBlob {
		t.Fatalf("expected patch blob to differ from base")
	}

	// compose cbin
	composeOut, composeErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"compose", "--base", base, "--id", "cbin", "pbin"}, IO{In: strings.NewReader(""), Out: &composeOut, Err: &composeErr})
		if code != 0 {
			t.Fatalf("compose cbin exit=%d stderr=%s", code, composeErr.String())
		}
	})
	fields = strings.Split(strings.TrimSpace(composeOut.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected compose output: %q", composeOut.String())
	}
	composeCommit := fields[1]
	composeBlob := strings.TrimSpace(git(t, dir, "rev-parse", composeCommit+":bin.dat"))
	if composeBlob != patchBlob {
		t.Fatalf("compose blob mismatch: got=%s want=%s", composeBlob, patchBlob)
	}
}

func TestEnsureRepoFailsOutsideGitRepo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"list"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "not a git repository") {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestUnknownPatchIDsReturnErrors(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "a\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "show", args: []string{"show", "nope"}, want: 1},
		{name: "diff", args: []string{"diff", "nope"}, want: 1},
		{name: "export", args: []string{"export", "nope"}, want: 1},
		{name: "apply", args: []string{"apply", "nope"}, want: 1},
		{name: "drop", args: []string{"drop", "nope"}, want: 1},
		{name: "cat", args: []string{"cat", "nope", "a.txt"}, want: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut := bytes.Buffer{}, bytes.Buffer{}
			withChdir(t, dir, func() {
				code := run(ctx, tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
				if code != tc.want {
					t.Fatalf("exit=%d want=%d stderr=%s", code, tc.want, errOut.String())
				}
			})
			if errOut.String() == "" {
				t.Fatalf("expected stderr")
			}
		})
	}
}

func TestDiffExportApplyFailOnRootCommitPatch(t *testing.T) {
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

	cases := []struct {
		name string
		args []string
	}{
		{name: "diff", args: []string{"diff", "root"}},
		{name: "export", args: []string{"export", "root"}},
		{name: "apply", args: []string{"apply", "root"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut := bytes.Buffer{}, bytes.Buffer{}
			withChdir(t, dir, func() {
				code := run(ctx, tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
				if code != 1 {
					t.Fatalf("exit=%d stderr=%s", code, errOut.String())
				}
			})
			if !strings.Contains(errOut.String(), "no parent") {
				t.Fatalf("expected no-parent error, got: %q", errOut.String())
			}
		})
	}
}

func TestApplyNoopOnEmptyDiffPatch(t *testing.T) {
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

	git(t, dir, "commit", "--allow-empty", "-m", "empty")
	empty := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/empty", empty)

	before := git(t, dir, "status", "--porcelain")
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"apply", "empty"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("apply exit=%d stderr=%s", code, errOut.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by empty apply:\nbefore=%q\nafter=%q", before, after)
	}
	if got := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD")); got != empty {
		t.Fatalf("HEAD moved unexpectedly: got=%s want=%s (base=%s)", got, empty, base)
	}
}

func TestApplyFailsWhenPatchDoesNotApplyToWorktree(t *testing.T) {
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

	// Make working tree incompatible.
	mustWrite(t, filepath.Join(dir, "hello.txt"), "bye\n")

	before := git(t, dir, "status", "--porcelain")
	applyOut, applyErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"apply", "p1"}, IO{In: strings.NewReader(""), Out: &applyOut, Err: &applyErr})
		if code != 1 {
			t.Fatalf("apply exit=%d stderr=%s", code, applyErr.String())
		}
	})
	if !strings.Contains(applyErr.String(), "git apply") {
		t.Fatalf("expected git apply error, got: %q", applyErr.String())
	}
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("apply unexpectedly changed status:\nbefore=%q\nafter=%q", before, after)
	}
	if got := mustRead(t, filepath.Join(dir, "hello.txt")); got != "bye\n" {
		t.Fatalf("file changed unexpectedly: %q", got)
	}
}

func TestResolveCommitFromIDOrRev_SnapshotAndRev(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "a.txt"), "one\n")
	git(t, dir, "add", "a.txt")
	git(t, dir, "commit", "-m", "base")

	// capture a snapshot with a working tree change
	mustWrite(t, filepath.Join(dir, "a.txt"), "two\n")
	before := git(t, dir, "status", "--porcelain")
	snapOut, snapErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"snapshot", "-m", "snap"}, IO{In: strings.NewReader(""), Out: &snapOut, Err: &snapErr})
		if code != 0 {
			t.Fatalf("snapshot exit=%d stderr=%s", code, snapErr.String())
		}
	})
	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by snapshot")
	}
	fields := strings.Split(strings.TrimSpace(snapOut.String()), "\t")
	if len(fields) < 2 {
		t.Fatalf("unexpected snapshot output: %q", snapOut.String())
	}
	snapID := fields[0]

	// snapshot id resolution
	catOut, catErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"cat", snapID, "a.txt"}, IO{In: strings.NewReader(""), Out: &catOut, Err: &catErr})
		if code != 0 {
			t.Fatalf("cat snapshot exit=%d stderr=%s", code, catErr.String())
		}
	})
	if got := catOut.String(); got != "two\n" {
		t.Fatalf("cat snapshot unexpected: %q", got)
	}

	// direct rev resolution
	catOut.Reset()
	catErr.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"cat", "HEAD", "a.txt"}, IO{In: strings.NewReader(""), Out: &catOut, Err: &catErr})
		if code != 0 {
			t.Fatalf("cat HEAD exit=%d stderr=%s", code, catErr.String())
		}
	})
	if got := catOut.String(); got != "one\n" {
		t.Fatalf("cat HEAD unexpected: %q", got)
	}
}

func TestShowPrintsPatchHeader(t *testing.T) {
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

	showOut, showErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"show", "p1"}, IO{In: strings.NewReader(""), Out: &showOut, Err: &showErr})
		if code != 0 {
			t.Fatalf("show exit=%d stderr=%s", code, showErr.String())
		}
	})
	out := showOut.String()
	if !strings.Contains(out, "id: p1\n") {
		t.Fatalf("show output missing id header: %q", out)
	}
	if !strings.Contains(out, "ref: refs/vwt/patches/p1\n") {
		t.Fatalf("show output missing ref header: %q", out)
	}
	if !strings.Contains(out, "base: "+base+"\n") {
		t.Fatalf("show output missing base header: %q", out)
	}
}

func TestRunHelpAndUnknownCommand(t *testing.T) {
	ctx := context.Background()

	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		code := run(ctx, []string{"--help"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("--help exit=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("--help output missing Usage: %q", out.String())
		}
	}

	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		code := run(ctx, []string{}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("no-args exit=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(out.String(), "Usage:") {
			t.Fatalf("no-args output missing Usage: %q", out.String())
		}
	}

	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		code := run(ctx, []string{"nope"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 2 {
			t.Fatalf("unknown exit=%d stderr=%s", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "unknown subcommand: nope") {
			t.Fatalf("unknown stderr missing message: %q", errOut.String())
		}
		if !strings.Contains(errOut.String(), "Usage:") {
			t.Fatalf("unknown stderr missing usage: %q", errOut.String())
		}
	}
}

func TestCommandArgumentCounts(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "list-extra", args: []string{"list", "x"}, want: 2},
		{name: "show-none", args: []string{"show"}, want: 2},
		{name: "show-many", args: []string{"show", "a", "b"}, want: 2},
		{name: "diff-none", args: []string{"diff"}, want: 2},
		{name: "diff-many", args: []string{"diff", "a", "b"}, want: 2},
		{name: "export-none", args: []string{"export"}, want: 2},
		{name: "export-many", args: []string{"export", "a", "b"}, want: 2},
		{name: "apply-none", args: []string{"apply"}, want: 2},
		{name: "apply-many", args: []string{"apply", "a", "b"}, want: 2},
		{name: "drop-none", args: []string{"drop"}, want: 2},
		{name: "drop-many", args: []string{"drop", "a", "b"}, want: 2},
		{name: "snapshot-extra", args: []string{"snapshot", "x"}, want: 2},
		{name: "cat-none", args: []string{"cat"}, want: 2},
		{name: "cat-many", args: []string{"cat", "a", "b", "c"}, want: 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut := bytes.Buffer{}, bytes.Buffer{}
			code := run(ctx, tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != tc.want {
				t.Fatalf("exit=%d want=%d stderr=%s", code, tc.want, errOut.String())
			}
			if errOut.String() == "" {
				t.Fatalf("expected stderr")
			}
		})
	}
}

func TestGCRejectsBadKeepDays(t *testing.T) {
	ctx := context.Background()
	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	code := run(ctx, []string{"gc", "--keep-days", "nope"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
	if code != 2 {
		t.Fatalf("exit=%d stderr=%s", code, errOut.String())
	}
}

func TestGCNoRefsReturnsZero(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")

	out, errOut := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"gc", "--keep-days", "1"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("gc exit=%d stderr=%s", code, errOut.String())
		}
	})
}

func TestGC_DryRunAndDelete(t *testing.T) {
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

	// Create an old commit and store it under a patch ref.
	t.Setenv("GIT_AUTHOR_DATE", "2000-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2000-01-01T00:00:00Z")
	oldCommit := strings.TrimSpace(git(t, dir, "commit-tree", tree, "-p", base, "-m", "old"))
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/old", oldCommit)

	// A newer ref should remain.
	git(t, dir, "update-ref", "-m", "test", "refs/vwt/patches/new", base)

	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/old") {
		t.Fatalf("expected old ref to exist")
	}
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/new") {
		t.Fatalf("expected new ref to exist")
	}

	// Dry-run should print but not delete.
	gcOut, gcErr := bytes.Buffer{}, bytes.Buffer{}
	withChdir(t, dir, func() {
		code := run(ctx, []string{"gc", "--keep-days", "1", "--dry-run"}, IO{In: strings.NewReader(""), Out: &gcOut, Err: &gcErr})
		if code != 0 {
			t.Fatalf("gc dry-run exit=%d stderr=%s", code, gcErr.String())
		}
	})
	if !strings.Contains(gcOut.String(), "would drop refs/vwt/patches/old\n") {
		t.Fatalf("gc dry-run output missing drop line: %q", gcOut.String())
	}
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/old") {
		t.Fatalf("dry-run deleted old ref unexpectedly")
	}

	// Actual gc should delete old and keep new.
	gcOut.Reset()
	gcErr.Reset()
	withChdir(t, dir, func() {
		code := run(ctx, []string{"gc", "--keep-days", "1"}, IO{In: strings.NewReader(""), Out: &gcOut, Err: &gcErr})
		if code != 0 {
			t.Fatalf("gc exit=%d stderr=%s", code, gcErr.String())
		}
	})
	if gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/old") {
		t.Fatalf("expected old ref to be deleted")
	}
	if !gitExitOK(dir, "show-ref", "--verify", "--quiet", "refs/vwt/patches/new") {
		t.Fatalf("expected new ref to remain")
	}
}

func TestCatRejectsUnsafePaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "hello\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")

	cases := []struct {
		name string
		args []string
	}{
		{name: "dotgit", args: []string{"cat", ".git/config"}},
		{name: "dotgit-dot", args: []string{"cat", "./.git/config"}},
		{name: "absolute", args: []string{"cat", "/etc/passwd"}},
		{name: "traversal-start", args: []string{"cat", "../nope"}},
		{name: "traversal-mid", args: []string{"cat", "foo/../bar"}},
		{name: "traversal-end", args: []string{"cat", "foo/.."}},
		{name: "dot-only", args: []string{"cat", "./"}},
		{name: "control-chars", args: []string{"cat", "a\nb"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut := bytes.Buffer{}, bytes.Buffer{}
			withChdir(t, dir, func() {
				code := run(ctx, tc.args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
				if code != 2 {
					t.Fatalf("exit=%d stderr=%s", code, errOut.String())
				}
			})
			if errOut.String() == "" {
				t.Fatalf("expected stderr")
			}
		})
	}
}

func withChdir(t *testing.T, dir string, fn func()) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	fn()
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
