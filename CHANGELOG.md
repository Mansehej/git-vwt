# Changelog

All notable changes to this project will be documented in this file.

The format is based on Keep a Changelog, and this project follows Semantic Versioning for tagged releases.

## [Unreleased]

- No unreleased changes yet.

## [0.1.0] - 2026-03-17

### Added

- Initial `git vwt` CLI with isolated virtual workspace operations: `open`, `info`, `read`, `write`, `rm`, `mv`, `ls`, `search`, `patch`, `apply`, and `close`.
- Dirty-worktree snapshotting so new workspaces can use the current checkout state as base context without mutating the working tree.
- Conflict-aware `apply` behavior, including a three-way fallback mode and JSON status output for automation.
- OpenCode integration plugin and cross-tool integration docs for OpenCode, Claude Code, and Codex.
- CI coverage across Linux, macOS, and Windows.
- Benchmark scripts for comparing serial, worktree, and virtual-workspace agent workflows.

### Changed

- Added end-to-end CLI coverage for `info`, `rm`, `mv`, `ls`, and `search`.
- Added release automation for tagged `v*` builds with packaged binaries and checksums.

### Fixed

- Fixed workspace-only remove and move operations so they do not depend on the state of the checked-out working tree.
