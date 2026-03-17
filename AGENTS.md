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
git vwt --ws <name> read <path>
git vwt --ws <name> write <path>
git vwt --ws <name> search <pattern> -- <pathspec>...
```

3. Inspect the derived patch:

```bash
git vwt --ws <name> patch
```

4. Apply and then close:

```bash
git vwt --ws <name> apply
git vwt --ws <name> close
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

## Release Process

For a tagged release:

1. Update `CHANGELOG.md` and land any release-prep changes on `main`.
2. Run the repo checks:

```bash
go test ./...
go build -o git-vwt ./cmd/git-vwt
bun test plugins/vwt-mode.test.ts --cwd .opencode
```

3. Create and push an annotated tag:

```bash
git tag -a v0.x.y -m "v0.x.y"
git push origin main
git push origin v0.x.y
```

4. `.github/workflows/release.yml` publishes the GitHub Release artifacts and checksums, then regenerates `Formula/git-vwt.rb` from the published `checksums.txt` and pushes the Homebrew formula bump back to `main`.
5. Verify the release page, the uploaded archives, and that `brew tap Mansehej/git-vwt https://github.com/Mansehej/git-vwt && brew install git-vwt` resolves to the new version.

Notes:

- GitHub Releases are the source of truth for distributed binaries.
- Homebrew installs from `Formula/git-vwt.rb`, which is kept in sync automatically by the release workflow.
- If `main` is branch-protected, GitHub Actions needs permission to push the automated Homebrew formula commit.

## Skills Distribution

This repo ships skill definitions under `skills/`.

- Claude Code: `.claude/skills/<skill>/SKILL.md` (repo) or `~/.claude/skills/<skill>/SKILL.md`
- OpenCode: `.opencode/skills/<skill>/SKILL.md` (repo) or `~/.config/opencode/skills/<skill>/SKILL.md`
- Codex: `.agents/skills/<skill>/SKILL.md` (repo) or `~/.agents/skills/<skill>/SKILL.md`
