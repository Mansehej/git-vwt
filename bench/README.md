# Benchmarks

This directory contains scripts to benchmark git-vwt + the OpenCode VWT plugin.

## What it measures

- Speedup from parallel subagents vs serial execution (OpenCode, real model calls)
- Disk usage overhead of N parallel worktrees vs N parallel VWT workspaces

## Run

From the repo root:

```bash
python3 bench/bench.py
```

Options:

```bash
python3 bench/bench.py --help
```
