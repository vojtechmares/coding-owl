# ADR-0015: Job disposal and worktree garbage collection

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

ADR-0007 gives every Job a worktree and a branch and explicitly leaves their
cleanup undecided. Worktrees share the object store but each holds a full set of
working files, so a week of finished Jobs is a week of checkouts sitting on disk.

There is also a second, messier population: worktrees whose Job no longer
matches them. A daemon killed mid-Run, a Job deleted from the database, a
directory removed by hand, a `git worktree` admin entry pointing at nothing.
Nothing in the Job lifecycle cleans those up, because by definition they have
fallen out of it.

## Decision

### Disposal is explicit

A Job that passes Verification enters **`review`**, not `done`. From there:

- `owl jobs accept <id>` - removes the worktree, keeps the branch, Job becomes
  `done`
- `owl jobs drop <id>` - removes the worktree and deletes the branch, Job
  becomes `cancelled`

Owl additionally treats **"branch merged into the Project's base branch" as an
implicit accept**, so merging through normal git tooling cleans up without a
second step.

Owl never pushes a branch anywhere. Everything stays local in the MVP.

### Garbage collection

The daemon runs a periodic **garbage collection** task - plain Go, no Agent
involved - on start and on an interval, also invokable as `owl gc`. It:

- reconciles worktrees on disk against Jobs in the database, removing those
  whose Job is gone, `cancelled`, or already `done`
- runs `git worktree prune` per Project to clear stale admin entries
- surfaces, rather than deletes, anything that looks like **unfinished work**:
  a worktree with uncommitted changes, a Job stuck in `review` beyond a
  threshold, a Job left `active` by a daemon that died

Unfinished work is reported through `owl status` and shown in the desktop app.

## Consequences

- The morning review has an actual verb, and `owl status` can always answer
  "what is waiting for me".
- Disk stays bounded without a timer that deletes things out from under the
  user mid-review.
- Garbage collection never destroys uncommitted work silently. Deleting a
  worktree that has changes in it requires the user to say so - the point of the
  task is to report as much as to reclaim.
- The desktop app gains a surface for "you have three Jobs waiting and one
  worktree with stray changes". This is not a notification in the system sense;
  ADR-scope still excludes those for the MVP.
- `review` is a fifth Job state, and the state set becomes `pending`, `active`,
  `blocked`, `review`, `done`, `cancelled`.
- Merge detection needs a per-Project poll against the base branch, which is
  cheap but is a scheduled task the daemon did not otherwise need.

## Alternatives considered

**Let git decide, with no verbs.** Owl removes a worktree once its branch merges
and does nothing else. Elegant, and the user's normal workflow is the whole
interface. Rejected because rejection is inexpressible: a branch reviewed and
declined looks exactly like one never opened, so those worktrees live forever
and Owl can never state what is genuinely outstanding.

**Time-based cleanup.** Remove worktrees N days after completion. Bounded disk
for zero ceremony, but a worktree can vanish mid-review, which is surprising
in the worst way the first time it happens.
