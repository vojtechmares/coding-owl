# Architecture decision records

One file per decision, named `NNNN-kebab-case-title.md`, numbered in the order
decisions were accepted. Numbers are never reused and files are never deleted -
a decision that stops being true gets its status changed to `Superseded by
ADR-XXXX` and stays where it is.

## Format

```markdown
# ADR-NNNN: Title

- **Status:** Proposed | Accepted | Superseded by ADR-XXXX
- **Date:** YYYY-MM-DD

## Context
What forces are at play; what made this a decision rather than a default.

## Decision
What we are doing, stated in the present tense.

## Consequences
What follows - the good, the bad, and what we now have to live with.

## Alternatives considered
What else was on the table and why it lost.
```

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0001](0001-single-owl-binary.md) | Single `owl` binary for everything headless | Accepted |
| [0002](0002-daemon-never-self-daemonizes.md) | The daemon never self-daemonizes | Accepted |
| [0003](0003-monorepo-single-go-module.md) | Monorepo with a single Go module | Accepted |
| [0004](0004-connectrpc-over-unix-socket.md) | ConnectRPC over a unix socket | Accepted |
| [0005](0005-internal-plugins-as-go-interfaces.md) | Plugins are Go interfaces, not external processes | Accepted |
| [0006](0006-host-execution-for-mvp.md) | MVP agents run as host processes, not containers | Accepted |
| [0007](0007-worktree-and-branch-per-job.md) | One git worktree and one branch per job | Accepted |
| [0008](0008-sqlite-queue-behind-source-interface.md) | Work queue is local SQLite behind a `Source` interface | Accepted |
| [0009](0009-wails-react-desktop.md) | Desktop app is Wails with a React frontend | Accepted |
| [0010](0010-homebrew-formula-and-cask-naming.md) | Homebrew ships a `coding-owl` formula and a `coding-owl-desktop` cask | Accepted |
| [0011](0011-idle-policy-and-interruption.md) | Idle policy and interruption semantics | Proposed |
