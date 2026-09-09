# ADR-0011: Idle policy and interruption semantics

- **Status:** Proposed - the behaviour is accepted, the pause *mechanism* below
  is not yet ratified
- **Date:** 2026-09-09

## Context

The product premise is doing work while the machine is otherwise idle, which
makes two questions load-bearing rather than incidental: what counts as idle,
and what happens when the user comes back while a job is running.

Host execution (ADR-0006) sharpens the first one. There is no VM to blame - the
agent burns real CPU on the user's laptop, so an idle policy that ignores power
state will happily flatten a battery in a bag.

## Decision

**Idle policy is configurable**, with the default being: no keyboard or mouse
input for **10 minutes** *and* the machine is **on AC power**.

**Manual override exists in both directions.** `owl run` starts work regardless
of idle state; `owl pause` stops it regardless.

**On user return, the daemon drains gracefully** - the running job finishes its
current step and no new jobs start. An explicit `owl pause` is the escape
hatch for users who do not want to wait for that.

**Concurrency is one job at a time** in the MVP.

**On daemon restart, in-flight jobs are marked interrupted and requeued**, with
their worktree preserved for inspection.

### Proposed mechanism, pending ratification

Immediate pause is `SIGSTOP` to the agent process, resumed with `SIGCONT`.

## Consequences

- Requiring AC power means Coding Owl never quietly drains a battery, at the
  cost of never running on a train.
- `SIGSTOP` buys instant pause with no session-state machinery at all, and
  because the process stays alive it naturally spans several idle windows.
  Its limit is that it does not survive a daemon restart or a reboot.
- Durable mid-job resume across restarts would need Claude Code's own session
  resume. Deferred; the requeue-on-restart rule above is the stand-in.
- "Configurable" means a config schema for the idle policy is needed in the
  first release, not retrofitted.
- Concurrency of one means ADR-0007's worktree-per-job design is not yet
  exercised for parallelism, though it is built to allow it.

## Alternatives considered

**Idle time only, ignoring power state.** Simpler and runs more often, but a
laptop on battery does unattended compile-heavy work until it dies.

**Run only when the screen is locked or asleep.** A much stronger signal that
the user has actually left, but it misses the common case of stepping away
without locking.

**Kill the agent immediately on user return.** Frees the machine fastest, but
leaves half-applied edits and a dirty worktree.

**Full pause/resume via session serialisation.** The nicest behaviour, and real
work. Deferred rather than rejected.

**Manual trigger only for the MVP.** Would prove the queue and worktree loop
sooner, but it postpones the one feature that defines the product.
