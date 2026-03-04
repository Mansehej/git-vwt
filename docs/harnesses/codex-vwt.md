# Codex + git-vwt

This guide describes a Codex plan aligned with the OpenCode direction:

- explicit opt-in VWT mode per run
- existing subagents can keep their roles (avoid duplicating role definitions)
- subagents cannot apply to the working directory
- only the primary/orchestrator can apply

## Recommended pattern: opt-in VWT profile + MCP tools

Use a dedicated Codex config profile for VWT runs (for example `config-vwt.toml`).
Start Codex with that profile only when you want VWT behavior.

Why this is the best practical fit today:

- Codex has no OpenCode-style plugin hook that can transparently reroute all built-in file tools
- MCP gives a reliable tool boundary (`vwt_read`, `vwt_write`, `vwt_patch`, `vwt_apply`)
- role-specific configs let you keep apply authority in the primary only

## Core workflow (parent-child)

1. Primary assigns each subagent a workspace name and task.
2. Subagent edits only through VWT tools in that workspace.
3. Subagent returns `vwt_patch` output (or patch summary + workspace name).
4. Primary reviews and calls `vwt_apply`.

This preserves the project invariant: only apply mutates the real checkout.

## Workspace naming

Codex currently does not expose a guaranteed shell-usable child session ID in all environments.
Use explicit names assigned by the parent:

- `codex-<run>-<role>-<n>`
- examples: `codex-482-auth-1`, `codex-482-tests-1`

Parent prompt contract for each worker:

- include the exact workspace name
- require worker to call tools with that name
- require worker to return patch status before finishing

## Guardrails (strict safety)

Enforce with configuration, not only prompt text:

- worker profile: no `vwt_apply` tool
- worker profile: deny shell `git vwt apply` via rules
- primary profile: allow `vwt_apply` (optionally with approval prompt)

If a worker still has shell access, keep `sandbox_mode = "read-only"` to reduce accidental checkout edits.

## Practical config skeleton

Use one shared worker config file, and point existing worker roles to it so you do not duplicate policy.

`.codex/config-vwt.toml`:

```toml
[features]
multi_agent = true

[agents]
max_threads = 6
max_depth = 1

[agents.default]
config_file = "agents/vwt-worker.toml"

[agents.orchestrator]
config_file = "agents/vwt-primary.toml"
```

`.codex/agents/vwt-worker.toml`:

```toml
sandbox_mode = "read-only"
developer_instructions = "Use virtual-worktree workflow. Never apply. Return workspace + patch."

# Expose only non-apply MCP VWT tools to workers.
# (Tool wiring is environment-specific; keep apply out of this profile.)
```

`.codex/agents/vwt-primary.toml`:

```toml
developer_instructions = "Own workspace assignment, patch review, and apply decisions."

# Expose vwt_apply only here.
```

`~/.codex/rules/vwt-worker.rules`:

```python
prefix_rule(
    pattern=["git", "vwt", "apply"],
    decision="forbid",
    justification="Workers must not apply to checkout"
)
```

`~/.codex/rules/vwt-primary.rules`:

```python
prefix_rule(
    pattern=["git", "vwt", "apply"],
    decision="prompt",
    justification="Primary controls apply"
)
```

## MCP tool surface

Use the cross-harness tool shape from `docs/harnesses/cross-harness-mcp.md`:

- workers: `vwt_read`, `vwt_write`, `vwt_edit`, `vwt_list`, `vwt_search`, `vwt_patch`, `vwt_close`
- primary only: `vwt_apply`

Server-side requirements remain mandatory:

- reject unsafe paths (`.git/**`, absolute, traversal)
- validate workspace names
- serialize per-workspace writes
- require explicit apply confirmation

## Fallback (no MCP yet)

You can run shell-only (`git vwt --ws ...`) with the same workspace contract and rules.
This is workable, but less robust than MCP because file operation boundaries are weaker.
