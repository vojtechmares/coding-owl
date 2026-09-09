# ADR-0007: One git worktree and one branch per Job

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Jobs run while the user is away, and the user comes back to review what
happened. Two things follow: the Job must not disturb whatever the user had
checked out, and its output has to arrive in a form that is pleasant to review
the next morning.

Host execution (ADR-0006) adds a second motivation - with no sandbox, the
working directory is the only structural bound on where an Agent operates.

## Decision

Each Job gets a dedicated **git worktree** in a scratch directory, with the
Agent's working directory set to it. Work lands on a **named branch**.

## Consequences

- The user's own checkout is never touched, so an overnight Job cannot collide
  with uncommitted work.
- Review is an ordinary `git diff` or branch checkout in whatever tooling the
  user already likes. The desktop app deliberately does not need a diff viewer
  on day one because of this.
- It doubles as the blast-radius mitigation for host execution, which is why
  both ADRs point at each other.
- The worktree and branch belong to the **Job**, not to a single Run, so a Job
  that takes several nights accumulates its work in one place and is reviewed
  as one branch.
- Concurrency later is a scheduling decision, not a correctness problem - two
  Jobs in two worktrees do not contend. (MVP still runs one Run at a time.)
- Costs disk per Job. Reclaiming it is ADR-0015.
- Projects whose builds need untracked local files - `.env`, caches, installed
  dependencies - will not work in a fresh worktree without per-Project setup
  commands. That is what per-Project config is for (ADR-0014).

## Alternatives considered

**Run in the user's checkout.** Rejected. It destroys the "walk away, come back
and review" premise and races with the user's own edits.

**Emit a patch file and a report.** Safest, but reviewing patches is worse
ergonomics than reviewing a branch, and it throws away git's own tooling.

**Auto-open a draft PR.** Attractive for the morning review, but it needs push
credentials and a forge integration in the MVP. It is a natural follow-up once
branches work.
