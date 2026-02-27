---
name: virtual-worktree
description: Orchestrate diff-only agent patches with git-vwt (import, compose, inspect, cat, export, apply, drop) without creating per-agent worktrees.
---

# Virtual Worktree Orchestrator

You manage a Git-native "patch inbox" workflow using the `git vwt` CLI.

## Invariants

- Diff-only producers: subagents must return unified diffs; they never write files.
- Read-only commands (`import/compose/list/show/diff/export/cat/snapshot/gc`) must not modify the user's working tree.
- Default deny patches touching `.git/**`.

Unified diff correctness
- Hunks must use proper unified-diff headers with line ranges (no bare `@@`).

## Workflow

1. Choose a base commit.
   - Prefer an explicit `--base <rev>`.
   - If the repository is dirty, create a synthetic base with `git vwt snapshot` and use that snapshot commit as `--base`.
2. Import the diff:

```bash
git vwt import --base <rev> --stdin
```

3. Inspect:

```bash
git vwt list
git vwt show <id>
git vwt diff <id>
```

Preview a composed "shadow" state (no working tree changes):

```bash
git vwt compose --base <rev> [--id <shadow-id>] <id>...
git vwt cat <shadow-id> <path>
```

Read HEAD version of a file (no patch id needed):

```bash
git vwt cat <path>
```

Notes:
- `compose` creates a new synthetic patch commit under `refs/vwt/patches/<shadow-id>`.
- You can base follow-up patches on the composed state:

```bash
git vwt import --base refs/vwt/patches/<shadow-id> --stdin
```

4. Export (portable patch email):

```bash
git vwt export <id>
```

5. Apply:

```bash
git vwt apply <id>
```

Applies patch changes into the working tree as unstaged changes (no commit created).

6. Drop when done:

```bash
git vwt drop <id>
```
