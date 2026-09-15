# Changelog

All notable changes to Coding Owl are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[ZeroVer](https://0ver.org/) - `v0.x.y` - until the first stable release.

## [Unreleased]

### Added

- A single `owl` binary that is the daemon, the CLI and the worker in one.
- A desktop app for macOS, installable with Homebrew as a cask.
- Projects, Jobs and Runs: queue a prompt against a repository and Owl carries
  it out in a worktree and branch of its own, one Run at a time, until
  Verification passes.
- Idle detection: work starts after ten minutes without input on AC power, and
  stops the moment the user is back.
- A Handoff document on each Job's branch, so a Run can pick up where the last
  one left off without a shared conversation.
- Accounts with a utilization ceiling, and concurrency caps, so Owl never
  spends the headroom the user needs.
- A standing unattended contract appended to every Run, and a default tool
  allowlist granted once per Account.
- A chat in the desktop app that can look at the repository, with the user's
  say-so.
- Skills declared per Project, fetched and placed for each Run.

### Fixed

- The daemon makes its socket under a private umask and tightens its
  directory on every start.
- The daemon settles configuration, credentials and recovery before it starts
  listening, and leaves no socket behind when it cannot start.
- Garbage collection prunes only Owl's own stale worktrees, never the user's.

[Unreleased]: https://github.com/vojtechmares/coding-owl/commits/main
