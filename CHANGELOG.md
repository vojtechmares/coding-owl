# Changelog

All notable changes to Coding Owl are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[ZeroVer](https://0ver.org/) - `v0.x.y` - until the first stable release.

## [Unreleased]

### Added

- `owl providers supported` names the model providers Owl can be configured
  with, and how each one's models are decided. It prints what Owl was built
  with rather than what is configured, so it answers before there is anything
  to list and without a daemon running - which `owl providers list`, reporting
  only what is already there, cannot.

## [0.1.0] - 2026-09-17

### Added

- A single `owl` binary that is the daemon, the CLI and the worker in one.
  Homebrew installs it, and `brew services` or `owl daemon install` runs the
  daemon under launchd. It needs git 2.38 or newer and says so at startup.
- A desktop app for macOS, installable with Homebrew as a cask, in the same
  design as codingowl.dev: small text in dense tables, hairline rules, and
  colour that says one of four things - in motion, waiting for you, gone
  wrong, at rest.
- Projects, Jobs and Runs: register a repository, queue a prompt against it
  with `owl add`, and Owl carries it out in a worktree and branch of its own
  until Verification passes. `owl project` and `owl queue` manage both.
- `owl status` answers the morning question - what is running, which Jobs
  want a decision, and what refused the blocked ones.
- Idle detection: work starts after ten minutes without keyboard or mouse
  input on AC power, and stops the moment the user is back.
- A Job is planned in a Run of its own before it is carried out, and that
  plan becomes the Handoff the execution Run is given.
- A Handoff document on each Job's branch, so a Run picks up where the last
  one left off without a shared conversation. It is what the next Run
  orients from, including edits the user makes to it.
- Verification gates every Job on the Project's own checks, and a Project
  can also ask for a review by a fresh Agent given the plan and the diff and
  nothing else. Every check runs, so a blocked Job reports all of what is
  wrong at once.
- Every Run starts by replaying the Job's branch onto current base, so
  Verification judges the work against the code everyone else is on. A
  rebase that conflicts blocks the Job, naming every path in the way.
- `owl jobs accept` keeps a reviewed Job's work and reclaims only its
  worktree; `owl jobs drop` refuses it and takes the branch with it. Neither
  pushes anything anywhere.
- Every Job carries how many Runs it may still take - three by default, one
  spent per Run whatever the Run became - and `owl jobs extend` gives an
  exhausted Job more.
- `owl pause` freezes a Run and every process it started, so the machine is
  the user's again at once, and `owl resume` carries the same Run on. A Run
  left frozen for the grace window ends, and its Job goes back to the place
  it held in the queue.
- Accounts with a utilization ceiling, and concurrency caps, so Owl never
  spends the headroom the user needs. A Job passed over keeps its place, and
  `owl status` says which cap or ceiling held it back.
- A standing unattended contract appended to every Run, and a default tool
  allowlist granted once per Account.
- Garbage collection reclaims the worktrees of finished Jobs, finishes a Job
  whose branch is already merged, and reports - never removes - anything
  that looks unfinished. `owl gc` runs it on demand.
- Skills declared per Project and pinned to a version, fetched and placed
  for each Run. The desktop app lists them and updates them.
- A chat in the desktop app that the daemon answers, and that can look at
  the repository with the user's say-so.
- Every Run is bounded: each phase has a limit on how long its Agent may run
  and on how long it may go without saying anything, so an Agent that wedges
  cannot hold the queue until morning.
- `owl models`, and a Models view in the app, listing what a Job's phases
  can run on. A model is named vendor-first, as `anthropic/claude-opus`: a
  versionless name follows the vendor's latest, one with a version pins it.
- `owl jobs show` prints what each Run was given and what came of it - the
  plan, the handoff, the system prompt that Run was started with, every
  check's verdict, and what each phase ran at and where that came from.
- `owl account exec <name> -- <command>` runs an Account's coding tool
  against that Account's own configuration, which is how an Account gets its
  MCP servers, its plugins and its settings. Owl models none of them.
- Standing instructions on an Account: text every Run on it reads, whichever
  Project the Run is for, kept as the file that Account's tool reads
  instructions from. `owl account instructions show|set|edit`, and the app.

[Unreleased]: https://github.com/vojtechmares/coding-owl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/vojtechmares/coding-owl/releases/tag/v0.1.0
