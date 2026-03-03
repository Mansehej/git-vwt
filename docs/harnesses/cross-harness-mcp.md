# Cross-Harness MCP Plan

This file defines a single MCP integration strategy that works across Claude Code, OpenCode, and Codex.

## Goal

Expose `git-vwt` as filesystem-like tools so agents can behave like they are editing files, while actual state lives in:

- `refs/vwt/workspaces/<workspace>`

And only `vwt_apply` mutates the real working directory.

## Proposed MCP tool surface

- `vwt_read(workspace?, path)`
- `vwt_write(workspace?, path, content)`
- `vwt_edit(workspace?, path, edits|newContent)`
- `vwt_list(workspace?, path?)`
- `vwt_search(workspace?, pattern, pathspecs?)`
- `vwt_patch(workspace?)`
- `vwt_apply(workspace?, confirm)`
- `vwt_close(workspace?)`

Internal helper behavior:

- auto-open workspace on first call: `git vwt --ws <ws> open`

## Workspace scoping model

Use both:

- connection default workspace (auto-assigned)
- per-call `workspace` override

This supports:

- single-agent sessions with minimal params
- parent agents assigning explicit workspace names to subagents

## Security and safety requirements

Enforce server-side, not prompt-side:

- reject absolute paths
- reject path traversal (`..`)
- reject `.git/**`
- validate workspace names with conservative charset
- serialize operations per workspace to avoid races
- gate `vwt_apply` behind explicit `confirm=true` and environment flag

## Suggested repo implementation steps

1. Add MCP entrypoint:
   - `cmd/git-vwt-mcp/main.go`
2. Add validation helpers:
   - path checks
   - workspace name checks
3. Implement tool handlers by shelling out to `git vwt --ws ...`
4. Add docs and config snippets for each harness
5. Optional: add `git vwt exec --ws <ws> -- <cmd...>` for isolated test runs without touching the checkout

## Harness mapping summary

- Claude Code: use hooks for lifecycle, MCP tools for VWT file semantics
- OpenCode: either custom-tool overrides or MCP; both support subagent isolation well
- Codex: skills + multi-agent roles + rules now, MCP for best long-term ergonomics
