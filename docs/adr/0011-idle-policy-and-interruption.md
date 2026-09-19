# ADR-0011: Idle policy and interruption semantics

- **Status:** Accepted
- **Amended:** 2026-09-19, by ADR-0038: the manual override is now a Shift.
- **Date:** 2026-09-09

## Context

The product premise is doing work while the machine is otherwise idle, which
makes two questions load-bearing rather than incidental: what counts as idle,
and what happens when the user comes back while a Run is in flight.

Host execution (ADR-0006) sharpens the first. There is no VM to blame - the
Agent burns real CPU on the user's laptop, so an idle policy that ignores power
state will happily flatten a battery in a bag.

The second question turns on a detail of how Agents are actually run. Owl
invokes `claude -p` as a child process (ADR-0012), and a single invocation
executes many tool calls before it returns. There is no "step" boundary an
outside process can stop at, so graceful interruption has to be built from
signals rather than from cooperation.

What makes that tolerable is that progress is durable outside the Agent: commits
on the Job's branch, and a handoff document kept current as the work proceeds
(ADR-0026). Killing an Agent loses its train of thought, not its work.

## Decision

**Idle policy is configurable**, defaulting to: no keyboard or mouse input for
**10 minutes** *and* the machine **on AC power**.

**Manual override exists in both directions.** `owl start` begins work
regardless of idle state; `owl pause` stops it regardless.

> **Amended 2026-09-19.** `owl start` begins one Run, not work in general.
> Working while the machine is in use is a Shift (ADR-0038), which also decides
> which Runs freeze on return and what `owl pause` holds.

**Concurrency was one Run at a time** in the MVP. Superseded by ADR-0021,
which caps it globally, per Project and per Account.

### Returning to the machine: freeze, then release

1. On the first input event, Owl sends **`SIGSTOP`** to the Agent's process
   group. The machine is immediately the user's again and nothing is lost.
2. If the machine becomes idle again within a **grace window (default 15
   minutes)**, Owl sends `SIGCONT` and the same Run carries on where it was.
3. If the grace window expires, Owl escalates to **`SIGTERM`** on the process
   group. The Run ends with outcome `interrupted`, and the next idle window
   starts a fresh Run that orients from the handoff document (ADR-0026).

**On daemon restart, in-flight Runs end as `interrupted`** and their Job returns
to pending. The Job's worktree and branch are preserved, so the next Run
continues in place rather than starting from the Project's base branch.

## Consequences

- Requiring AC power means Coding Owl never quietly drains a battery, at the
  cost of never running on a train.
- The freeze step is what makes the product tolerable to live with. Without it,
  sitting down at the laptop means either waiting out a Run that may have
  another half hour in it, or paying a resume cycle for every ten-second visit.
- The escalation step is what makes it durable. A frozen process holds dead
  sockets, and holds them across a lid close; ending the Run cleanly within
  fifteen minutes keeps that window short.
- Signals go to the **process group**, not the Agent's PID. Claude Code spawns
  children - test runners, compilers, package managers - and freezing only the
  parent would leave those pegging the CPU, defeating the entire point.
- Two mechanisms live in one code path, and the grace window is a new tunable.
  That is the price of the above and it is worth it.
- `SIGTERM` can land mid-tool-call. Commits and the handoff document cover what
  matters, and the Job's worktree is a throwaway branch, so the blast radius is a
  possibly-untidy working tree that the next Run inherits and can see in
  `git status`.
- "Configurable" means an idle policy needs a config schema in the first
  release, not retrofitted.
- Freeze-on-return applies to every Run in flight, not just one, now that
  ADR-0021 permits several.

## Alternatives considered

**Idle time only, ignoring power state.** Simpler and runs more often, but a
laptop on battery does unattended compile-heavy work until it dies.

**Run only when the screen is locked or asleep.** A much stronger signal that
the user has actually left, but it misses the common case of stepping away
without locking.

**Let the Run finish; idle gates only whether a new Run starts.** The simplest
rule, and the one the original phrasing of this ADR implied. Rejected because a
Run has no predictable length, so returning to the machine can mean sharing it
with a compile for an unbounded time.

**`SIGTERM` immediately on every return.** One mechanism, no grace timer, no
frozen processes. Rejected because it pays a full stop-and-resume cycle every
time the user touches the laptop, however briefly.

**Cooperative stop by driving the session turn by turn** over
`--input-format stream-json`, so that "between turns" becomes a real boundary.
Rejected for the MVP: it only helps if Jobs are naturally multi-turn, and it
makes the prompt something Owl has to decompose rather than hand over.
