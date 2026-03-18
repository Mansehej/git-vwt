# OpenCode VWT Architecture

This document defines the target architecture for making `git-vwt` feel invisible to OpenCode agents.

The core idea is simple:

- agents keep using normal OpenCode tools
- the plugin swaps the backing store from the checked-out filesystem to a `git-vwt` workspace when isolation is needed
- orchestration tools like `vwt_apply` and `vwt_close` stay outside the normal file-editing surface

If this design is followed, the agent should not need to learn a new editing model. It should continue to think in terms of "read a file", "edit a file", "search files", and "apply a patch". Only the source of truth changes.

## Goals

- Preserve the default OpenCode tool contract for agents.
- Isolate subagent edits without creating Git worktrees.
- Keep primary-session UX close to stock OpenCode.
- Prevent child sessions from applying changes to the working directory.
- Make the integration understandable as filesystem virtualization, not as a separate tool ecosystem.

## Non-goals

- Replacing every OpenCode tool with a VWT-specific variant.
- Teaching agents to reason about workspaces as a first-class editing primitive.
- Forcing web, question, todo, or LSP tools through `git-vwt`.
- Replacing package-manager or shell behavior with synthetic abstractions.

## Mental Model

OpenCode normally reads and writes the checked-out working tree.

With VWT mode enabled, the plugin should behave like a filesystem shim:

- same tool name
- same arguments
- same result shape
- same permissions
- different backing store

That means:

- primary sessions can still use the real working tree by default
- child sessions read and write a virtual tree stored in `refs/vwt/workspaces/<name>`
- when the primary decides to integrate a child session, it uses `vwt_apply`

From the model's perspective, this is still just file IO.

## Source Of Truth By Session Type

### Primary session, default mode

- Source of truth: checked-out working directory
- File tools: stock OpenCode behavior
- Integration tools: `vwt_patch`, `vwt_apply`, `vwt_close`

### Child session, VWT mode enabled

- Source of truth: `git-vwt` workspace tree
- File tools: same tool names and semantics, routed through VWT
- Working directory writes: blocked indirectly by not exposing apply and by overriding file tools

### Primary session, isolated mode (`OPENCODE_VWT_PRIMARY=1`)

- Source of truth: VWT workspace tree for the primary too
- This is useful for highly parallel or fully virtualized runs
- The same "source swap" principle still applies

## Default OpenCode Built-in Tools

OpenCode's built-ins are documented at `https://opencode.ai/docs/tools/`.

Current built-ins:

- `bash`
- `edit`
- `write`
- `read`
- `grep`
- `glob`
- `list`
- `lsp` (experimental)
- `patch`
- `skill`
- `todowrite`
- `todoread`
- `webfetch`
- `websearch`
- `question`

Only a subset of these need VWT virtualization.

## Tool Virtualization Boundary

### Tools that should become source-swapped

These are the tools the agent uses for normal file work and should feel identical whether they hit disk or a virtual workspace:

- `read`
- `write`
- `edit`
- `patch`
- `apply_patch` if the model/runtime uses that name in practice
- `grep`
- `glob`
- `list`

### Tools that should stay stock OpenCode

These do not need VWT-aware backing storage:

- `question`
- `todowrite`
- `todoread`
- `webfetch`
- `websearch`
- `skill`
- `lsp`

### Tools that remain orchestration-specific

These are not part of the normal file editing contract and should stay VWT-specific:

- `vwt_patch`
- `vwt_apply`
- `vwt_close`

These tools exist for parent/child integration, not for ordinary editing.

## Exact 1:1 Mapping Contract

For a virtualized tool to count as exact parity, it should preserve all of the following:

- tool name
- argument names
- argument meaning
- path semantics
- output format
- common success strings
- common failure cases
- permission category

The plugin is allowed to change only one thing:

- where the bytes come from or go to

In other words, the agent should not need a special prompt like "use the VWT version of read".

## Mapping Table

### `read`

- Agent contract: read a file or directory view; support line-windowed reads
- VWT backend: `git vwt read` or `git vwt ls` fallback for directories
- Required parity:
  - same `filePath`, `offset`, `limit` behavior
  - same line numbering behavior expected by OpenCode
  - same directory-read conventions if directories are supported

### `write`

- Agent contract: write full file contents
- VWT backend: `git vwt write`
- Required parity:
  - same overwrite semantics
  - same path behavior
  - same success/failure contract

### `edit`

- Agent contract: exact string replacement in an existing file
- VWT backend: read from VWT, transform in memory, write back through `git vwt write`
- Required parity:
  - same `oldString` / `newString` / `replaceAll` semantics
  - same ambiguity and not-found failures

### `patch` / `apply_patch`

- Agent contract: structured partial edits using patch text
- VWT backend: parse patch, apply against VWT-backed file contents, then write back through `git vwt write` and `git vwt rm`
- Required parity:
  - same patch envelope expectations
  - same path validation rules
  - same success output shape

This is the most important parity point after `read`/`write`, because many GPT-family models naturally prefer patch-style editing.

### `grep`

- Agent contract: regex search over project contents
- VWT backend: `git vwt search`
- Required parity:
  - same pattern input expectations
  - same path / include filtering where possible
  - same result readability for the model

### `list`

- Agent contract: list directory contents
- VWT backend: `git vwt ls`
- Required parity:
  - same path behavior
  - same sorting expectations
  - same directory marker conventions

### `glob`

- Agent contract: glob-style file discovery
- VWT backend: virtual tree traversal plus glob filtering
- Required parity:
  - same pattern meaning as normal OpenCode globbing
  - same base-path handling
  - same file-only vs directory behavior expected by the tool

This is usually the hardest one to make exact unless `git-vwt` eventually grows a native glob primitive.

## What Changes Internally

The plugin should do the following for VWT-backed sessions:

1. Resolve the requested path relative to the OpenCode session directory/worktree.
2. Enforce safety rules, especially `.git/**` denial and path confinement.
3. Decide whether the current session should use the working tree or a VWT workspace.
4. Route the operation to either:
   - stock filesystem IO, or
   - the `git vwt` workspace tree
5. Return results in the same shape the agent would normally expect.

This preserves the agent-facing contract while changing the underlying storage layer.

## Session And Workspace Model

### Workspace naming

- One workspace per session
- Current convention: `opencode-<sessionID>`

Why this works:

- no naming prompt for agents
- stable routing for child sessions
- easy cleanup of stale refs

### Parent and child behavior

- child session: writes into its own workspace
- primary session: either stays on the working tree or gets its own workspace if explicitly isolated
- child sessions never apply directly to the checkout

## Orchestration Model

The file tools should stay boring. The orchestration is where VWT becomes visible.

### Child idle flow

1. Child session becomes idle.
2. Plugin checks the child workspace patch.
3. If the patch is empty, the workspace can be closed.
4. If the patch is non-empty, the plugin sends a synthetic prompt to the parent.
5. The parent runs:
   - `vwt_apply`
   - conflict resolution if needed
   - `vwt_close`

This keeps the handoff out of the user's manual workflow and out of the child's permissions.

## Why `vwt_apply` Is Not Part Of The 1:1 Surface

Agents doing ordinary editing should not need to know about apply/close.

`vwt_apply` is different because it does not mean "edit a file". It means:

- take a virtual tree diff
- merge it into the checked-out working tree
- surface conflicts if necessary

That is an integration action, not a file editing action. Keeping it separate is correct.

## Permissions Model

The plugin should preserve OpenCode's permission categories even when the backend is virtualized.

Examples:

- `read` stays a `read` permission ask
- `write`, `edit`, `patch`, `apply_patch` stay under edit-like permission asks
- `list`, `glob`, `grep` keep their corresponding permission types

This matters because users reason about permissions by tool meaning, not by backend implementation.

## Path Semantics

To feel exact to the model, paths should behave the same in stock and VWT-backed modes:

- same relative path base
- same absolute-path handling rules
- same refusal of unsafe paths
- same normalization of `./foo`, `foo`, and nested paths

The plugin should remain the place where OpenCode path semantics are preserved and translated into safe `git-vwt` paths.

## Install Modes

The installer supports two placement models.

### Global install

Command:

```bash
git vwt opencode install
```

Destination:

- `$OPENCODE_CONFIG_DIR` if set
- otherwise `~/.config/opencode`

Files written:

- `opencode.json`
- `package.json`
- `bun.lock`
- `plugins/vwt-mode.ts`

Use this when you want VWT mode available across many repos.

### Project install

Command:

```bash
git vwt opencode install --project
```

Destination:

- current repository

Files written:

- `opencode.json`
- `.opencode/.gitignore`
- `.opencode/package.json`
- `.opencode/bun.lock`
- `.opencode/plugins/vwt-mode.ts`

Use this when you want repo-local configuration.

## Runtime Toggle Model

The plugin should stay opt-in at runtime.

- `OPENCODE_VWT=1` enables VWT mode
- `OPENCODE_VWT_PRIMARY=1` isolates primary sessions too
- `OPENCODE_CONFIG_DIR` can redirect the global config location

This keeps installation separate from activation.

## Current Coverage vs Target Coverage

### Current strong coverage

- `read`
- `write`
- `edit`
- `grep`
- `list`
- `glob`
- `apply_patch`
- orchestration via `vwt_patch`, `vwt_apply`, `vwt_close`

### Main parity gap to watch

- OpenCode documents a built-in `patch` tool, while the plugin currently emphasizes `apply_patch`

If exact parity is the goal, `patch` should be handled with the same care as `apply_patch`, or the runtime behavior should be verified so the plugin is intercepting the actual patch-edit path the models use.

### Secondary parity risks

- `glob` semantics may drift from stock OpenCode if implemented only as tree listing plus filtering
- `bash` remains an escape hatch for agents with broad shell permissions
- output strings should stay as close as practical to OpenCode expectations so the model does not infer a different tool contract

## Implementation Checklist For Exactness

Use this checklist when evaluating whether a tool is truly virtualized rather than merely "similar".

### Tool contract

- same tool name
- same argument schema
- same help text intent
- same result structure

### Behavior

- same path resolution
- same newline handling
- same line numbering format where applicable
- same not-found behavior
- same overwrite behavior
- same replacement ambiguity behavior

### Safety

- no writes under `.git/**`
- no absolute-path escapes outside the worktree/config root
- child sessions cannot apply to the working tree

### Orchestration

- child idle prompts are synthetic and automatic
- primary-only tools remain primary-only
- empty child patches are cleaned up automatically

## What Agents Should Be Told

Ideally, very little.

The best prompt-level instruction is not "use these special VWT tools".
It is closer to:

- use normal file tools as usual
- if a child session becomes idle with integration work, the primary will be prompted to apply it
- do not try to apply from subagents

Everything else should be enforced by the plugin and tool layer.

## Why This Architecture Is Better Than A Separate Tool Ecosystem

If the agent has to learn `vwt_read`, `vwt_write`, `vwt_edit`, and so on, several things get worse:

- prompts become more complicated
- tool selection becomes less reliable
- default OpenCode agent behavior stops matching user expectations
- every built-in capability needs a duplicate mental model

The source-swap architecture avoids that. It preserves OpenCode's native ergonomics while changing only the storage layer.

## End-To-End Example

### Repo-local installation

```bash
git vwt opencode install --project
OPENCODE_VWT=1 opencode
```

### What happens next

1. Primary session starts normally.
2. Child session uses `read`, `edit`, `write`, `patch`, `grep`, `list`, and `glob` as if nothing changed.
3. The plugin routes those file operations into `opencode-<sessionID>`.
4. The child finishes.
5. The plugin computes the child patch and prompts the parent.
6. The parent runs `vwt_apply` and resolves conflicts if required.
7. The parent runs `vwt_close`.
8. The working directory now contains the integrated child changes.

At no point did the child need to understand Git refs, patches as transport objects, or VWT internals.

## Recommended Next Steps

If the goal is exact tool behavior for agents, prioritize the following in order:

1. Verify whether OpenCode models are using `patch` or `apply_patch` in practice and normalize support accordingly.
2. Tighten `glob` parity so it matches stock OpenCode behavior as closely as possible.
3. Audit success/error strings for `read`, `write`, `edit`, and patch operations so the model sees familiar tool responses.
4. Keep VWT-specific tools limited to orchestration only.

## Summary

The right architecture is:

- normal OpenCode tools for the agent
- `git-vwt` as a backing-store swap for file operations
- VWT-specific tools only for parent-child integration

If this boundary is kept strict, agents can behave exactly as they do today while gaining isolated workspace storage under the hood.
