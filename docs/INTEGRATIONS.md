# Integrations

This repository ships skill definitions under `skills/`.

`git vwt` works when the `git-vwt` binary is on your `PATH` (Git discovers it as a `git` subcommand).

Install by copying a skill directory to one of these locations:

- Claude Code: `.claude/skills/<skill>/SKILL.md` (repo) or `~/.claude/skills/<skill>/SKILL.md`
- OpenCode: `~/.config/opencode/skills/<skill>/SKILL.md` (global) and/or `.opencode/skills/<skill>/SKILL.md` (repo)
- Codex: `.agents/skills/<skill>/SKILL.md` (repo) or `~/.agents/skills/<skill>/SKILL.md`

OpenCode also scans `.claude/skills` and `.agents/skills`.
