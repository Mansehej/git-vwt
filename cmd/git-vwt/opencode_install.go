package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type opencodeInstallFile struct {
	Path    string
	Content string
}

type bunInstallResult struct {
	Attempted bool
	Skipped   bool
	Message   string
}

var runOpenCodeBunInstall = defaultRunOpenCodeBunInstall

func cmdOpenCode(ctx context.Context, argv []string, stdio IO) int {
	if len(argv) == 0 {
		fmt.Fprintln(stdio.Err, "opencode: expected subcommand")
		writeOpenCodeInstallUsage(stdio.Err)
		return 2
	}
	switch argv[0] {
	case "install":
		return cmdOpenCodeInstall(ctx, argv[1:], stdio)
	default:
		fmt.Fprintf(stdio.Err, "opencode: unknown subcommand: %s\n", argv[0])
		writeOpenCodeInstallUsage(stdio.Err)
		return 2
	}
}

func cmdOpenCodeInstall(ctx context.Context, argv []string, stdio IO) int {
	fs := flag.NewFlagSet("opencode install", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target project directory")
	force := fs.Bool("force", false, "overwrite conflicting existing files")
	skipBunInstall := fs.Bool("skip-bun-install", false, "write files but skip bun install")
	fs.SetOutput(stdio.Err)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stdio.Err, "opencode install: no positional arguments")
		return 2
	}

	root, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(stdio.Err, "opencode install: resolve dir: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		fmt.Fprintf(stdio.Err, "opencode install: create dir: %v\n", err)
		return 1
	}

	written, unchanged, err := installOpenCodeFiles(root, *force)
	if err != nil {
		fmt.Fprintf(stdio.Err, "opencode install: %v\n", err)
		return 1
	}

	bunResult := bunInstallResult{Skipped: true, Message: "bun install skipped by flag"}
	if !*skipBunInstall {
		bunResult, err = runOpenCodeBunInstall(ctx, root)
		if err != nil {
			fmt.Fprintf(stdio.Out, "installed OpenCode plugin files in %s (%d written, %d unchanged)\n", root, written, unchanged)
			fmt.Fprintf(stdio.Err, "opencode install: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdio.Out, "installed OpenCode plugin files in %s (%d written, %d unchanged)\n", root, written, unchanged)
	if bunResult.Message != "" {
		fmt.Fprintln(stdio.Out, bunResult.Message)
	}
	fmt.Fprintln(stdio.Out, "run OpenCode with: OPENCODE_VWT=1 opencode")
	return 0
}

func installOpenCodeFiles(root string, force bool) (written int, unchanged int, err error) {
	for _, asset := range opencodeInstallFiles {
		target := filepath.Join(root, filepath.FromSlash(asset.Path))
		current, readErr := os.ReadFile(target)
		switch {
		case readErr == nil:
			if bytes.Equal(current, []byte(asset.Content)) {
				unchanged++
				continue
			}
			if !force {
				return 0, 0, fmt.Errorf("refusing to overwrite %s; rerun with --force", target)
			}
		case os.IsNotExist(readErr):
			// write below
		case readErr != nil:
			return 0, 0, readErr
		}
	}

	written = 0
	unchanged = 0
	for _, asset := range opencodeInstallFiles {
		target := filepath.Join(root, filepath.FromSlash(asset.Path))
		current, readErr := os.ReadFile(target)
		if readErr == nil && bytes.Equal(current, []byte(asset.Content)) {
			unchanged++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return 0, 0, err
		}
		if err := os.WriteFile(target, []byte(asset.Content), 0o644); err != nil {
			return 0, 0, err
		}
		written++
	}
	return written, unchanged, nil
}

func defaultRunOpenCodeBunInstall(ctx context.Context, root string) (bunInstallResult, error) {
	if _, err := exec.LookPath("bun"); err != nil {
		return bunInstallResult{
			Skipped: true,
			Message: "bun not found; run `bun install --cwd .opencode` after installing Bun",
		}, nil
	}

	cmd := exec.CommandContext(ctx, "bun", "install", "--frozen-lockfile")
	cmd.Dir = filepath.Join(root, ".opencode")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return bunInstallResult{Attempted: true}, fmt.Errorf("bun install failed: %s", msg)
	}
	return bunInstallResult{Attempted: true, Message: "installed OpenCode plugin dependencies with bun"}, nil
}

func writeOpenCodeInstallUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: git vwt opencode install [--dir <path>] [--force] [--skip-bun-install]")
}
