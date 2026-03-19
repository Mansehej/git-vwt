# OpenCode + git-vwt

This guide describes the OpenCode integration plan that:

- works with users' existing subagents (no need to duplicate agent definitions)
- enforces "subagents can't apply"
- only enables VWT behavior when explicitly toggled per run via `OPENCODE_VWT=1`

For the full architecture and exact tool-mapping model, see `docs/harnesses/opencode-vwt-architecture.md`.

## What this repo now ships

- `opencode.json`: marks `vwt_patch`, `vwt_apply`, and `vwt_close` as primary-only
- `.opencode/plugins/vwt-mode.ts`: VWT mode plugin (tool routing + safety; no-op unless `OPENCODE_VWT=1`)

## Quickstart

- Install the bundled OpenCode plugin:
  - global OpenCode config: `git vwt opencode install`
  - current project only: `git vwt opencode install --project`
- If `git vwt` on your PATH is a different tool, build this repo's binary and use `./git-vwt ...`:
  - `go build -o git-vwt ./cmd/git-vwt`
- Start OpenCode with VWT mode enabled:
  - `OPENCODE_VWT=1 opencode`
- Primary session edits the working directory normally (no extra apply step).
- Subagent sessions write to isolated workspaces:
  - `opencode-<sessionID>`
- When a child session becomes idle with a non-empty workspace patch, the plugin injects a synthetic orchestration message into the primary session.
- The primary session integrates the child workspace with `vwt_apply`, resolves conflicts if needed, and finishes with `vwt_close`.
- On startup, the plugin sweeps stale `opencode-*` workspace refs for sessions that no longer exist.

## Why OpenCode is a strong fit

OpenCode supports:

- plugins (event hooks + tool middleware + adding/replacing tools)
- custom tools that can replace built-ins by name (`read`, `edit`, `write`, ...)
- subagents as child sessions (parent/child session graph)
- granular permissions

References:

- Tools: https://opencode.ai/docs/tools/
- Permissions: https://opencode.ai/docs/permissions/
- Agents: https://opencode.ai/docs/agents/
- Commands: https://opencode.ai/docs/commands/
- Plugins: https://opencode.ai/docs/plugins/
- Custom tools: https://opencode.ai/docs/custom-tools/

## Chosen approach: plugin-driven "VWT mode" (inherits into existing subagents)

Ship a single project plugin (example: `.opencode/plugins/vwt-mode.ts`) that is enabled only when `OPENCODE_VWT=1` is set.

When enabled, it:

1. Routes file tools to `./git-vwt --ws opencode-<sessionID> ...` for **subagent (child) sessions**
2. Redirects `patch`/`apply_patch` in subagent sessions to edit the VWT workspace (not the working directory)
3. Enforces safety:
    - child sessions can never apply
    - primary session can apply/close child workspaces
4. For primary sessions, checks `git vwt version --check --json` and, when an update is available, instructs the agent to ask the user about updating at the end of a user-facing response

When `OPENCODE_VWT` is not set, the plugin returns no hooks/tools and OpenCode behaves normally.

## Toggle

- `OPENCODE_VWT=1` enables VWT mode for every session created in that process.

## Workspace naming

Use one workspace per session:

- `ws = opencode-<sessionID>`

Because subagents run in child sessions, every subagent naturally gets its own workspace without any naming prompts.

## Tool routing strategy

In VWT-enabled runs (`OPENCODE_VWT=1`), route these tools to VWT operations for child sessions:

- `read` -> `git vwt --ws <ws> read <path>`
- `patch` / `apply_patch` -> parse patch text, then read/write through VWT (key for GPT-* models)
- `write`/`edit` -> route through VWT for non-`apply_patch` models
- `list` -> `git vwt --ws <ws> ls [path]`
- `grep` -> `git vwt --ws <ws> search <pattern> -- <pathspec...>`
- `glob` -> either `git vwt ls` + filter, or keep using OpenCode's builtin filesystem globbing when acceptable

In the primary session, keep normal filesystem behavior. This keeps the UX the same as stock OpenCode while still isolating subagents.

Note: exact parity now treats `patch` and `apply_patch` the same way, so patch-style edits stay source-swapped regardless of which entrypoint the runtime uses.

## Enforcing "subagents can't apply"

Enforce mechanically, not via prompt text:

- Provide tools `vwt_patch`, `vwt_apply`, and `vwt_close`, but make them primary-only.
  - Preferred: `experimental.primary_tools: ["vwt_patch", "vwt_apply", "vwt_close"]` in `opencode.json`
- In the plugin, block any attempt to apply from child sessions:
  - deny tool calls to `vwt_apply` for sessions with `parentID != null`
  - deny `bash` commands matching `git vwt apply*` for sessions with `parentID != null`

This works even if users have existing subagents with permissive `bash` permissions.

## How the primary learns there is something to apply

Remove the need for user-visible handoff:

- On `session.idle` for a child session, the plugin runs:
  - `./git-vwt --ws opencode-<childSessionID> patch`
- If non-empty, the plugin queues the child workspace and sends a synthetic prompt to the parent session with `client.session.promptAsync(...)`.
- The parent session then runs `vwt_apply`, resolves any conflicts in the working tree, and closes the child workspace with `vwt_close`.

## Approaches we considered (and why we ended up here)

1. Always override built-in tools to use VWT
   - Pros: simplest mental model
   - Cons: affects every run; hard to opt in/out; surprises users

2. Separate "VWT agents" with only `vwt_*` tools (Option C)
   - Pros: explicit toggle, minimal risk to normal sessions
   - Cons: does not work with existing subagents unless users duplicate them into `vwt-*` variants

3. Per-run toggle via `OPENCODE_CONFIG_DIR` (Option A)
   - Pros: clean separation; no conditional logic in tools
   - Cons: per-run toggle (not per session); still doesn't automatically inherit into arbitrary existing subagents unless everything is defined in that config dir

4. Plugin-driven env toggle + tool overrides (chosen)
   - Pros: explicit opt-in per run; works with existing subagents; can enforce "subagents can't apply"; can auto-orchestrate child-to-parent integration when patches exist
   - Cons: requires a plugin (some up-front complexity); toggle is per-run (restart to enable/disable); implementing full parity for every built-in tool is incremental
