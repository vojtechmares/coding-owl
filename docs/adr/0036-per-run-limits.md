# ADR-0036: Every Run is bounded by a timeout and a stall limit

- **Status:** Accepted
- **Date:** 2026-09-17
- **Amends:** ADR-0025's default TTL

## Context

Every step of a Run was bounded except the one that matters. Setup commands got
fifteen minutes (ADR-0007), Verification checks five apiece (ADR-0030), and the
agent Verifier fifteen (ADR-0013). The planning and execution Agents - the long,
open-ended, unattended ones this product exists to run - got nothing.

The backstops that look like they cover this do not:

- **`budgetUSD`** is off unless a Project sets it, and it is the wrong lever on a
  subscription Account, where there is no dollar spend to cap. That is why
  dollar budgets are out of scope in ADR-0019 and ADR-0020 in the first place.
- **The ceiling** (ADR-0020) terminates a Run mid-flight, but it learns
  utilization *from that Run's own output stream*. An Agent that has wedged emits
  nothing, so the figure never moves; and one that is stuck is not spending
  tokens, so it would not move even if it did.
- **Idle interruption** (ADR-0011) fires when the user comes back. On a machine
  nobody touches it never fires, and that is exactly the case in question.
- **The TTL** (ADR-0025) counts attempts. A Run that never ends never completes
  an attempt, so the TTL never advances.

Each is sound for what it was built for, and a hung Agent goes between all four.

What makes it worse than a wasted night is concurrency. The default is one Run
at a time (ADR-0021) and a Run in flight holds that slot, so one wedged Agent
stops the queue **indefinitely** - not for a night, but until a person notices. A
scheduler that has silently stopped scheduling is the hardest failure to see,
because `owl status` still reports a Run in progress.

## Decision

Every phase carries **two** limits, and both are always set:

```yaml
phases:
  execute:
    timeout: 4h    # the longest this phase's Agent may run at all
    stall: 15m     # the longest it may go without saying anything
```

They cascade and are reported exactly as model and effort already are
(ADR-0028): defaults, then the daemon's configuration, then the Project's,
narrowest winning, with `owl jobs show` printing each effective value beside the
level it came from.

Defaults are `1h` for planning and `4h` for execution, with `15m` of silence for
both. Planning is short by design and execution is the long one; together they
fit inside a night rather than running into the user's morning. Fifteen minutes
is already what Owl waits for a frozen Run and for a Verifier.

### Why both

Neither limit subsumes the other, and the two failures are different shapes.

A total timeout is the only thing that catches an Agent working busily towards
nothing - it talks the whole time, so no amount of listening for silence will
ever end it. But it has to be generous enough for the longest honest Run, which
makes it slow to notice a tool that has simply stopped.

Silence catches that in minutes. Owl already timestamps every `stream-json`
event it reads (ADR-0012), so the measurement costs a timer reset per line.

### Passing a limit is the Run's own failure

A Run ended by a limit is recorded `failed` and **spends an attempt**, unlike
the grace window and the ceiling, which end a Run `interrupted` and cost it
nothing.

The distinction is whether the ending is something that happened to the Run or
something the Run did. A user coming back to their laptop is the former. A Run
that will not finish, or will not speak, is the latter - and if it cost no
attempt, a Job that wedges every single time would be queued again every night
for ever, which is the failure this ADR exists to end.

### The TTL default drops to 3

ADR-0025 set the default TTL to 10 and named that generosity as one of three
mitigations for a tension it recorded: under ADR-0011 an interrupted Run costs
an attempt exactly as a failure does, so a user who opens their laptop each
morning can exhaust a Job that is making real progress.

With Runs bounded, ten attempts is the wrong trade in the other direction: a Job
that wedges every time would spend forty hours over five nights proving it. Three
says so on the third night.

This narrows the ADR-0025 mitigation rather than removing it: `--ttl` at enqueue
and `owl jobs extend` remain, and an exhausted Job is still reported and never
deleted, so nothing is lost - it needs a decision.

## Consequences

- **A long honest Run can now be killed.** Four hours is a guess, and the first
  Project that legitimately needs more will find out by being cut off. It is
  configurable, `owl jobs show` prints the limit that did it, and the blocked
  reason names it - but this is a real behaviour change and the cost of having a
  bound at all.
- **The daily-laptop tension gets sharper.** Three attempts is three mornings,
  and ADR-0025's own consequence section explains why that stings. Users who hit
  it should raise the TTL rather than the limits.
- **Nothing new does the killing.** A limit ends a Run through the same
  SIGTERM-then-SIGKILL escalation against the whole process group that the grace
  window already uses (ADR-0011, ADR-0034).
- **The daemon-stop path must stay distinguishable.** A Run ended because the
  daemon is stopping is still `interrupted`; only the limits set the flag that
  makes an ending a failure. Getting that backwards would quietly charge
  attempts for the user's own shutdown, so it is pinned by a test.

## Alternatives considered

**A total timeout only**, as the production-readiness review suggested. One
setting, one reason, simplest to explain. Rejected because the timeout that is
generous enough for real work is far too slow to catch the common case, which is
a tool that stopped talking twenty minutes in.

**A stall limit only.** Catches the common case quickly and never cuts off an
Agent that is genuinely working. Rejected because it gives no bound at all on a
Run that keeps talking, which is precisely the runaway loop, and because "this
Run will end by some time" is a property worth being able to state.

**Ending a Run as `interrupted`**, matching the ceiling and the grace window.
Rejected: a Job that passes its limit every time then never exhausts, and the
queue-blocking failure returns in a slower form.

**A daemon-wide limit rather than a per-phase one.** One number to set. Rejected
because planning and execution differ by hours, so a single number is either
useless for planning or dangerous for execution - and ADR-0028 already made the
phase the unit these choices are made in.
