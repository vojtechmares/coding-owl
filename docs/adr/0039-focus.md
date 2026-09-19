# ADR-0039: Each Account may have one Focus, a Project whose Jobs go first

- **Status:** Accepted
- **Date:** 2026-09-19
- **Amends:** ADR-0025's "there is no priority field"

## Context

ADR-0025 made the queue FIFO with no priority field, and put urgency in
`owl queue reorder`. That rejected priority tiers for their inflation: once
three things are first, nothing is.

Reordering is a one-off, though. Moving `api`'s Jobs to the front does nothing
for the `api` Job queued tomorrow, which lands at the back. When one Project has
a deadline, keeping it first means reordering after every enqueue.

## Decision

A **Focus** is a Project marked to go first on its Account. Each Account has at
most one, and it is standing until cleared.

**It orders, it does not filter.** The scheduler scans focused Projects' Jobs
first, then everything else, each part oldest first. A focused Project with
nothing runnable - blocked, at its Account's Ceiling, at a cap - holds nothing
up: the rest of the queue runs as it would have.

**Focused Jobs go ahead of unfocused Jobs on any Account.** Where the global cap
binds (ADR-0021), a focused Job on one Account starts before an older unfocused
Job on another. Two Accounts' focused Projects take turns oldest first.

**It is daemon state, not configuration.** `owl focus <project>` sets it for
that Project's Account, `owl focus clear -a <account>` clears it, and
`owl focus` lists it. Focusing another Project on the same Account moves it,
and says so. It is cleared when its Project is removed or configured onto
another Account.

## Consequences

- A standing Focus is the priority ADR-0025 declined, confined to one Project
  per Account. One per Account is what keeps it from inflating: there is no
  second rank to promote something to.
- `owl status` has to say when a Job was started ahead of an older one because
  of Focus, as it already says when a Job was skipped.
- A Focus left on keeps its Project first for weeks. That is its meaning, and
  why `owl focus` and the desktop app show it wherever the queue is shown.
- It needs no Shift and no Idle of its own: it orders whatever work is allowed.

## Alternatives considered

**Focus as a filter**, so only its Project runs. Leaves the machine idle while
work is runnable, which ADR-0025 rejected for strict head-of-line ordering.

**One Focus for the whole daemon.** Simpler, and wrong for a person who runs
separate Accounts for separate clients side by side.

**Several ranked Focuses per Account.** The priority tiers ADR-0025 rejected,
under another name.

**Focus in the daemon's config file.** Beside the Ceiling and caps, and wrong
for something flipped during the day: config describes the setup, commands own
the state.

**Focus only within its Account**, leaving FIFO across Accounts. Then focusing
a Project does not make it next whenever the global cap binds.
