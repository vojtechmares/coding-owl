# ADR-0034: An Agent that will not stop when it is asked is killed

- **Status:** Accepted
- **Date:** 2026-09-11

## Context

ADR-0011 names `SIGTERM` on the Agent's process group as the last step of
interruption: a Run that stays frozen for the whole grace window is continued,
asked to stop, and recorded `interrupted` once it has gone. It says nothing
about an Agent that catches `SIGTERM` and carries on.

Such an Agent is not hypothetical. A coding tool that traps signals to save
its own state, a test runner in the group that ignores them, or a process
wedged in an uninterruptible call all stay alive after being asked to stop.
The Run is then still in progress, its Job is still `active`, and the queue is
held by it. No verb reaches it: `owl pause` and `owl resume` act on a live
Run, `owl jobs drop` refuses a Job with a Run going, and stopping the daemon
only asks again. The Job is held for ever, which is the one outcome ADR-0011
exists to prevent.

The executor already kills a cancelled Agent that ignores `SIGTERM` for five
seconds, because a daemon that is stopping cannot wait for ever either. The
grace window ended up with a weaker guarantee than a daemon shutdown.

## Decision

**The grace window escalates past `SIGTERM` to `SIGKILL`.** When the window
expires, the Run's process group is continued and sent `SIGTERM`, as
ADR-0011 says. An Agent that is still there five seconds later is sent
`SIGKILL`, on the group, so that what it started goes with it.

The delay is the same five seconds the executor gives a cancelled Agent,
and for the same reason: it is long enough for a tool that does act on
`SIGTERM` to flush and exit, and short enough that nobody notices it as a
wait. It is not configurable; the grace window is the tunable, and this is
the guarantee behind it.

A Run ended this way is recorded `interrupted` with the same reason as one
that stopped when asked. Its Job returns to `pending` in the place it held,
and its worktree, branch and commits are kept, exactly as for `SIGTERM`. What
was not committed is lost, which is also true of `SIGTERM` landing mid-tool-
call, and is what the handoff document and per-Run commits exist to bound
(ADR-0011, ADR-0026).

The same escalation applies to a Run being ended by a daemon that is
stopping; that was already the executor's behaviour and is now the stated
one.

## Consequences

- The grace window is a guarantee: a paused Run is over at most five seconds
  after the window, whatever the Agent does. Nothing can hold a Job for ever.
- An Agent that traps `SIGTERM` to save state gets five seconds to do it.
  That is a contract Drivers have to fit, and a Driver whose tool needs
  longer has a reason to say so in its own ADR.
- `SIGKILL` cannot be caught, so a tool killed this way leaves whatever it
  keeps outside git - a lock file, a half-written cache - in whatever state it
  was in. The next Run of the Job starts in the same worktree and has to cope,
  which it already had to for a machine losing power.
- ADR-0011 stays as written: `SIGTERM` is still the step that ends a Run that
  is behaving. This record adds the step after it.

## Alternatives considered

**Ask again, and keep asking.** Repeating `SIGTERM` changes nothing for an
Agent that ignores it, and leaves the Job held.

**Leave it to the user with a verb.** An `owl kill` that a person runs when
they notice. But the whole point of the grace window is that nobody is there
to notice: it fires because the user closed the lid and did not come back.
An unattended contract (ADR-0017) cannot end in "a human notices".

**A configurable delay.** Nobody has asked for one, and a second tunable next
to the grace window is a second thing to get wrong. Five seconds is what the
executor already uses; if a Driver needs a different figure, that is the
place to decide it.

**Make it the Driver's problem.** Have each Driver say how to stop its tool.
Correct in principle, and ADR-0018 leaves room for it, but every Driver ends
at the same place: after asking nicely, kill. The common step lives once.
