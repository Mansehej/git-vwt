# Claude Code + git-vwt

This guide describes a practical Claude Code plan aligned with the OpenCode direction:

- explicit opt-in VWT mode per run
- keep existing subagents usable (no required agent duplication)
- enforce "subagents cannot apply"
- primary/orchestrator remains the only apply path

## What Claude gives us (and what it does not)

Claude Code provides strong lifecycle and isolation primitives:

- `--worktree` for isolated sessions
- subagent `isolation: "worktree"`
- `WorktreeCreate` and `WorktreeRemove` hooks

But Claude native file tools still operate on a real filesystem directory. So a hooks-only setup does not automatically move file IO into `refs/vwt/workspaces/<ws>`.

References:

- https://code.claude.com/docs/en/cli-reference
- https://code.claude.com/docs/en/sub-agents
- https://code.claude.com/docs/en/hooks

## Chosen pattern: opt-in VWT mode + MCP tools + hooks

Enable VWT only when an env toggle is set (example: `CLAUDE_VWT=1`). In that mode:

1. Provide VWT MCP tools (`vwt_read`, `vwt_write`, `vwt_edit`, `vwt_list`, `vwt_search`, `vwt_patch`, `vwt_apply`, `vwt_close`).
2. Use `WorktreeCreate`/`WorktreeRemove` hooks to keep Claude's isolation lifecycle and deterministic workspace naming.
3. Enforce apply safety in MCP and policy: child/subagent contexts cannot call `vwt_apply`; primary can.

When `CLAUDE_VWT` is not set, keep normal Claude behavior.

## Workspace naming and lifecycle

Use one workspace per session-like unit:

- primary: `claude-<sessionId>`
- subagent: `claude-<parentSessionId>-<subagentName>-<nonce>` (or hook-provided worktree name if already unique)

Lifecycle rules:

- on first tool use (or `WorktreeCreate`), run `git vwt --ws <ws> open`
- edits/read/search operate through VWT tools
- primary reviews via `vwt_patch`
- primary applies via `vwt_apply`
- close with `vwt_close` during teardown (or periodic cleanup)

This mirrors repo invariants: only apply mutates the working directory.

## Safety model (hard guardrails)

Enforce server-side and harness-side, not by prompt text:

- reject apply when caller is a subagent/child context
- require explicit confirmation argument for `vwt_apply` (for example `confirm=true`)
- optionally require process env flag for apply (for example `VWT_APPLY_ENABLED=1`)
- reject unsafe paths (`.git/**`, absolute paths, traversal)

If shell access is available, add policy/hook checks that block child attempts to run `git vwt apply` directly.

## Existing subagents compatibility

Use existing subagent definitions where possible:

- keep current subagent configs; do not require `vwt-*` duplicates
- make VWT MCP tools available globally in VWT mode
- bias tool policy/instructions toward VWT tools for file operations

Important: if a subagent can still use unrestricted native filesystem editing tools, full VWT-only IO is not guaranteed. Treat that as policy-hardening work, not a solved property.

## Hook and policy guidance

Use hooks for lifecycle wiring, not as the only safety mechanism:

- `WorktreeCreate`: derive workspace name, `git vwt --ws <ws> open`, create/return a directory path Claude expects
- `WorktreeRemove`: optional cleanup directory and `git vwt --ws <ws> close`
- policy/rules: deny `vwt_apply` for child contexts; deny `git vwt apply` shell calls from child contexts

Minimal `WorktreeCreate` shape (illustrative):

```json
{
  "hooks": {
    "WorktreeCreate": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash -lc 'set -euo pipefail; NAME=$(jq -r .name); WS=\"claude-$NAME\"; DIR=\"$(pwd)/.claude/worktrees/$NAME\"; mkdir -p \"$DIR\"; git vwt --ws \"$WS\" open >/dev/null; printf \"%s\\n\" \"$DIR\"'"
          }
        ]
      }
    ]
  }
}
```

## Recommended end state

- opt-in mode via `CLAUDE_VWT=1`
- MCP provides VWT file semantics across primary and subagents
- hooks preserve Claude subagent lifecycle ergonomics
- primary/orchestrator is the only allowed apply actor
