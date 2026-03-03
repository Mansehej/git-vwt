# Codex + git-vwt

This guide explains how to get subagent-style isolation in Codex using virtual workspaces.

## What Codex gives you today

Codex supports:

- skills from `.agents/skills/*/SKILL.md`
- experimental multi-agent roles
- sandbox policies and approval policies
- execution rules (`prefix_rule`) for command allow/prompt/forbid
- MCP servers (stdio and streamable HTTP)

References:

- Skills: https://developers.openai.com/codex/skills
- Multi-agent: https://developers.openai.com/codex/multi-agent
- Rules: https://developers.openai.com/codex/rules
- MCP: https://developers.openai.com/codex/mcp
- Config reference: https://developers.openai.com/codex/config-reference

## Recommended architecture (no Codex core changes)

1. Skill-enforce VWT workflow

- add `.agents/skills/virtual-worktree/SKILL.md`
- require: do all edits with `git vwt --ws <name> ...`
- require: return `git vwt --ws <name> patch`
- require: do not run `git vwt apply` unless explicitly asked

2. Multi-agent role guardrails

- set subagent roles to `sandbox_mode = "read-only"`
- put VWT requirements in `developer_instructions`

3. Allow only the VWT escape hatch

- add a rules file allowing `git vwt` outside sandbox escalation prompts

## Example skill

`./.agents/skills/virtual-worktree/SKILL.md`

```markdown
---
name: virtual-worktree
description: Use git-vwt virtual workspaces for isolated edits and patch output.
---

Workflow:
- Never edit files directly in the checkout.
- Create/open a workspace: `git vwt --ws <WS> open`.
- Read/write/search only via `git vwt --ws <WS> ...`.
- Return `git vwt --ws <WS> patch` when done.
- Do not run `git vwt apply` unless explicitly requested.
```

## Example role config

`.codex/config.toml`

```toml
[features]
multi_agent = true

[agents]
max_threads = 6
max_depth = 1

[agents.worker]
description = "Implementation agent that must use git-vwt workflow"
config_file = "agents/worker.toml"
```

`.codex/agents/worker.toml`

```toml
sandbox_mode = "read-only"
developer_instructions = "Use the virtual-worktree skill and do all edits via git vwt."
```

## Example rules allowlist

`~/.codex/rules/default.rules`

```python
prefix_rule(
    pattern = ["git", "vwt"],
    decision = "allow",
    justification = "Allow virtual workspace operations under git-vwt"
)
```

## Workspace naming in multi-agent runs

Codex does not currently expose a stable per-subagent ID directly inside shell commands.
In practice, have the parent assign names explicitly:

- `vwt-auth-fix`
- `vwt-test-repair`
- `vwt-docs-pass`

Then each subagent uses its assigned `--ws` value.

## Stronger option: MCP server

For better reliability, expose `git-vwt` as an MCP toolset and route edits through MCP tools instead of generic shell.
