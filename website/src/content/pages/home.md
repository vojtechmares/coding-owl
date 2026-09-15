---
title: Coding Owl
eyebrow: Coding agents, while you are away
tagline: Your machine works while you are away.
description: Coding Owl runs coding agents on your machine while it is idle, so the work you queued up is waiting for review when you come back.
install: brew install vojtechmares/tap/coding-owl
features:
  - label: Idle
    title: Only when you are away.
    text: Ten minutes without input, on AC power, and Owl starts. The moment you are back, the agent is frozen and the machine is yours.
  - label: Isolated
    title: A worktree and a branch per Job.
    text: An agent never touches your checkout. Every Job works on its own branch, rebased onto the base branch at the start of each Run.
  - label: Verified
    title: Done means the checks pass.
    text: After the agent exits, Owl runs your project's own checks. A Job is finished only once they pass, and lands as a branch for you to review.
---

## How it works

- **A Job is a standing intent.** It survives more than one attempt. Each
  attempt is a Run with a fresh Session, and what carries between Runs is a
  Handoff document committed on the Job's branch, not a conversation.
- **Owl owns the process, the agent owns the conversation.** Owl decides when
  to start, when to stop and what counts as done. The agent decides how to do
  the work.
- **Unattended by contract.** Every Run tells the agent it is running alone:
  make reasonable assumptions, write them down, commit as you go, and stop
  plainly when blocked.
- **Capacity is respected.** Give an Account a ceiling and Owl schedules
  against your subscription's rate limit under it, so it never spends the
  headroom you need in the morning.

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

## Risks

- **There is no sandbox.** Agents run as your user, with your files, your
  network and whatever you are signed in to. The worktree is where an agent
  works, not a wall around it.
- **The default allowlist runs code.** Build tools and package runners, without
  asking, and `git` includes `git push`.
- **No limits until you set them.** Nothing caps a Run's time or spend by
  default.
- **The desktop app is not notarised.** It is ad-hoc signed, and the cask
  clears the quarantine flag.

Read [the risks in the guide](/docs/guide#risks) before you queue work.
