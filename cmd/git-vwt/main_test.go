package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
