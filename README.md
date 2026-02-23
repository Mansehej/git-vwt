# git-vwt

Git-native "patch inbox" that stores agent-produced unified diffs as commits under private refs, without per-agent worktrees.

MVP commands:

- `git vwt import --base <rev> [--id <id>] [--agent <name>] [--title <title>] [--stdin|<file>]`
- `git vwt list`
- `git vwt show <id>`
- `git vwt diff <id>`
- `git vwt export <id>`
- `git vwt apply <id> [--no-commit]`
- `git vwt drop <id>`
- `git vwt snapshot [-m <msg>]`

## Build

From this directory:

```bash
go test ./...
go build -o git-vwt ./cmd/git-vwt
```

Put `git-vwt` on your PATH to use as `git vwt ...`.

## Safety

- `import/list/show/diff/export` do not touch your working tree.
- Patches touching `.git/**` are rejected by default (`import --allow-dot-git` overrides).

## Skills

See `skills/` for cross-tool Agent Skills that orchestrate `git vwt` workflows.
