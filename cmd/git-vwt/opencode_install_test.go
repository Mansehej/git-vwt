package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeInstallWritesBundledFiles(t *testing.T) {
	orig := runOpenCodeBunInstall
	t.Cleanup(func() { runOpenCodeBunInstall = orig })

	called := false
	runOpenCodeBunInstall = func(ctx context.Context, root string) (bunInstallResult, error) {
		called = true
		if _, err := os.Stat(filepath.Join(root, ".opencode", "package.json")); err != nil {
			t.Fatalf("package.json not written before bun install: %v", err)
		}
		return bunInstallResult{Attempted: true, Message: "installed OpenCode plugin dependencies with bun"}, nil
	}

	dir := t.TempDir()
	var out, errOut bytes.Buffer
	withChdir(t, dir, func() {
		code := run(context.Background(), []string{"opencode", "install"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("opencode install exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !called {
		t.Fatal("expected bun install to be attempted")
	}
	if got := mustRead(t, filepath.Join(dir, "opencode.json")); got != opencodeInstallFiles[0].Content {
		t.Fatalf("unexpected opencode.json contents: %q", got)
	}
	if got := mustRead(t, filepath.Join(dir, ".opencode", "plugins", "vwt-mode.ts")); !strings.Contains(got, "VWT mode is enabled") {
		t.Fatalf("unexpected plugin contents: %q", got)
	}
	if !strings.Contains(out.String(), "run OpenCode with: OPENCODE_VWT=1 opencode") {
		t.Fatalf("missing usage hint: %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
}

func TestOpenCodeInstallRefusesOverwriteWithoutForce(t *testing.T) {
	orig := runOpenCodeBunInstall
	t.Cleanup(func() { runOpenCodeBunInstall = orig })
	runOpenCodeBunInstall = func(ctx context.Context, root string) (bunInstallResult, error) {
		t.Fatal("bun install should not run on overwrite failure")
		return bunInstallResult{}, nil
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "opencode.json"), "{}\n")
	var out, errOut bytes.Buffer
	withChdir(t, dir, func() {
		code := run(context.Background(), []string{"opencode", "install"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("opencode install exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "rerun with --force") {
		t.Fatalf("expected force hint, got %q", errOut.String())
	}
}

func TestOpenCodeInstallForceAndSkipBunInstall(t *testing.T) {
	orig := runOpenCodeBunInstall
	t.Cleanup(func() { runOpenCodeBunInstall = orig })
	runOpenCodeBunInstall = func(ctx context.Context, root string) (bunInstallResult, error) {
		t.Fatal("bun install should be skipped")
		return bunInstallResult{}, nil
	}

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "opencode.json"), "{}\n")
	var out, errOut bytes.Buffer
	withChdir(t, dir, func() {
		code := run(context.Background(), []string{"opencode", "install", "--force", "--skip-bun-install"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("opencode install exit=%d stderr=%s", code, errOut.String())
		}
	})
	if got := mustRead(t, filepath.Join(dir, "opencode.json")); got != opencodeInstallFiles[0].Content {
		t.Fatalf("unexpected opencode.json contents after force: %q", got)
	}
	if !strings.Contains(out.String(), "bun install skipped by flag") {
		t.Fatalf("expected skip message, got %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
}
