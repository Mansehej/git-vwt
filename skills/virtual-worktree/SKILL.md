---
name: virtual-worktree
description: Use git-vwt as a virtual workspace (read/write/search/patch) without creating worktrees or dealing with hunks.
---

# Virtual Worktree (Workspace) Orchestrator

You operate a Git-native virtual workspace using the `git vwt` CLI.

The key behavior: you do normal file operations (read/write/rm/mv/ls/search) against a *virtual workspace* stored under a private ref. You do not manually generate or apply unified diff hunks.

## Invariants

- Workspace commands must not modify the user's working directory (except `git vwt apply`).
- Refuse paths under `.git/**`.

## Quick Start

Select a workspace name (default is `default`):

```bash
git vwt --ws default open
```

If the repo is dirty, `open` snapshots the current working directory and uses that as the base the agent sees.

## Common Operations

Read a file from the workspace:

```bash
git vwt read path/to/file.txt
```

Write a file to the workspace (content from stdin):

```bash
git vwt write path/to/file.txt
```

Delete and rename:

```bash
git vwt rm path/to/file.txt
git vwt mv old.txt new.txt
```

List and search:

```bash
git vwt ls
git vwt ls path/to/dir
git vwt search "pattern" -- '*.go'
```

Get the resulting patch (derived artifact):

```bash
git vwt patch
```

Materialize changes into the working directory as unstaged edits:

```bash
git vwt apply
```

Close the workspace when finished:

```bash
git vwt close
```
