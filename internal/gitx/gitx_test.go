package gitx

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerWithEnv(t *testing.T) {
	r := Runner{Dir: ".", Env: []string{"A=1"}}
	r2 := r.WithEnv(map[string]string{"A": "x", "B": "2"})
	if len(r.Env) != 1 || r.Env[0] != "A=1" {
		t.Fatalf("original Env mutated: %v", r.Env)
	}
	if got := strings.Join(r2.Env, ","); got != "A=x,B=2" {
		t.Fatalf("unexpected merged env: %q", got)
	}
}

func TestMergeEnv_OverridesAndSorts(t *testing.T) {
	base := []string{"B=2", "A=1"}
	kv := map[string]string{"A": "x", "C": "3"}
	got := mergeEnv(base, kv)
	want := []string{"A=x", "B=2", "C=3"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx=%d got=%q want=%q full=%v", i, got[i], want[i], got)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote([]string{"a", "b c"}); got != "a 'b c'" {
		t.Fatalf("got=%q", got)
	}
	if got := shellQuote([]string{""}); got != "''" {
		t.Fatalf("got=%q", got)
	}
	if got := shellQuote([]string{"a'b"}); got != "'a'\\''b'" {
		t.Fatalf("got=%q", got)
	}
}

func TestRunGit_DebugLogging(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	var dbg bytes.Buffer
	r := Runner{Dir: dir, Env: os.Environ(), Debug: true, DebugWriter: &dbg}
	_, _ = r.RunGit(ctx, nil, "rev-parse", "--git-dir")
	if !strings.Contains(dbg.String(), "+ git rev-parse") {
		t.Fatalf("expected debug log, got: %q", dbg.String())
	}
}

func TestRunGit_SetsExitCodeFromGit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	r := Runner{Dir: dir, Env: os.Environ()}
	res, err := r.RunGit(ctx, nil, "rev-parse", "--git-dir")
	if err == nil {
		t.Fatalf("expected error")
	}
	if res.ExitCode <= 0 {
		t.Fatalf("expected non-zero exit code, got %d", res.ExitCode)
	}
}

func TestRunGit_SetsExitCodeMinusOneOnStartFailure(t *testing.T) {
	ctx := context.Background()
	badDir := filepath.Join(t.TempDir(), "does-not-exist")

	r := Runner{Dir: badDir, Env: os.Environ()}
	res, err := r.RunGit(ctx, nil, "status")
	if err == nil {
		t.Fatalf("expected error")
	}
	if res.ExitCode != -1 {
		t.Fatalf("expected exit=-1, got %d", res.ExitCode)
	}
}
