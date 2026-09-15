---
title: Coding Owl
tagline: Your machine works while you are away.
description: Coding Owl runs coding agents on your machine while it is idle, so the work you queued up is waiting for review when you come back.
install: brew install vojtechmares/tap/coding-owl
---

## What it is

Coding Owl is a daemon and a small desktop app for your Mac. You register a git
repository as a Project and queue Jobs against it - a prompt each, with the
branch and checks it should run under. When the machine has been idle for ten
minutes and is on AC power, Owl picks the next Job, gives it a worktree and a
branch of its own, and runs a coding agent on it. Claude Code is the first
agent it knows how to drive.

Nothing runs while you are at the keyboard. The moment you come back, the
agent is stopped and the machine is yours again.

## How it works

- **A Job is a standing intent.** It survives more than one attempt. Each
  attempt is a Run with a fresh Session, and what carries between Runs is a
  Handoff document committed on the Job's branch, not a conversation.
- **Owl owns the process, the agent owns the conversation.** Owl decides when
  to start, when to stop and what counts as done. The agent decides how to do
  the work.
- **Verification gates completion.** After the agent exits, Owl runs the
  Project's own checks. A Job is only done once they pass; until then it comes
  back for another Run.
- **Unattended by contract.** Every Run tells the agent it is running alone:
  make reasonable assumptions, write them down, commit as you go, and stop
  plainly when blocked.
- **Capacity is respected.** Owl schedules against your subscription's rate
  limit with a ceiling, so it never spends the headroom you need in the
  morning.

## When it fits

- **The backlog you never get to.** Refactors, test coverage, dependency
  bumps, the rename that touches forty files. Queue them, go home, review a
  branch in the morning.
- **Long-running, low-attention work.** Anything that takes an agent an hour
  and takes you ten minutes to review.
- **One machine, one subscription.** Owl uses the Claude Code you already have
  installed and the account you already pay for. There is no server and no
  extra seat.

## What it is not

It is not a cloud service and it is not a chat window. Owl does not run in the
background while you work, and it does not merge anything. Every result is a
branch waiting for a human.
