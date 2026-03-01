# Repository Guidelines

git-vwt is a Git-native "patch inbox" that stores agent-produced unified diffs as commits under private refs, without per-agent worktrees.

## Core Invariants

- Diff-only producers: when asked to propose changes, return a unified diff; do not write/edit files directly.
- Read-only commands: `git vwt import/compose/list/show/diff/export/cat/snapshot/gc` must not modify the user's working tree.
- Path safety: reject patches touching `.git/**`.
- Git safety: do not edit git config; do not use destructive git commands.

## Data Model (How Patches Are Stored)

- Patch commits are kept alive under private refs: `refs/vwt/patches/<id>`.
- Snapshots (synthetic base commits) are stored under: `refs/vwt/snapshots/<id>`.

## Recommended Agent Workflow

1. Choose a base commit.
   - Prefer explicit `git vwt import --base <rev> ...`.
   - If the repo is dirty, create a synthetic base with `git vwt snapshot` and use the snapshot commit as `--base`.
2. Produce a diff-only patch (unified diff with `diff --git` headers).
3. Import + inspect:

```bash
git vwt import --base <rev> --stdin
git vwt list
git vwt show <id>
git vwt diff <id>
```

4. (Optional) test the patch commit in an isolated environment (do not pollute the main checkout).
5. Apply and then drop:

```bash
git vwt apply <id>
git vwt drop <id>
```

## Patch Format Expectations

- Output a single unified diff (optionally inside one fenced ```diff block).
- Use relative paths only; no absolute paths and no `..` path traversal.
- Ensure the patch ends with a trailing newline (Git may treat missing final newlines as a corrupt patch).
- Keep diffs focused; avoid unrelated refactors/format churn.

## Worktrees (When You Actually Need Them)

This project exists to avoid per-agent worktrees for patch exchange. If you need multiple writer agents editing in parallel, use worktree isolation to prevent collisions, but be aware of the overhead (duplicated working files + per-worktree environment setup).

## Project Layout

- CLI entrypoint: `cmd/git-vwt/`
- Core logic: `internal/vwt/`
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
