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
var userConfigDir = os.UserConfigDir

type opencodeInstallTarget struct {
	Root      string
	PluginDir string
	Mode      string
}

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
	project := fs.Bool("project", false, "install into the current project instead of the global OpenCode config")
	dir := fs.String("dir", "", "override the target directory")
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

	target, err := resolveOpenCodeInstallTarget(*project, *dir)
	if err != nil {
		fmt.Fprintf(stdio.Err, "opencode install: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(target.Root, 0o755); err != nil {
		fmt.Fprintf(stdio.Err, "opencode install: create dir: %v\n", err)
		return 1
	}

	written, unchanged, err := installOpenCodeFiles(target, *force)
	if err != nil {
		fmt.Fprintf(stdio.Err, "opencode install: %v\n", err)
		return 1
	}

	bunResult := bunInstallResult{Skipped: true, Message: "bun install skipped by flag"}
	if !*skipBunInstall {
		bunResult, err = runOpenCodeBunInstall(ctx, target.PluginDir)
		if err != nil {
			fmt.Fprintf(stdio.Out, "installed OpenCode plugin files in %s %s (%d written, %d unchanged)\n", target.Mode, target.Root, written, unchanged)
			fmt.Fprintf(stdio.Err, "opencode install: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdio.Out, "installed OpenCode plugin files in %s %s (%d written, %d unchanged)\n", target.Mode, target.Root, written, unchanged)
	if bunResult.Message != "" {
		fmt.Fprintln(stdio.Out, bunResult.Message)
	}
	fmt.Fprintln(stdio.Out, "run OpenCode with: OPENCODE_VWT=1 opencode")
	return 0
}

func resolveOpenCodeInstallTarget(project bool, dir string) (opencodeInstallTarget, error) {
	if strings.TrimSpace(dir) != "" {
		root, err := filepath.Abs(dir)
		if err != nil {
			return opencodeInstallTarget{}, fmt.Errorf("resolve dir: %v", err)
		}
		if project {
			return opencodeInstallTarget{Root: root, PluginDir: filepath.Join(root, ".opencode"), Mode: "project"}, nil
		}
		return opencodeInstallTarget{Root: root, PluginDir: root, Mode: "global"}, nil
	}
	if project {
		root, err := os.Getwd()
		if err != nil {
			return opencodeInstallTarget{}, fmt.Errorf("resolve current working directory: %v", err)
		}
		return opencodeInstallTarget{Root: root, PluginDir: filepath.Join(root, ".opencode"), Mode: "project"}, nil
	}
	if cfg := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG_DIR")); cfg != "" {
		root, err := filepath.Abs(cfg)
		if err != nil {
			return opencodeInstallTarget{}, fmt.Errorf("resolve OPENCODE_CONFIG_DIR: %v", err)
		}
		return opencodeInstallTarget{Root: root, PluginDir: root, Mode: "global"}, nil
	}
	base, err := userConfigDir()
	if err != nil {
		return opencodeInstallTarget{}, fmt.Errorf("resolve user config dir: %v", err)
	}
	root := filepath.Join(base, "opencode")
	return opencodeInstallTarget{Root: root, PluginDir: root, Mode: "global"}, nil
}

func installOpenCodeFiles(target opencodeInstallTarget, force bool) (written int, unchanged int, err error) {
	assets := openCodeInstallFilesForTarget(target.Mode == "project")
	if len(assets) == 0 {
		return 0, 0, fmt.Errorf("no OpenCode install assets available")
	}

	root := target.Root
	for _, asset := range assets {
		path := filepath.Join(root, filepath.FromSlash(asset.Path))
		current, readErr := os.ReadFile(path)
		switch {
		case readErr == nil:
			if bytes.Equal(current, []byte(asset.Content)) {
				unchanged++
				continue
			}
			if !force {
				return 0, 0, fmt.Errorf("refusing to overwrite %s; rerun with --force", path)
			}
		case os.IsNotExist(readErr):
			// write below
		case readErr != nil:
			return 0, 0, readErr
		}
	}

	written = 0
	unchanged = 0
	for _, asset := range assets {
		path := filepath.Join(root, filepath.FromSlash(asset.Path))
		current, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Equal(current, []byte(asset.Content)) {
			unchanged++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return 0, 0, err
		}
		if err := os.WriteFile(path, []byte(asset.Content), 0o644); err != nil {
			return 0, 0, err
		}
		written++
	}
	return written, unchanged, nil
}

func openCodeInstallFilesForTarget(project bool) []opencodeInstallFile {
	assets := make([]opencodeInstallFile, 0, len(opencodeInstallFiles))
	for _, asset := range opencodeInstallFiles {
		if project {
			assets = append(assets, asset)
			continue
		}
		switch asset.Path {
		case ".opencode/.gitignore":
			continue
		case ".opencode/package.json":
			assets = append(assets, opencodeInstallFile{Path: "package.json", Content: asset.Content})
		case ".opencode/bun.lock":
			assets = append(assets, opencodeInstallFile{Path: "bun.lock", Content: asset.Content})
		case ".opencode/plugins/vwt-mode.ts":
			assets = append(assets, opencodeInstallFile{Path: "plugins/vwt-mode.ts", Content: asset.Content})
		default:
			assets = append(assets, asset)
		}
	}
	return assets
}

func defaultRunOpenCodeBunInstall(ctx context.Context, root string) (bunInstallResult, error) {
	if _, err := exec.LookPath("bun"); err != nil {
		return bunInstallResult{
			Skipped: true,
			Message: fmt.Sprintf("bun not found; run `bun install --cwd %s` after installing Bun", filepath.ToSlash(root)),
		}, nil
	}

	cmd := exec.CommandContext(ctx, "bun", "install", "--frozen-lockfile")
	cmd.Dir = root
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
	fmt.Fprintln(w, "usage: git vwt opencode install [--project] [--dir <path>] [--force] [--skip-bun-install]")
}
