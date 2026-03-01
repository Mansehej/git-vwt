# git-vwt

Git-native virtual workspace for agents.

Agents use normal file operations (read/write/rm/mv/ls/search). `git vwt` stores the evolving workspace state as a Git commit under a private ref, without creating worktrees and without requiring agents to produce/apply hunks.

## Commands

- `git vwt open [--base <rev>|auto]`
  - Creates the workspace ref if missing.
  - `auto` means: if the repo is dirty, snapshot the current working directory and use that snapshot as the workspace base; otherwise use `HEAD`.
- `git vwt info`
- `git vwt read <path>`
- `git vwt write <path> [<src-file>]` (stdin if no src)
- `git vwt rm <path>`
- `git vwt mv <from> <to>`
- `git vwt ls [path]`
- `git vwt search <pattern> [-- <pathspec>...]`
- `git vwt patch` (unified diff vs workspace base)
- `git vwt apply` (apply workspace changes to the working directory as unstaged changes)
- `git vwt close` (delete the workspace ref)

Global flags:

- `--ws <name>` selects the workspace (default: `default`)
- `--agent <name>` sets the author name for workspace commits
- `--debug` prints git commands to stderr

## Build

From this directory:

```bash
go test ./...
go build -o git-vwt ./cmd/git-vwt
```

Put `git-vwt` on your PATH to use as `git vwt ...`.

## Safety

- Workspace operations (`open/info/read/write/rm/mv/ls/search/patch`) do not touch your working tree.
- `apply` is the only command that modifies the working tree.
- Paths under `.git/**` are refused.

## Skills

See `skills/` for cross-tool Agent Skills that orchestrate `git vwt` workflows.
