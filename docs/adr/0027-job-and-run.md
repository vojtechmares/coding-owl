# ADR-0027: A Job is a standing intent; a Run is one attempt at it

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

This split has been assumed by ADRs 0007, 0008, 0011, 0015, 0025 and 0026
without ever being decided in its own right. It deserves recording, because it
is the shape everything else is built on and the alternatives were real.

The tempting model is one row: the thing you queue is the thing that runs, with
a status field. It collapses under two forces already committed to. A Job takes
several nights (ADR-0011, ADR-0026), so an attempt is plainly not the same thing
as the work. And ADR-0008's `Source` interface anticipates scheduled maintenance
Sources, where one standing intent runs repeatedly forever.

## Decision

Two entities.

**Job** - the standing intent. Its Source, Project, prompt, configuration,
worktree, branch, TTL and state. One Job is reviewed as one branch.

**Run** - one attempt at a Job. Its attempt number, start and end, outcome, log
location, and the Account utilization observed while it ran.

The worktree and branch belong to the **Job** (ADR-0007), so several nights
accumulate in one place.

| | |
| --- | --- |
| Job states | `pending`, `active`, `blocked`, `review`, `done`, `cancelled`, `exhausted` |
| Run outcomes | `succeeded`, `failed`, `interrupted` |

## Consequences

- Recurring Sources arrive later with no schema change: a nightly lint Job is one
  intent with many Runs, which is what the model already says.
- Review is per Job. The user is never asked which of four attempt-branches is
  the real one.
- Run history is where operational questions get answered - how many nights this
  took, what utilization it drew, which attempt first went wrong.
- The distinction has to be held in language and in the UI. The moment `owl
  status` starts saying "job failed" when it means "this Run was interrupted",
  the model has collapsed in everything but the schema.
- Two tables and a join for the common "show me my Jobs with their latest Run"
  query. Cheap, but it is the query everything renders.

## Alternatives considered

**One entity with a status field.** The queue row *is* the job, and an
interruption flips it back to `queued` to reuse the same worktree. Simplest
possible schema. Rejected because there is nowhere to record that this is the
third attempt, and nowhere to hang per-attempt facts like observed utilization -
and because getting it wrong later means migrating databases that already exist
on user machines.

**One entity with lineage**, where an interrupted Job closes and a successor
points back at it through `continues_job_id`. Preserves history without a second
table. Rejected because it reads badly for exactly the case ADR-0008 is
designing for: a recurring maintenance task becomes an ever-growing chain rather
than one intent with many executions.
