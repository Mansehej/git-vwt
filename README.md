# git-vwt

[![CI](https://github.com/Mansehej/git-vwt/actions/workflows/ci.yml/badge.svg)](https://github.com/Mansehej/git-vwt/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)

`git-vwt` is a Git subcommand (`git vwt`) for agent-safe editing.

It stores workspace state as commits under
`refs/vwt/workspaces/<name>`, so tools can read/write/search files without
touching your working directory. You inspect the resulting diff, then apply it
once with `git vwt apply`.

```bash
git vwt --ws demo open
printf 'hello from vwt\n' | git vwt --ws demo write notes/hello.txt
git vwt --ws demo patch
git vwt --ws demo apply
```

## Table of contents

- [Why use it](#why-use-it)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Command reference](#command-reference)
- [Workspace behavior](#workspace-behavior)
- [Safety guarantees](#safety-guarantees)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Integrations and skills](#integrations-and-skills)

## Why use it

- No per-agent worktrees to create or clean up.
- Workspace operations are isolated from your working directory.
- `git vwt patch` is deterministic (always `base..head`).
- Works as a standard Git extension once `git-vwt` is on `PATH`.

## Requirements

- Git
- Go 1.22+

## Installation

### Option 1: Build from source (recommended)

From the repository root:

```bash
go build -o git-vwt ./cmd/git-vwt
install -m 0755 git-vwt "$HOME/.local/bin/git-vwt"
```

Make sure `$HOME/.local/bin` is on your `PATH`, then verify:

```bash
git vwt help
```

### Option 2: Install with Go from a local clone

From the repository root:

```bash
go install ./cmd/git-vwt
```

This installs `git-vwt` into your Go bin directory (`$GOBIN`, or
`$(go env GOPATH)/bin` by default).

## Quick start

```bash
# 1) Open (or reuse) a workspace
git vwt --ws demo open

# 2) Edit workspace files (does NOT touch your working directory)
printf 'hello from vwt\n' | git vwt --ws demo write notes/hello.txt
git vwt --ws demo read notes/hello.txt
git vwt --ws demo search hello -- '*.txt'

# 3) Inspect the workspace diff vs its base
git vwt --ws demo patch

# 4) Apply that diff to your working directory as unstaged changes
git vwt --ws demo apply

# 5) Close when done
git vwt --ws demo close
```

## Command reference

| Command | What it does |
| --- | --- |
| `git vwt open [--base <rev>|auto]` | Create workspace if missing. |
| `git vwt info` | Print `<workspace> <head> <base>`. |
| `git vwt read <path>` | Read file from workspace. |
| `git vwt write <path> [<src-file>]` | Write file into workspace (stdin if no source file). |
| `git vwt rm <path>` | Remove file from workspace. |
| `git vwt mv <from> <to>` | Rename/move a file in workspace. |
| `git vwt ls [path]` | List workspace directory entries. |
| `git vwt search <pattern> [-- <pathspec>...]` | Search workspace content. |
| `git vwt patch` | Print unified diff (`base..head`). |
| `git vwt apply` | Apply workspace diff to working directory. |
| `git vwt close` | Delete workspace ref. |

### Global flags and env vars

| Flag / env var | Description |
| --- | --- |
| `--ws <name>` / `VWT_WORKSPACE` | Workspace name (default: `default`). |
| `--agent <name>` / `VWT_AGENT` | Author name for workspace commits. |
| `--debug` | Print underlying Git commands to stderr. |

## Workspace behavior

- Existing workspace refs are reused.
- Missing workspaces are created automatically by commands that need one.
- `open --base auto` chooses the base commit like this:
  - clean repo: base is `HEAD`
  - dirty repo (or no `HEAD`): base is a synthetic snapshot of current working
    directory state (includes untracked files, excludes ignored files)

This means pre-existing dirty changes are treated as base context, not as
workspace edits.

## Safety guarantees

- `open/info/read/write/rm/mv/ls/search/patch` do not modify your working tree.
- `apply` is the only command that updates your working directory.
- `apply` leaves `HEAD` and index unchanged (changes are unstaged).
- Unsafe paths are rejected (absolute paths, `..`, and `.git/**`).

## Troubleshooting

- `git: 'vwt' is not a git command`
  - Ensure the binary is named `git-vwt` and is on your `PATH`.
  - Confirm with `command -v git-vwt`.
- `permission denied` when installing to `~/.local/bin`
  - Create the directory first: `mkdir -p "$HOME/.local/bin"`.
- `not a git repository`
  - Run commands from inside a Git repository.

## Development

From the repository root:

```bash
go test ./...
go build -o git-vwt ./cmd/git-vwt
```

## Benchmarks

Bench scripts live under `bench/`.

- Single-session subagent benchmark: `python3 bench/webapp_bench.py`
  - Measures serial vs subagents vs worktrees for a tiny static webapp scaffold.
- Multi-process benchmark (recommended): `python3 bench/process_bench.py --components 8 --workers 8`
  - Runs N parallel `opencode run` processes and compares:
    - serial execution
    - parallel work via `git worktree`
    - parallel work via git-vwt workspaces (no worktrees)
  - Also reports disk overhead (worktree checkout bytes vs `.git/objects` growth).

## Integrations and skills

This repo includes cross-tool skill definitions under `skills/`.

See `docs/INTEGRATIONS.md` for Claude Code, OpenCode, and Codex installation
paths.
