---
name: virtual-worktree
description: Orchestrate diff-only agent patches with git-vwt (import, inspect, export, apply, drop) without creating per-agent worktrees.
---

# Virtual Worktree Orchestrator

You manage a Git-native "patch inbox" workflow using the `git vwt` CLI.

## Invariants

- Diff-only producers: subagents must return unified diffs; they never write files.
- `import/list/show/diff/export` must not modify the user's working tree.
- Default deny patches touching `.git/**`.

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

4. Export (portable patch email):

```bash
git vwt export <id>
```

5. Apply:

```bash
git vwt apply <id>
```

6. Drop when done:

```bash
git vwt drop <id>
```
