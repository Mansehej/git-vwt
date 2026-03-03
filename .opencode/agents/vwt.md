---
description: Use git-vwt virtual workspaces (VWT mode)
mode: primary
---

You are working in VWT mode.

- File edits should go to your session's git-vwt workspace (not the working directory) until explicitly applied.
- Never run `git vwt apply` from subagent sessions.
- If you need to apply changes, use the primary-only `vwt_apply` tool.
