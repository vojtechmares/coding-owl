# ADR-0016: Rebase the Job branch onto its base at the start of every Run

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

A Job can span several nights (ADR-0011), and its base branch moves while it
does. That undermines two things at once. Verification (ADR-0013) run against a
week-old base can fail for reasons that have nothing to do with the Job, and
pass for reasons that will not survive contact with current code. And the diff
presented for review is against a base nobody is on any more.

## Decision

At the start of **every** Run, Owl fetches and rebases the Job's branch onto the
Project's base branch.

- Clean rebase - the Run proceeds, and Verification runs against the rebased
  state.
- Conflict - `git rebase --abort`, and the Job moves to `blocked` naming the
  conflicting paths.

The first Run of a Job has nothing to rebase; its branch is cut fresh from the
base.

## Consequences

- A green Job is green against today's base, which is the only claim worth
  making.
- The review diff is linear and against current `main`.
- Rebasing rewrites the commits the Agent made, so a resumed Session's memory of
  its own shas goes stale. Claude Code orients from the working tree rather than
  from commit ids, so it recovers - but a prompt that names a sha will not
  resolve after a rebase.
- The daemon rewrites git history unattended. That is bounded by the fact that
  the branch is Owl's own, exists only for this Job, and is never pushed
  (ADR-0015). Owl must never rebase anything else.
- Abort handling has to be solid: a Run must never begin on top of a worktree
  left mid-rebase. Garbage collection (ADR-0015) should detect and report that
  state rather than trying to resolve it.
- "Conflict blocks the Job" means an active Project can block a long-running Job
  repeatedly. That is the honest signal, and the fix is smaller Jobs.

## Alternatives considered

**Merge the base in instead of rebasing.** Does not rewrite the Agent's
commits, which is a genuine advantage for a resumed Session. Rejected because
Claude Code re-reads the tree rather than relying on shas, so the advantage is
mostly theoretical, and merge commits clutter a short-lived branch that exists
only to be reviewed once.

**Leave the branch alone.** Perfectly predictable, and no unattended git
operation can surprise anyone. Rejected because it makes Verification a claim
about a base that no longer exists, which is the one thing the gate in ADR-0013
was added to prevent.
