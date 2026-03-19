package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenCodeInstallWritesBundledFiles(t *testing.T) {
	orig := runOpenCodeBunInstall
	origUserConfigDir := userConfigDir
	t.Cleanup(func() { runOpenCodeBunInstall = orig })
	t.Cleanup(func() { userConfigDir = origUserConfigDir })

	called := false
	runOpenCodeBunInstall = func(ctx context.Context, root string) (bunInstallResult, error) {
		called = true
		if _, err := os.Stat(filepath.Join(root, "package.json")); err != nil {
			t.Fatalf("package.json not written before bun install: %v", err)
		}
		return bunInstallResult{Attempted: true, Message: "installed OpenCode plugin dependencies with bun"}, nil
	}

	configRoot := t.TempDir()
	userConfigDir = func() (string, error) { return configRoot, nil }
	expectedRoot := filepath.Join(configRoot, "opencode")
	var out, errOut bytes.Buffer
	withChdir(t, t.TempDir(), func() {
		code := run(context.Background(), []string{"opencode", "install"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("opencode install exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !called {
		t.Fatal("expected bun install to be attempted")
	}
	gotConfig := mustRead(t, filepath.Join(expectedRoot, "opencode.json"))
	if gotConfig != opencodeInstallFiles[0].Content {
		t.Fatalf("unexpected opencode.json contents: %q", gotConfig)
	}
	if !strings.Contains(gotConfig, `"vwt_patch"`) {
		t.Fatalf("expected vwt_patch to be primary-only, got %q", gotConfig)
	}
	if got := mustRead(t, filepath.Join(expectedRoot, "plugins", "vwt-mode.ts")); !strings.Contains(got, "VWT mode is enabled") || !strings.Contains(got, "patch: tool({") || !strings.Contains(got, "apply_patch: tool({") {
		t.Fatalf("unexpected plugin contents: %q", got)
	}
	if _, err := os.Stat(filepath.Join(expectedRoot, ".opencode", ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("global install should not write project .opencode files, err=%v", err)
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
	origUserConfigDir := userConfigDir
	t.Cleanup(func() { runOpenCodeBunInstall = orig })
	t.Cleanup(func() { userConfigDir = origUserConfigDir })
	runOpenCodeBunInstall = func(ctx context.Context, root string) (bunInstallResult, error) {
		t.Fatal("bun install should not run on overwrite failure")
		return bunInstallResult{}, nil
	}

	configRoot := t.TempDir()
	userConfigDir = func() (string, error) { return configRoot, nil }
	globalRoot := filepath.Join(configRoot, "opencode")
	mustWrite(t, filepath.Join(globalRoot, "opencode.json"), "{}\n")
	var out, errOut bytes.Buffer
	withChdir(t, t.TempDir(), func() {
		code := run(context.Background(), []string{"opencode", "install"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 1 {
			t.Fatalf("opencode install exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !strings.Contains(errOut.String(), "rerun with --force") {
		t.Fatalf("expected force hint, got %q", errOut.String())
	}
}

func TestOpenCodeInstallProjectForceAndSkipBunInstall(t *testing.T) {
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
		code := run(context.Background(), []string{"opencode", "install", "--project", "--force", "--skip-bun-install"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
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

func TestOpenCodeInstallProjectWritesProjectLayout(t *testing.T) {
	orig := runOpenCodeBunInstall
	t.Cleanup(func() { runOpenCodeBunInstall = orig })

	called := false
	runOpenCodeBunInstall = func(ctx context.Context, root string) (bunInstallResult, error) {
		called = true
		if !strings.HasSuffix(filepath.ToSlash(root), "/.opencode") {
			t.Fatalf("expected bun install in project .opencode dir, got %s", root)
		}
		return bunInstallResult{Attempted: true, Message: "installed OpenCode plugin dependencies with bun"}, nil
	}

	dir := t.TempDir()
	var out, errOut bytes.Buffer
	withChdir(t, dir, func() {
		code := run(context.Background(), []string{"opencode", "install", "--project"}, IO{In: strings.NewReader(""), Out: &out, Err: &errOut})
		if code != 0 {
			t.Fatalf("opencode install exit=%d stderr=%s", code, errOut.String())
		}
	})
	if !called {
		t.Fatal("expected bun install to be attempted")
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode", ".gitignore")); err != nil {
		t.Fatalf("expected project .gitignore, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".opencode", "package.json")); err != nil {
		t.Fatalf("expected project package.json, err=%v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
}

func TestOpenCodeInstallAssetsMatchSourceFiles(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	expected := map[string]string{
		"opencode.json":                 mustRead(t, filepath.Join(repoRoot, "opencode.json")),
		".opencode/.gitignore":          mustRead(t, filepath.Join(repoRoot, ".opencode", ".gitignore")),
		".opencode/package.json":        mustRead(t, filepath.Join(repoRoot, ".opencode", "package.json")),
		".opencode/bun.lock":            mustRead(t, filepath.Join(repoRoot, ".opencode", "bun.lock")),
		".opencode/plugins/vwt-mode.ts": mustRead(t, filepath.Join(repoRoot, ".opencode", "plugins", "vwt-mode.ts")),
	}

	if len(opencodeInstallFiles) != len(expected) {
		t.Fatalf("asset count mismatch: got %d want %d", len(opencodeInstallFiles), len(expected))
	}

	seen := make(map[string]bool, len(opencodeInstallFiles))
	for _, asset := range opencodeInstallFiles {
		want, ok := expected[asset.Path]
		if !ok {
			t.Fatalf("unexpected bundled asset %q", asset.Path)
		}
		seen[asset.Path] = true
		if asset.Content != want {
			t.Fatalf("bundled asset %q is out of sync with source file", asset.Path)
		}
	}

	for assetPath := range expected {
		if !seen[assetPath] {
			t.Fatalf("missing bundled asset %q", assetPath)
		}
	}
}
