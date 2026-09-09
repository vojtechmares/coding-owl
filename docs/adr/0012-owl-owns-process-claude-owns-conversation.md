# ADR-0012: Coding Owl owns the process, Claude Code owns the conversation

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Claude Code 2.1.266 ships its own background-agent manager. `claude --bg`
dispatches a detached session and prints a short id; `claude agents --json`
lists them without a TTY; `claude logs <id>` prints recent output; `claude stop
<id>` halts a session while keeping its conversation; `claude --resume`
continues it; and `claude rm <id>` deletes a session "and its worktree when
that is safe".

That overlaps a large part of what Coding Owl was planning to build. Where the
boundary sits needs stating deliberately, because discovering it later means
discovering it as duplicated work.

## Decision

**Coding Owl owns** the worktree, the branch, scheduling, process lifecycle and
log capture. It runs the Agent as a direct child process of the daemon:

```
claude -p --output-format stream-json ...
```

**Claude Code owns conversation state.** Owl stores the Claude Code session id
on the Job. A later Run continues that conversation with `--resume
<session-id>` instead of re-prompting from scratch.

Owl does **not** use `claude --bg`, `claude agents`, `claude logs`, `claude
stop` or `claude rm`.

ADR-0018 generalises this: everything Claude-specific below is the contract of
the `claude-code` **Driver**, not of Owl. Other Drivers answer the same
questions differently, and declare which of them they can answer at all.

## Consequences

- ADR-0007 stands unchanged. The worktree and branch are Owl's, created per Job.
- Owl owns stdout and exit status, so the log stream in ADR-0004 has a single
  source, and the `--print`-only flags are all available per Run:
  `--max-budget-usd` to cap spend on an unattended night, `--permission-mode`,
  and `--allowedTools`/`--disallowedTools`.
- `--permission-prompts none` becomes the default unattended posture: anything
  that would prompt is **denied** rather than auto-approved. For work nobody is
  watching this is strictly better than `--dangerously-skip-permissions`, and it
  materially softens the "there is no sandbox" consequence in ADR-0006.
- Conversation context carries across nights at no cost in tokens or wall-clock.
  The `job` table gains a `session_id` column.
- Owl's Agents never appear in the user's own `claude agents` listing, so
  scheduled work and interactive work stay visibly separate.
- The coupling is now to two narrow surfaces - the `-p` `stream-json` protocol
  and `--resume` - rather than to the whole background-agent CLI. Both are still
  a CLI contract rather than a stability-guaranteed API. Pin a tested version
  range and fail loudly on mismatch.
- The `Executor` interface (ADR-0005) keeps its meaning: the container
  implementation runs the same command, inside a container.

## Alternatives considered

**Delegate to Claude Code's background agents.** Owl becomes a scheduler on top
of `--bg`, `agents --json`, `logs`, `stop` and `rm`, and gets worktrees, session
persistence and log capture for free. Rejected because it couples Owl to a much
larger slice of a CLI that carries no stability guarantee, mixes Owl's sessions
into the same list as the user's own interactive ones, and hollows out the
`Executor` interface that the container future depends on.

**Own everything, with no session reuse.** Every Run is a fresh `claude -p`
that orients itself from the worktree's git log and diff. Maximum independence,
and the easiest path to running a coding agent other than Claude Code. Rejected
for the MVP because it pays real tokens and wall-clock re-reading state every
night, and invites an Agent to undo or redo its own earlier work. Worth
revisiting if `Executor` ever needs to run a different agent.
