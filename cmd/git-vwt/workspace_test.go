package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceOpenWriteReadPatchApplyClose(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "hello\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")

	ws := "ws1"
	before := git(t, dir, "status", "--porcelain")

	// open (should not touch working directory)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "open"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("open exit=%d stderr=%s", code, errOut.String())
			}
		})
		fields := strings.Split(strings.TrimSpace(out.String()), "\t")
		if len(fields) != 3 {
			t.Fatalf("unexpected open output: %q", out.String())
		}
		if fields[0] != ws {
			t.Fatalf("unexpected ws name: %q", fields[0])
		}
		if fields[1] == "" || fields[2] == "" {
			t.Fatalf("unexpected open output: %q", out.String())
		}
	}

	after := git(t, dir, "status", "--porcelain")
	if before != after {
		t.Fatalf("worktree changed by open: before=%q after=%q", before, after)
	}

	// write (should not touch working directory)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "write", "hello.txt"}, IO{In: strings.NewReader("hello world\n"), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("write exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	if got := mustRead(t, filepath.Join(dir, "hello.txt")); got != "hello\n" {
		t.Fatalf("write modified working dir: %q", got)
	}

	// read (from workspace)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "read", "hello.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("read exit=%d stderr=%s", code, errOut.String())
			}
		})
		if got := out.String(); got != "hello world\n" {
			t.Fatalf("unexpected read output: %q", got)
		}
	}

	// patch
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "patch"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("patch exit=%d stderr=%s", code, errOut.String())
			}
		})
		if !strings.Contains(out.String(), "+hello world") {
			t.Fatalf("patch missing change: %q", out.String())
		}
	}

	// apply should modify working directory as unstaged changes
	headBefore := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "apply"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("apply exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	headAfter := strings.TrimSpace(git(t, dir, "rev-parse", "HEAD"))
	if headAfter != headBefore {
		t.Fatalf("apply moved HEAD: before=%s after=%s", headBefore, headAfter)
	}
	if got := mustRead(t, filepath.Join(dir, "hello.txt")); got != "hello world\n" {
		t.Fatalf("apply did not change working dir: %q", got)
	}
	if cached := strings.TrimSpace(git(t, dir, "diff", "--cached", "--name-only")); cached != "" {
		t.Fatalf("apply staged changes unexpectedly: %q", cached)
	}
	if status := git(t, dir, "status", "--porcelain"); status != " M hello.txt\n" {
		t.Fatalf("unexpected status after apply: %q", status)
	}

	// close
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "close"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("close exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	if ok := gitExitOK(dir, "show-ref", "--verify", "--quiet", wsRef(ws)); ok {
		t.Fatalf("workspace ref still exists after close")
	}
}

func TestWorkspaceBaseIsDirtyWorkingDirectorySnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "hello.txt"), "clean\n")
	git(t, dir, "add", "hello.txt")
	git(t, dir, "commit", "-m", "base")

	// Make repo dirty before workspace creation.
	mustWrite(t, filepath.Join(dir, "hello.txt"), "dirty\n")
	if status := git(t, dir, "status", "--porcelain"); status != " M hello.txt\n" {
		t.Fatalf("unexpected status: %q", status)
	}

	ws := "wsdirty"

	// read should reflect dirty working directory (snapshot base)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "read", "hello.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("read exit=%d stderr=%s", code, errOut.String())
			}
		})
		if got := out.String(); got != "dirty\n" {
			t.Fatalf("workspace did not see dirty base, got: %q", got)
		}
	}

	// patch should be empty (workspace head == snapshot base)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "patch"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("patch exit=%d stderr=%s", code, errOut.String())
			}
		})
		if strings.TrimSpace(out.String()) != "" {
			t.Fatalf("expected empty patch, got: %q", out.String())
		}
	}

	// write a new file and ensure patch does not include the pre-existing dirty change.
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "write", "agent.txt"}, IO{In: strings.NewReader("agent\n"), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("write exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "patch"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("patch exit=%d stderr=%s", code, errOut.String())
			}
		})
		p := out.String()
		if !strings.Contains(p, "diff --git a/agent.txt b/agent.txt") {
			t.Fatalf("patch missing agent file: %q", p)
		}
		if strings.Contains(p, "hello.txt") {
			t.Fatalf("patch unexpectedly includes dirty base file: %q", p)
		}
	}

	// apply should add agent.txt but not change hello.txt (already dirty)
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "apply"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("apply exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	if got := mustRead(t, filepath.Join(dir, "hello.txt")); got != "dirty\n" {
		t.Fatalf("apply changed dirty base file unexpectedly: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, "agent.txt")); got != "agent\n" {
		t.Fatalf("apply did not create agent file: %q", got)
	}
	status := git(t, dir, "status", "--porcelain")
	if !strings.Contains(status, " M hello.txt\n") || !strings.Contains(status, "?? agent.txt\n") {
		t.Fatalf("unexpected status after apply: %q", status)
	}
}

func TestApplyFallsBackToThreeWayAndWritesConflictMarkers(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "shared.txt"), "HEADER\nLINE=BASE\nFOOTER\n")
	git(t, dir, "add", "shared.txt")
	git(t, dir, "commit", "-m", "base")

	ws := "wsconflict"

	// Create a workspace change (theirs).
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "write", "shared.txt"}, IO{In: strings.NewReader("HEADER\nLINE=THEIRS\nFOOTER\n"), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("write exit=%d stderr=%s", code, errOut.String())
			}
		})
	}
	if got := mustRead(t, filepath.Join(dir, "shared.txt")); got != "HEADER\nLINE=BASE\nFOOTER\n" {
		t.Fatalf("workspace write modified working dir unexpectedly: %q", got)
	}

	// Modify working directory differently (ours).
	mustWrite(t, filepath.Join(dir, "shared.txt"), "HEADER\nLINE=OURS\nFOOTER\n")
	if status := git(t, dir, "status", "--porcelain"); status != " M shared.txt\n" {
		t.Fatalf("unexpected status before apply: %q", status)
	}

	// Apply should fall back to three-way and leave conflict markers.
	{
		out, errOut := bytes.Buffer{}, bytes.Buffer{}
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "apply"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 1 {
				t.Fatalf("apply exit=%d (want 1 for conflicts) stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}

	content := mustRead(t, filepath.Join(dir, "shared.txt"))
	if !strings.Contains(content, "<<<<<<< ours") || !strings.Contains(content, ">>>>>>> theirs") {
		t.Fatalf("expected conflict markers, got: %q", content)
	}
	if !strings.Contains(content, "LINE=OURS") || !strings.Contains(content, "LINE=THEIRS") {
		t.Fatalf("expected both versions present, got: %q", content)
	}
	if cached := strings.TrimSpace(git(t, dir, "diff", "--cached", "--name-only")); cached != "" {
		t.Fatalf("apply staged changes unexpectedly: %q", cached)
	}
}

func TestApplyJSONReportsConflictStatusAndPaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "shared.txt"), "HEADER\nLINE=BASE\nFOOTER\n")
	git(t, dir, "add", "shared.txt")
	git(t, dir, "commit", "-m", "base")

	ws := "wsjson"
	withChdir(t, dir, func() {
		code := run(ctx, []string{"--ws", ws, "write", "shared.txt"}, IO{
			In:  strings.NewReader("HEADER\nLINE=THEIRS\nFOOTER\n"),
			Out: &bytes.Buffer{},
			Err: &bytes.Buffer{},
		})
		if code != 0 {
			t.Fatalf("write exit=%d", code)
		}
	})

	mustWrite(t, filepath.Join(dir, "shared.txt"), "HEADER\nLINE=OURS\nFOOTER\n")

	var out, errOut bytes.Buffer
	withChdir(t, dir, func() {
		code := run(ctx, []string{"--ws", ws, "apply", "--json"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("apply --json exit=%d (want 1) stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	})

	var result applyJSONResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid json output: %v; output=%q", err, out.String())
	}
	if result.Status != "conflicted" {
		t.Fatalf("unexpected status: %q", result.Status)
	}
	if len(result.Paths) != 1 || result.Paths[0] != "shared.txt" {
		t.Fatalf("unexpected conflict paths: %#v", result.Paths)
	}
	content := mustRead(t, filepath.Join(dir, "shared.txt"))
	if !strings.Contains(content, "<<<<<<< ours") || !strings.Contains(content, ">>>>>>> theirs") {
		t.Fatalf("expected conflict markers, got: %q", content)
	}
}

func TestWorkspaceInfoMoveRemoveListAndSearch(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	git(t, dir, "init")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "docs", "guide.txt"), "alpha\nbeta\n")
	mustWrite(t, filepath.Join(dir, "notes", "todo.txt"), "alpha task\n")
	mustWrite(t, filepath.Join(dir, "top.txt"), "root\n")
	git(t, dir, "add", "docs/guide.txt", "notes/todo.txt", "top.txt")
	git(t, dir, "commit", "-m", "base")

	ws := "wsops"

	var openOut bytes.Buffer
	withChdir(t, dir, func() {
		code := run(ctx, []string{"--ws", ws, "open"}, IO{In: strings.NewReader(""), Out: &openOut, Err: &bytes.Buffer{}})
		if code != 0 {
			t.Fatalf("open exit=%d", code)
		}
	})
	openFields := strings.Split(strings.TrimSpace(openOut.String()), "\t")
	if len(openFields) != 3 {
		t.Fatalf("unexpected open output: %q", openOut.String())
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "info"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("info exit=%d stderr=%s", code, errOut.String())
			}
		})
		if got := strings.TrimSpace(out.String()); got != strings.TrimSpace(openOut.String()) {
			t.Fatalf("info mismatch: got=%q want=%q", got, strings.TrimSpace(openOut.String()))
		}
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "ls"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("ls exit=%d stderr=%s", code, errOut.String())
			}
		})
		if got := out.String(); got != "docs\nnotes\ntop.txt\n" {
			t.Fatalf("unexpected ls output: %q", got)
		}
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "ls", "docs"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("ls docs exit=%d stderr=%s", code, errOut.String())
			}
		})
		if got := out.String(); got != "guide.txt\n" {
			t.Fatalf("unexpected docs ls output: %q", got)
		}
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "search", "alpha", "--", "docs/*.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("search exit=%d stderr=%s", code, errOut.String())
			}
		})
		got := out.String()
		if !strings.Contains(got, openFields[1]+":docs/guide.txt:1:alpha") {
			t.Fatalf("search missing docs match: %q", got)
		}
		if strings.Contains(got, "notes/todo.txt") {
			t.Fatalf("search ignored pathspec: %q", got)
		}
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "mv", "docs/guide.txt", "docs/moved.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("mv exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "rm", "top.txt"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("rm exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "ls", "docs"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("ls docs after mv exit=%d stderr=%s", code, errOut.String())
			}
		})
		if got := out.String(); got != "moved.txt\n" {
			t.Fatalf("unexpected docs ls after mv: %q", got)
		}
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "patch"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("patch exit=%d stderr=%s", code, errOut.String())
			}
		})
		got := out.String()
		if !strings.Contains(got, "diff --git a/docs/guide.txt b/docs/guide.txt") {
			t.Fatalf("patch missing moved source delete: %q", got)
		}
		if !strings.Contains(got, "diff --git a/docs/moved.txt b/docs/moved.txt") {
			t.Fatalf("patch missing moved destination add: %q", got)
		}
		if !strings.Contains(got, "diff --git a/top.txt b/top.txt") {
			t.Fatalf("patch missing top.txt delete: %q", got)
		}
	}

	{
		var out, errOut bytes.Buffer
		withChdir(t, dir, func() {
			code := run(ctx, []string{"--ws", ws, "apply"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
			if code != 0 {
				t.Fatalf("apply exit=%d stderr=%s", code, errOut.String())
			}
		})
	}

	if _, err := os.Stat(filepath.Join(dir, "top.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected top.txt removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docs", "guide.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected docs/guide.txt removed, err=%v", err)
	}
	if got := mustRead(t, filepath.Join(dir, "docs", "moved.txt")); got != "alpha\nbeta\n" {
		t.Fatalf("unexpected moved file content: %q", got)
	}
}
