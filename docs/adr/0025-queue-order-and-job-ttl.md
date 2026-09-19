# ADR-0025: Queue order and Job attempt limits

- **Status:** Accepted
- **Amended:** 2026-09-19, by ADR-0039: each Account may have one Focus.
- **Date:** 2026-09-09

## Context

With concurrency defaulting to one (ADR-0021), queue order *is* the schedule.

Two things complicate the obvious implementation. Jobs are pinned to their
Project's Account (ADR-0023) and Accounts have utilization ceilings
(ADR-0020), so the oldest queued Job can be ineligible while a younger one is
perfectly runnable. And nothing yet capped re-attempts: under ADR-0011 an
interrupted Run returns its Job to pending, which on its own repeats forever.

## Decision

### Order

FIFO. The scheduler scans oldest-first and starts the first Job whose global
cap, Project cap and Account ceiling all currently permit. A skipped Job keeps
its position and is reconsidered on the next tick.

There is no priority field. Urgency is expressed with `owl queue reorder`, and
a re-attempt never changes a Job's position.

> **Amended 2026-09-19.** One exception: each Account may have a Focus, a
> Project whose Jobs are scanned ahead of the rest (ADR-0039). Within each part
> the order is still FIFO.

### Attempt limit

Every Job carries a **TTL**, defaulting to 10, decremented at the end of every
Run whatever its outcome. At zero the Job becomes `exhausted`: no longer
scheduled, and reported in `owl status` and the desktop app.

TTL is settable at enqueue (`owl add --ttl`) and can be topped up with
`owl jobs extend <id>`.

The full Job state set is `pending`, `active`, `blocked`, `review`, `done`,
`cancelled`, `exhausted`.

## Consequences

- The scheduler never wastes an idle night on a Job whose Account does not reset
  until morning.
- Because skipping is allowed, the queue is no longer a literal plan. `owl
  status` has to be able to say why a Job was passed over, or the ordering looks
  arbitrary.
- The TTL means exactly what it says, with no heuristic behind it: ten Runs, and
  the Job stops.
- **It counts healthy nights.** Under ADR-0011 a user who opens their laptop
  each morning ends nearly every Run `interrupted`, so a large Job can exhaust
  its TTL while making real progress every single night. The mitigations are the
  generous default, `--ttl` at enqueue, and `owl jobs extend`; and an exhausted
  Job is reported, never deleted, so no work is lost - it needs a decision.
- `exhausted` is distinct from `blocked` on purpose: blocked means something is
  wrong with the work, exhausted means the Job ran out of attempts. They want
  different responses.

## Alternatives considered

**Decrement only on Runs that made no progress**, measured as new commits on the
branch - which ADR-0017 guarantees are there. Targets spinning precisely and
never penalises a Job for the user waking up. Rejected in favour of a counter
whose meaning needs no explanation.

**Progress-based TTL plus an absolute Run cap.** Catches both spinning and
non-convergence. Rejected as two numbers where one will do, the second of which
would almost never fire and so would be almost untested.

**Strict head-of-line ordering** - only the head is ever considered. Perfectly
honest as a plan, and idles the machine whenever the head cannot run.

**Priority tiers.** Urgency without reordering, at the cost of a field on every
enqueue and inevitable inflation when one person both sets and reads it.
