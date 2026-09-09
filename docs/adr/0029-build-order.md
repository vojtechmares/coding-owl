# ADR-0029: The core loop ships first; features land across daemon and Desktop together

- **Status:** Accepted
- **Date:** 2026-09-09
- **Supersedes:** the "day-one surface" paragraph of ADR-0009

## Context

The MVP includes both the CLI and the desktop app. Read one way, that says build
a complete desktop surface for the first release, and ADR-0009 said as much by
listing a day-one feature set.

The trouble is that the desktop app is not the product. The scheduler is. A plan
that builds a full frontend before an Agent has ever run overnight puts the part
that can be evaluated last, behind the part that cannot.

## Decision

**The core loop is built first, in the daemon**, and proven end to end before any
feature work spreads outward.

After that, **features land one at a time, across daemon, API and desktop app
together**. A feature is not done when the daemon can do it; it is done when the
daemon does it, the API exposes it, and the desktop app shows it.

There is no "desktop v1 feature set". The desktop app is whatever the features
shipped so far have put in it.

## Consequences

- The proto contract is **grown per feature rather than designed up front**.
  ADR-0004's Buf breaking-change detection is what makes that safe, and this is
  the reason it was worth adopting.
- ADR-0009's pure-view rule stops being an aspiration and becomes the working
  method: a feature that cannot be expressed as a daemon RPC is not shippable.
- The desktop app will look thin for a while. That is expected, not a gap - a
  reader finding a sparse frontend should not conclude it was abandoned.
- Every feature costs frontend work at the moment it is added, so features are
  more expensive individually and the ordering of the backlog matters more.
- The CLI stays ahead of the desktop app in practice, since it is the cheapest
  client to extend. That is fine; it is not a promise that it always will be.

## Alternatives considered

**A defined desktop v1** - Review and Status surfaces built as one release, chat
and skills deferred. Gives a coherent first thing to look at and a clear
milestone. Rejected because it still front-loads frontend work ahead of a
scheduler that has never run.

**Everything at once**, desktop app as the complete surface from the start. The
version worth demonstrating, and the version where the frontend is the critical
path for months while the actual product waits behind it.
