# Claude Code + git-vwt

This guide explains how to emulate `claude --worktree` isolation while storing edits in `refs/vwt/workspaces/<name>`.

## What Claude provides

Claude Code already has the right lifecycle hooks and isolation controls:

- `--worktree` for isolated sessions
- subagent `isolation: "worktree"`
- `WorktreeCreate` and `WorktreeRemove` hooks

References:

- https://code.claude.com/docs/en/cli-reference
- https://code.claude.com/docs/en/sub-agents
- https://code.claude.com/docs/en/hooks

## Integration pattern

Use hooks to replace default `git worktree` behavior with a VWT-aware setup:

1. `WorktreeCreate`
   - read `name` from hook JSON stdin
   - create/open VWT workspace: `git vwt --ws <name> open`
   - create an isolated directory for the session (Claude needs a real directory path)
   - print that absolute path to stdout
2. `WorktreeRemove`
   - optional cleanup of temp directory
   - optional `git vwt --ws <name> close`

## Important practical note

Claude's native file tools operate on the filesystem directory returned by `WorktreeCreate`.
If you want edits to live only in VWT refs, pair this with either:

- a VWT MCP toolset (`vwt_read`, `vwt_write`, `vwt_patch`, ...), and steer agent/tool policy toward those tools
- or a wrapper workflow where filesystem edits are synced to VWT explicitly

Without that, you only replaced worktree creation, not file IO semantics.

## Example hook skeleton

```json
{
  "hooks": {
    "WorktreeCreate": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash -lc 'set -euo pipefail; NAME=$(jq -r .name); DIR=\"$(pwd)/.claude/worktrees/$NAME\"; mkdir -p \"$DIR\"; git vwt --ws \"$NAME\" open >/dev/null; printf \"%s\\n\" \"$DIR\"'"
          }
        ]
      }
    ],
    "WorktreeRemove": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash -lc 'set -euo pipefail; PATH_TO_REMOVE=$(jq -r .worktree_path); rm -rf -- \"$PATH_TO_REMOVE\"'"
          }
        ]
      }
    ]
  }
}
```

## Recommended end state

For best fidelity and portability:

- keep Claude worktree lifecycle hooks for subagent isolation UX
- expose VWT as MCP filesystem-like tools
- reserve `git vwt apply` as the only action that mutates the user checkout
