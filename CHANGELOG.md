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
- Every Run is bounded: each phase has a limit on how long its Agent may run
  and on how long it may go without saying anything, so an Agent that wedges
  on an untouched machine cannot hold the queue until morning. Both default to
  something sensible, are set per phase, and `owl jobs show` prints what is in
  force and where it came from.
- `owl models`, and a Models view in the desktop app, listing what a Job's
  phases can run on.
- `owl account exec <name> -- <command>`, which runs an Account's coding tool
  against that Account's own configuration. An Account's configuration
  directory is the tool's configuration directory, so the tool's own commands
  are how an Account gets its MCP servers, its plugins and its settings. Owl
  models none of them.
- Standing instructions on an Account: text every Run on it reads, whichever
  Project the Run is for. Kept as the file that Account's tool reads
  instructions from, so the tool reads them itself and Owl injects nothing.
  Editable with `owl account instructions show|set|edit` and in the desktop
  app.

### Changed

- A model is named vendor-first, as `anthropic/claude-opus`. A versionless
  name follows the vendor's latest; one with a version pins it. A model naming
  no vendor, or a vendor the Driver does not serve, is refused before the Job
  gets a worktree.
- A Job's default TTL is 3 rather than 10, so a Job that goes wrong is reported
  on the third night rather than the tenth. `--ttl` at enqueue and
  `owl jobs extend` are unchanged.
- The desktop app is set in the same design as codingowl.dev - a light ground,
  hairline rules and Geist - instead of the liquid glass it shipped with. A
  dashboard is small text in dense tables, and the glass was in the way of
  reading it. Colour now says one of four things and nothing else: blue is in
  motion, near-black is waiting for you, rose is something that went wrong,
  grey is at rest.

### Fixed

- The daemon makes its socket under a private umask and tightens its
  directory on every start.
- The daemon settles configuration, credentials and recovery before it starts
  listening, and leaves no socket behind when it cannot start.
- Garbage collection prunes only Owl's own stale worktrees, never the user's.
- A worktree holding work git does not know about is reported rather than
  reclaimed even in a repository that sets `status.showUntrackedFiles=no`,
  which used to make such a worktree look clean and get it deleted silently.
- A long Jobs list no longer drags the desktop window sideways, sidebar and
  all: a table wider than its panel scrolls inside it.

[Unreleased]: https://github.com/vojtechmares/coding-owl/commits/main
