# Virtual Worktree (Diff-Only) Project Plan

## Product Positioning

- Build a Git-native "patch inbox" that stores each agent output as a commit under private refs, without per-agent worktrees.
- Ship as a standalone `git` subcommand for universal usage (`git vwt ...`).
- Provide Agent Skills as thin integrations for Claude Code, OpenCode, and Codex.

## 1) Requirements and Invariants

- Diff-only producers: subagents never write files; they return unified diffs.
- Orchestrator flow: import diff -> inspect -> (optional) test -> apply.
- Safety invariants:
  - `import/show/diff/list/export` never touch the user's working tree.
  - Default deny patches touching `.git/**` (override flag optional).
  - No git config edits, no destructive git commands.

## 2) Data Model

- Store each patch as commit `C` with parent base commit `B`.
- Keep commits alive under private refs (avoid GC pruning):
  - `refs/vwt/patches/<id>`
  - Optional snapshots: `refs/vwt/snapshots/<id>`
- Metadata strategy:
  - MVP: commit subject/body holds agent/title info.
  - Later: richer metadata via `git notes`.

## 3) CLI Surface (MVP)

- `git vwt import --base <rev> [--id <id>] [--agent <name>] [--title <title>] [--stdin|<file>]`
  - Use temporary `GIT_INDEX_FILE` + `read-tree` + patch apply + `write-tree` + `commit-tree` + `update-ref`.
- `git vwt compose --base <rev> [--id <id>] [--agent <name>] [--title <title>] <patch-id>...`
  - Compose multiple patch commits into a single synthetic commit (shadow view) without touching the working tree.
- `git vwt list`
  - Read refs and print id, subject, date, and base.
- `git vwt show <id>`
- `git vwt diff <id>`
- `git vwt export <id>`
  - Portable patch output (`git format-patch --stdout <base>..<patch>`).
- `git vwt cat <path>`
- `git vwt cat <id|rev> <path>`
  - Print a file as it exists in HEAD (default) or in a patch/snapshot/rev (useful for agent context without applying).
- `git vwt apply <id>`
  - Apply the patch commit diff to the working tree as unstaged changes (do not move `HEAD` and do not update the index).
- `git vwt drop <id>`
  - Delete `refs/vwt/patches/<id>`.
- `git vwt gc`
  - Optional prune/retention policy.

## 4) Base Handling (Dirty Repo Support)

- `git vwt snapshot [-m <msg>]`
  - Capture current workspace into a synthetic commit using a temporary index.
  - Include untracked files by default, exclude ignored files.
- Import policy:
  - Prefer explicit `--base` unless the patch artifact embeds base metadata.
  - Avoid implicit base selection when repository is dirty.

## 5) Optional Single Runner

- One reusable runner checkout/worktree for testing patches without polluting main checkout.
- `git vwt runner init [--path <dir>]`
- `git vwt runner exec <id> -- <cmd...>`
  - Checkout patch commit in runner, execute command, return output/exit code.
- Keep runner optional; core value remains diff storage and orchestration.

## 6) Agent Skills Packaging (Cross-Tool)

- Ship `skills/virtual-worktree/` with `SKILL.md` + optional scripts/references.
- Installation paths:
  - Claude Code: `.claude/skills/...` or `~/.claude/skills/...`
  - OpenCode: `.opencode/skills/...` (also scans `.claude/skills` and `.agents/skills`)
  - Codex: `.agents/skills/...` or `~/.agents/skills/...`
- Skill behavior:
  - Manual orchestrator skill (recommended) that runs `git vwt` commands.
  - Optional "diff-only subagent" skill with explicit output contract: exactly one fenced `diff` block.
- Compatibility note:
  - Keep frontmatter minimal and portable (`name`, `description`), with tool-specific extras only when needed.

## 7) Claude Code Plugin Packaging (Optional but Polished)

- Add plugin manifest + namespaced skills for easy distribution:
  - `.claude-plugin/plugin.json`
  - `skills/...`
- Gives users installable commands such as `/vwt:propose-patch`.

## 8) OpenCode and Codex Integration Docs

- OpenCode:
  - Document skill paths, permissions, and tool restrictions for diff-only workflows.
- Codex:
  - Document `.agents/skills` placement and patch-only output template.
  - Optionally use `agents/openai.yaml` for invocation/dependency policy.

## 9) Testing and CI

- Integration tests in temporary repos to verify:
  - Import does not alter working tree status.
  - Patch commit diff is correct.
  - `apply` produces expected tree.
  - Snapshots capture dirty state deterministically.
  - Path safety blocks `.git/**` edits by default.
- Cross-platform CI: macOS, Linux, Windows.
- Keep runtime dependencies minimal.

## 10) Release and Distribution

- Publish signed GitHub Releases with multi-platform binaries.
- Provide package manager installs (e.g., Homebrew + Scoop).
- Use semantic versioning, stable flags, and clear changelog.

## Post-v1 Roadmap

- MCP server exposing `vwt_import/list/show/apply/runner_exec` tools.
- Patch stacks (ordered apply of multiple patch IDs).
- Tagging, conflict preflight checks, and richer metadata querying.

## Current Ecosystem Context (as of Feb 2026)

- Claude Code: supports skills/plugins and now includes worktree-related hook events + agent isolation options.
- OpenCode: supports Agent Skills and scans `.opencode/skills`, `.claude/skills`, and `.agents/skills` locations.
- Codex: supports Agent Skills and `.agents/skills` distribution model.

This makes a standalone `git vwt` CLI plus thin Skill wrappers the most portable and professional delivery strategy.
