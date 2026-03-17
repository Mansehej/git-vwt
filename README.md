# git-vwt

[![CI](https://github.com/Mansehej/git-vwt/actions/workflows/ci.yml/badge.svg)](https://github.com/Mansehej/git-vwt/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)

Virtual workspaces for agent-safe editing.

`git-vwt` adds a `git vwt` subcommand that lets tools write changes into an isolated virtual workspace (stored as a commit under `refs/vwt/workspaces/<name>`) instead of touching your checked-out working directory. This gives you worktree-like isolation without the checkout/disk overhead and cleanup burden of spawning N worktrees. You inspect a unified diff, then apply it as normal unstaged changes when you're ready.

This repo also ships an OpenCode plugin (`.opencode/plugins/vwt-mode.ts`) that routes subagent edits into per-session virtual workspaces, so you can parallelize agent work without creating Git worktrees.

```bash
git vwt --ws demo open
printf 'hello from vwt\n' | git vwt --ws demo write notes/hello.txt
git vwt --ws demo patch
git vwt --ws demo apply
```

## Table of contents

- [Why](#why)
- [OpenCode quickstart (experimental)](#opencode-quickstart-experimental)
- [Worktrees vs git-vwt](#worktrees-vs-git-vwt)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Installation](#installation)
- [CLI quick start](#cli-quick-start)
- [Command reference](#command-reference)
- [Workspace behavior](#workspace-behavior)
- [Safety guarantees](#safety-guarantees)
- [Benchmarks](#benchmarks)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Release process](#release-process)
- [Changelog](#changelog)
- [Integrations and skills](#integrations-and-skills)

## Why

- Parallel agent work without worktree sprawl.
- Keep your working directory clean until you choose to apply.
- Deterministic patch (`base..head`) you can review.
- Conflict-friendly apply: if parallel work overlaps, `apply` can fall back to conflict markers instead of dropping a patch.
- Git-native: workspace state is just commits and refs.

## OpenCode quickstart (experimental)

The bundled OpenCode plugin is still experimental. The core `git vwt` CLI is the stable surface; the OpenCode integration is shipped for early adopters and may continue to evolve as OpenCode's plugin APIs change.

Build the workspace CLI:

```bash
go build -o git-vwt ./cmd/git-vwt
```

Run OpenCode with VWT mode enabled:

```bash
OPENCODE_VWT=1 opencode
```

Default behavior:

- Primary session edits the working directory normally.
- Subagent sessions write to isolated workspaces named `opencode-<sessionID>`.
- The plugin sends synthetic child-to-parent orchestration messages so the primary can apply and close subagent workspaces automatically.
- On startup, the plugin also sweeps orphaned `opencode-*` workspace refs whose sessions no longer exist.

Optional (advanced): isolate primary sessions too (useful when running many `opencode run` processes in parallel):

```bash
OPENCODE_VWT=1 OPENCODE_VWT_PRIMARY=1 opencode
```

See `docs/harnesses/opencode-vwt.md` for more details.

## Worktrees vs git-vwt

Use `git worktree` when:

- You need a full on-disk checkout per worker (running tests/builds/servers concurrently).
- You need to run tooling against each isolated change set before integration.
- Your workflow depends on tools that mutate lots of files via `bash` (formatters, codegen) and must run against the modified tree.
- You want separate branches/commit history per worker.

Use `git vwt` when:

- You want many lightweight isolated edit buffers with minimal disk overhead.
- Your workers are primarily file read/write/search operations with a final merge/apply step.
- You can defer running tooling until after you apply/integrate.
- You want conflicts surfaced as markers in one checkout instead of juggling worktrees.

## How it works

- A workspace is a commit whose tree is the "workspace view".
- The workspace base is the parent commit; the workspace head is the workspace commit.
- `open/info/read/write/rm/mv/ls/search/patch` operate on the workspace tree and do not touch your checkout.
- `patch` prints `git diff <base>..<head>`.
- `apply` applies that diff to your checkout as unstaged changes:
  - exit `0`: applied cleanly
  - exit `1`: either (a) applied with conflicts (conflict markers written), or (b) failed; check the output
  - `apply --json`: machine-readable status for automation (`clean`, `conflicted`, `failed`)

Base selection (`open --base auto`):

- Clean repo: base is `HEAD`.
- Dirty repo (or no `HEAD`): base is a synthetic snapshot of the current working directory (includes untracked files, excludes ignored files).

This means pre-existing dirty changes are treated as base context, not workspace edits.

## Requirements

- Git
- Go 1.22+

## Installation

### Option 1: install from GitHub Releases

Download the archive for your platform from GitHub Releases, extract it, and install the `git-vwt` binary somewhere on your `PATH`.

- Release page: `https://github.com/Mansehej/git-vwt/releases`
- Verify downloads with `checksums.txt`

### Option 2: install with Homebrew

```bash
brew tap Mansehej/git-vwt
brew install git-vwt
```

Then verify:

```bash
git vwt version
```

To upgrade later:

```bash
brew upgrade git-vwt
```

### Option 3: build from source

From the repository root:

```bash
go build -o git-vwt ./cmd/git-vwt
install -m 0755 git-vwt "$HOME/.local/bin/git-vwt"
```

Make sure `$HOME/.local/bin` is on your `PATH`, then verify:

```bash
git vwt version
```

### Option 4: install with Go from a local clone

From the repository root:

```bash
go install ./cmd/git-vwt
```

This installs `git-vwt` into your Go bin directory (`$GOBIN`, or `$(go env GOPATH)/bin` by default).

## CLI quick start

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
| `git vwt version` | Print the CLI version. |
| `git vwt version --check` | Print the CLI version and check for a newer release. |
| `git vwt version --check --json` | Print machine-readable version/update status for agent integrations. |
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
| `--version` | Print the CLI version. |
| `--ws <name>` / `VWT_WORKSPACE` | Workspace name (default: `default`). |
| `--agent <name>` / `VWT_AGENT` | Author name for workspace commits. |
| `VWT_NO_UPDATE_CHECK=1` | Disable update checks. |
| `--debug` | Print underlying Git commands to stderr. |

## Updates

- Homebrew installs update with `brew upgrade git-vwt`.
- GitHub Release installs update by downloading the newer archive and replacing the existing binary.
- Run `git vwt version --check` to check for a newer release.
- Run `git vwt version --check --json` for machine-readable status in agent integrations.
- The OpenCode plugin can use that status to prompt the agent to ask the user about updating at the end of a response, without polluting normal CLI command output.

## Workspace behavior

- Existing workspace refs are reused.
- Missing workspaces are created automatically by commands that need one.
- `open --base auto` chooses the base commit like this:
  - clean repo: base is `HEAD`
  - dirty repo (or no `HEAD`): base is a synthetic snapshot of current working directory state (includes untracked files, excludes ignored files)

## Safety guarantees

- `open/info/read/write/rm/mv/ls/search/patch` do not modify your working tree.
- `apply` is the only command that updates your working directory.
- `apply` leaves `HEAD` and index unchanged (changes are unstaged).
- `apply` may write conflict markers and exit `1` when conflicts occur.
- Unsafe paths are rejected (absolute paths, `..`, and `.git/**`).

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

## Release process

- Release metadata lives in `CHANGELOG.md`.
- Tagging a `v*` release triggers `.github/workflows/release.yml` to build GitHub release archives for Linux, macOS, and Windows, plus a `checksums.txt` manifest.
- The release workflow automatically updates `Formula/git-vwt.rb` on `main` so Homebrew stays aligned with the latest tagged release.
- The release checklist is documented in `docs/RELEASING.md`.

## Changelog

See `CHANGELOG.md`.

## Integrations and skills

This repo includes cross-tool skill definitions under `skills/`.

See `docs/INTEGRATIONS.md` for Claude Code, OpenCode, and Codex installation paths.
