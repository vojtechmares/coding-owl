# ADR-0008: Work queue is local SQLite behind a `Source` interface

- **Status:** Accepted
- **Amended:** 2026-09-18, to name the migration strategy this record called for
  and never described. It said only that one was needed, so the append-only
  policy the runner depends on had to be reconstructed from the code.
- **Date:** 2026-09-09

## Context

In the MVP, the work Coding Owl picks up is prompts the user wrote ahead of
time. But that is one of several plausible producers. Later ones include issues
from GitHub, GitLab or a self-hosted Forgejo; scheduled maintenance work such
as test and lint runs; and continuation of whatever the user was last working
on.

If the MVP hard-codes "the queue is the source", every one of those becomes a
parallel execution path bolted onto the scheduler.

## Decision

A local **SQLite** database holds Jobs and their Run history. `Source` is an
interface from day one, and the local queue is simply its first
implementation.

Future producers are **queue producers** - they feed the same queue rather than
introducing a second path into the scheduler.

## Consequences

- No external service, no daemon dependency to install, and the queue survives
  restarts.
- Run history, Job status, and the data behind `owl jobs list` come for free
  from the same store rather than needing separate bookkeeping.
- Single-writer access matches the daemon's shape exactly; the CLI and GUI
  reach it through the API, never by opening the database.
- A schema migration strategy is needed from the very first release, because
  this file lives on user machines across upgrades. It is hand-rolled and
  deliberately small: `.sql` files embedded in the binary under
  `internal/store/migrations/`, applied in file name order, with progress
  tracked in SQLite's own `PRAGMA user_version` as the index into that sorted
  list. The version therefore records how many migrations have been applied,
  not which ones, which is correct only while the list is **append-only** - a
  migration is added at the end and never inserted between others, renamed,
  deleted or edited. `internal/store/migrations.sha256` records what has
  shipped and `TestMigrationsAreAppendOnly` fails when the policy is broken,
  so the rule does not live only in a maintainer's head (ADR-0017).
- Because future sources converge on the queue, scheduling, idle gating, and
  pause/resume stay one code path forever.

## Alternatives considered

**Flat files or JSON on disk.** No querying, and concurrent status updates from
a running daemon get awkward fast.

**An embedded key-value store such as bbolt.** Fine for a queue, notably worse
for the history and status queries the CLI and GUI both want.

**Polling GitHub directly with no local queue.** Rejected. It couples the MVP
to one forge and leaves offline and ad-hoc prompts with nowhere to live.

**A migration library such as `golang-migrate` or `goose`.** Not adopted. The
hand-rolled runner is correct under the append-only policy, and it is a few
dozen lines with no dependency and no extra table in the database. Swapping it
once v0.1.0 is on user machines would need a back-fill of its own bookkeeping
from the `user_version` those databases already carry, so it is not free later
either. Revisiting this is a new issue with an ADR of its own.
