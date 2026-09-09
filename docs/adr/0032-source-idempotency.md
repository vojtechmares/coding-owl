# ADR-0032: Every Job carries a Source reference, and producing is an upsert

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

ADR-0008 committed to a `Source` interface and said future producers feed the
queue rather than becoming alternate execution paths. It did not say how a
Source avoids producing the same Job twice.

That matters as soon as there is a second Source. A GitHub Source polling every
five minutes must not create a Job on every tick. A nightly maintenance Source
must create exactly one Job per night.

It also has to be decided now rather than later, because it is a schema
question, and ADR-0008 explicitly called out migrating databases that already
live on user machines as a real cost.

## Decision

Every Job carries **`source`** and **`source_ref`**, unique together.
Production is an **upsert** on that pair.

| Source | `source_ref` |
| --- | --- |
| local queue | a generated ulid |
| GitHub issues | `issue:1234` |
| scheduled maintenance | `lint:2026-09-09` |

## Consequences

- Idempotency is structural rather than a discipline each Source has to
  remember. A Source that produces the same logical work twice simply writes the
  same row twice.
- Periodicity falls out of the same rule: encoding the date in the ref makes
  "once per night" a consequence of the unique constraint rather than a separate
  mechanism with its own bugs.
- `source_ref` is an opaque string whose meaning lives inside each Source. A
  Source that picks a bad convention is only caught by its own tests, and the
  convention cannot be changed later without orphaning the Jobs it already made.
- Two columns are carried through the first milestone that the local queue
  barely uses. That is the point - it is the migration that does not happen.
- **Reopening a closed GitHub issue would produce the same ref**, and therefore
  no new Job. Whether that is correct is a Source-level policy question, and it
  is deferred until a GitHub Source actually exists rather than guessed at now.

## Alternatives considered

**Per-Source state tables** holding watermarks - last seen issue, last fired
timestamp - with the Job carrying only its Source. Arguably more honest, since
Sources genuinely differ and a watermark is not the same shape as a reference.
Rejected because every Source then implements idempotency separately, and each
gets it subtly wrong in its own way.

**Defer it entirely** until a second Source exists, designing against real
requirements rather than guesses. Rejected only because of the migration cost;
on a greenfield database it would be the better instinct.
