# OpenCode + git-vwt

This guide describes the OpenCode integration plan that:

- works with users' existing subagents (no need to duplicate agent definitions)
- enforces "subagents can't apply"
- only enables VWT behavior when explicitly toggled for a session

## What this repo now ships

- `opencode.json`: marks `vwt_apply` as primary-only and asks before apply
- `.opencode/plugins/vwt-mode.ts`: VWT mode plugin (tool routing + safety)
- `.opencode/commands/vwt-on.md`: enable VWT mode for the current session
- `.opencode/commands/vwt-off.md`: disable VWT mode for the current session
- `.opencode/commands/vwt-status.md`: show VWT mode status
- `.opencode/agents/vwt.md`: optional primary agent that turns VWT mode on via agent name

## Quickstart

- Start OpenCode in the repo: `opencode`
- Enable VWT mode for the current session: `/vwt-on` (or launch with `OPENCODE_VWT=1`)
- Any subagents spawned after enabling use isolated workspaces named `opencode-<sessionID>`
- If `git vwt` on your PATH is a different tool, build this repo's binary and use `./git-vwt ...`:
  - `go build -o git-vwt ./cmd/git-vwt`
- Apply from the primary only:
  - tool: `vwt_apply` (primary-only)
  - or shell: `./git-vwt --ws opencode-<sessionID> apply` (or `git vwt --ws ... apply` if installed)

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

Ship a single project plugin (example: `.opencode/plugins/vwt-mode.ts`) that does three jobs:

1. Decide whether a session tree is "VWT enabled" (explicit toggle)
2. Route file tools to either:
   - normal filesystem behavior (default)
   - or `./git-vwt --ws opencode-<sessionID> ...` (VWT enabled)
3. Enforce safety:
   - child sessions can never apply
   - primary session can apply (optionally gated by approval)

The key point: because the plugin affects tool behavior by session ID and propagates to child sessions, all existing subagents automatically inherit the same VWT semantics when invoked from a VWT-enabled primary.

## Explicit toggle options

Pick one (the plugin can support multiple):

- Agent toggle (cleanest per-session UX): create a primary agent like `vwt-build`. When the user sends a message with `agent == vwt-build`, mark that session as VWT enabled.
- Command toggle (no new agents required): define `/vwt-on` and `/vwt-off` and have the plugin listen for `command.executed` events to flip the current session.
- Environment toggle (per run): `OPENCODE_VWT=1` enables VWT mode for every session created in that process.

## Workspace naming

Use one workspace per session:

- `ws = opencode-<sessionID>`

Because subagents run in child sessions, every subagent naturally gets its own workspace without any naming prompts.

## Tool routing strategy

In VWT-enabled sessions, route these tools to VWT operations:

- `read` -> `git vwt --ws <ws> read <path>`
- `write` -> `git vwt --ws <ws> write <path>`
- `edit` -> implement OpenCode-style exact string replacements, but read/write through VWT
- `list` -> `git vwt --ws <ws> ls [path]`
- `grep` -> `git vwt --ws <ws> search <pattern> -- <pathspec...>`
- `glob` -> either `git vwt ls` + filter, or keep using OpenCode's builtin filesystem globbing when acceptable

In non-VWT sessions, keep normal behavior (filesystem). This keeps the plugin safe to ship without changing default workflows.

Note: you do not have to implement every tool on day one. A practical MVP is `read` + `edit` + `write` + `grep` + `list`, and to hard-block `patch`/`multiedit` while VWT mode is on (with an error that tells the model to use `edit`/`write` instead).

## Enforcing "subagents can't apply"

Enforce mechanically, not via prompt text:

- Provide a tool `vwt_apply` (or keep apply as `bash`), but make it primary-only.
  - Preferred: `experimental.primary_tools: ["vwt_apply"]` in `opencode.json`
- In the plugin, block any attempt to apply from child sessions:
  - deny tool calls to `vwt_apply` for sessions with `parentID != null`
  - deny `bash` commands matching `git vwt apply*` for sessions with `parentID != null`

This works even if users have existing subagents with permissive `bash` permissions.

## How the primary learns there is something to apply

Remove the need for subagent prompt conventions:

- On `session.idle` for a child session, the plugin runs:
  - `./git-vwt --ws opencode-<childSessionID> patch`
- If non-empty, the plugin notifies the parent:
  - simplest: `tui.toast.show` with the workspace ID
  - nicer: inject a short message into the parent session (via the SDK) containing the patch and the apply command

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

4. Plugin-driven session-tree toggle + conditional tool routing (chosen)
   - Pros: explicit opt-in; works with existing subagents; can enforce "subagents can't apply"; can auto-notify the primary when patches exist
   - Cons: requires a plugin (some up-front complexity); implementing full parity for every built-in tool is incremental
