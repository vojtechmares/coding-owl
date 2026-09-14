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
- **Supersedes:** what of an earlier record this replaces, when it replaces part
  of one rather than all of it. Optional.
- **Amended:** YYYY-MM-DD, and what was corrected. For a record that said
  something untrue about its own mechanism: the decision stands, so the status
  does not change, but a reader should be able to see that the project once
  held it the other way. Optional.

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
| [0009](0009-wails-react-desktop.md) | Desktop app is Wails with a React frontend | Partly superseded by 0029 |
| [0010](0010-homebrew-formula-and-cask-naming.md) | Homebrew ships a `coding-owl` formula and a `coding-owl-desktop` cask | Accepted |
| [0011](0011-idle-policy-and-interruption.md) | Idle policy and interruption semantics | Accepted |
| [0012](0012-owl-owns-process-claude-owns-conversation.md) | Coding Owl owns the process, Claude Code owns the conversation | Partly superseded by 0026 |
| [0013](0013-verification-gates-completion.md) | Verification gates Job completion, and does not retry | Accepted |
| [0014](0014-configuration-and-filesystem-layout.md) | Configuration discovery and filesystem layout | Accepted |
| [0015](0015-job-disposal-and-garbage-collection.md) | Job disposal and worktree garbage collection | Accepted |
| [0016](0016-rebase-job-branch-each-run.md) | Rebase the Job branch onto its base at the start of every Run | Accepted |
| [0017](0017-unattended-contract.md) | Owl appends a standing unattended contract to every Run | Accepted |
| [0018](0018-driver-and-executor-axes.md) | Driver and Executor are separate plugin axes | Accepted |
| [0019](0019-accounts.md) | Accounts are first-class, with isolated tool configuration | Accepted |
| [0020](0020-account-utilization-ceiling.md) | Scheduling respects a per-Account utilization ceiling | Accepted |
| [0021](0021-concurrency-caps.md) | Concurrency is capped globally, per Project and per Account | Accepted |
| [0022](0022-desktop-chat.md) | Desktop chat is a daemon service with consent-gated, shell-free commands | Accepted |
| [0023](0023-job-account-binding.md) | A Job runs on its Project's Account | Accepted |
| [0024](0024-skills-declared-per-project.md) | Skills are declared per Project and managed by Owl | Accepted |
| [0025](0025-queue-order-and-job-ttl.md) | Queue order and Job attempt limits | Accepted |
| [0026](0026-phases-and-handoff-continuity.md) | Plan then execute, each Run in a fresh context, continuity by handoff document | Accepted |
| [0027](0027-job-and-run.md) | A Job is a standing intent; a Run is one attempt at it | Accepted |
| [0028](0028-model-and-effort-selection.md) | Model and effort are chosen per phase | Accepted |
| [0029](0029-build-order.md) | The core loop ships first; features land across daemon and Desktop together | Accepted |
| [0030](0030-verification-check-specification.md) | Verification checks are shell commands with optional expectations | Accepted |
| [0031](0031-project-identity.md) | A Project is identified by a name, not by its path | Accepted |
| [0032](0032-source-idempotency.md) | Every Job carries a Source reference, and producing is an upsert | Accepted |
| [0033](0033-skill-fetching-and-placement.md) | Skills are fetched natively in Go and placed per Driver | Accepted |
| [0034](0034-sigkill-after-sigterm.md) | An Agent that will not stop when it is asked is killed | Accepted |
| [0035](0035-agent-permissions-allowlist.md) | An unattended Agent is granted a default allowlist, once, per Account | Accepted |
