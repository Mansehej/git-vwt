# Repository Guidelines

git-vwt is a Git-native virtual workspace. Agents use normal file operations (read/write/rm/mv/ls/search) against a workspace stored as a commit under a private ref, without per-agent worktrees and without manually dealing with diff hunks.

## Core Invariants

- Workspace commands (`open/info/read/write/rm/mv/ls/search/patch`) must not modify the user's working directory.
- `git vwt apply` is the only command that modifies the working directory.
- Path safety: refuse paths under `.git/**` (and unsafe/absolute paths).
- Git safety: do not edit git config; do not use destructive git commands.

## Data Model

- Workspace state is stored under: `refs/vwt/workspaces/<name>`.
- The ref points at a commit whose tree is the workspace view.
- The workspace base commit is the parent of the workspace commit.
- When the repo is dirty, the base commit is a synthetic snapshot of the working directory.

## Recommended Agent Workflow

1. Open a workspace (auto-snapshots dirty working dir):

```bash
git vwt --ws <name> open
```

2. Read/write/search within the workspace:

```bash
git vwt read <path>
git vwt write <path>
git vwt search <pattern> -- <pathspec>...
```

3. Inspect the derived patch:

```bash
git vwt patch
```

4. Apply and then close:

```bash
git vwt apply
git vwt close
```

## Worktrees (When You Actually Need Them)

This project exists to avoid per-agent worktrees for patch exchange. Use a real worktree only when you explicitly need a checked-out directory to run tools/tests in isolation.

## Project Layout

- CLI entrypoint: `cmd/git-vwt/`
- Core logic: `cmd/git-vwt/` (workspace operations implemented in the CLI for now)
- Git runner helpers: `internal/gitx/`
- Docs: `docs/`
- Cross-tool skills: `skills/` (see `docs/INTEGRATIONS.md`)

## Build & Test

From `git-vwt/`:

```bash
go test ./...
go build -o git-vwt ./cmd/git-vwt
```

Put `git-vwt` on your `PATH` to use it as `git vwt ...`.

## Skills Distribution

This repo ships skill definitions under `skills/`.

- Claude Code: `.claude/skills/<skill>/SKILL.md` (repo) or `~/.claude/skills/<skill>/SKILL.md`
- OpenCode: `.opencode/skills/<skill>/SKILL.md` (repo) or `~/.config/opencode/skills/<skill>/SKILL.md`
- Codex: `.agents/skills/<skill>/SKILL.md` (repo) or `~/.agents/skills/<skill>/SKILL.md`
