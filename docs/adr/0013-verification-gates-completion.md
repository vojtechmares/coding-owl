# ADR-0013: Verification gates Job completion, and does not retry

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

An Agent exiting cleanly says nothing about whether its work is any good. It
can stop having written nothing, having misunderstood the task, or having left
the build broken - and all of those exit zero.

That matters more here than in an interactive session. Nobody is watching, and
the result is read hours later. A Job wrongly marked `done` does not cost a
minute of confusion, it costs a day.

## Decision

After an Agent exits, Owl runs **Verification**. A Job reaches `done` only if
Verification passes.

Verification is a plugin interface in the sense of ADR-0005, with two
implementations:

- **Command verifier** - runs the Project's own checks: tests, linters,
  formatters. This is the default.
- **Agent verifier** - starts a **fresh** Claude Code Session to review the
  diff. Fresh is the whole point: the reviewer must not carry the context that
  produced the work.

**In the MVP, a failed Verification stops.** The Job moves to `blocked` with
the failure attached, and waits for the user. There is no automatic retry and
no fixer loop.

Job states are `pending`, `active`, `blocked`, `done`, `cancelled`.

## Consequences

- The morning review separates "done" from "needs you", which is the
  distinction that makes an overnight tool worth having at all.
- Spend is bounded and predictable. There is no path by which Owl burns a night
  looping on a failure it cannot fix.
- Every branch presented for review is the product of exactly one Agent pass,
  which keeps the diff comprehensible.
- Trivially fixable failures - a formatting nit, one broken assertion - wait a
  full day for the user. This is accepted deliberately: automated resolvers are
  the intended next step, and the `Verifier` interface is where they attach.
- Each Project needs its check commands defined somewhere. That is a separate
  open decision.
- The agent verifier costs a second Agent invocation per Run, so it is opt-in
  per Project rather than the default.

## Alternatives considered

**Trust the Agent's exit status.** Rejected explicitly. It is the cheapest
option and the one that makes the product untrustworthy, because the failure it
misses is precisely the overnight one.

**Ask the Agent for a structured verdict** via `--json-schema`. Better than raw
exit status, but the Agent is still grading its own homework in the context that
produced the work.

**Feed failures back automatically, bounded by caps.** The natural end state,
and where automated resolvers will land. Deferred: it is a meaningfully larger
machine - retry caps, budget caps, loop detection - and outside MVP scope.

**Per-check retry policy**, auto-fixing cheap deterministic failures such as
formatting while surfacing test failures. Deferred along with retry generally.
