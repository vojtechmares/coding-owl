# ADR-0038: A Shift lets Owl work on an Account while the machine is in use

- **Status:** Accepted
- **Date:** 2026-09-19
- **Amends:** ADR-0011's manual override clause

## Context

Owl works while the machine is Idle (ADR-0011). That turned out to leave queued
work waiting through whole afternoons at a desk, when the machine had room for
it and the person at it wanted it done.

ADR-0011 did name a manual override, `owl start`, but it starts one Run for the
Job at the head of the queue and nothing after it. There is no way to say "keep
working on this Account for the next two hours", and the Idle watcher freezes
the Runs it started as soon as anyone touches the keyboard.

## Decision

A **Shift** is a bounded period during which Owl starts Runs on one Account
whether or not the machine is Idle. At most one Shift is on per Account.

### Starting and ending

`owl shift start|stop|status`. `start` covers every Account unless given
`-a/--account`, and takes:

| Flag | Default | Meaning |
| --- | --- | --- |
| `-f/--for` | 1h | how long the Shift starts Runs for |
| `-u/--until-empty` | off | also end once the Account's queue is empty |
| `--require-power` | off | start nothing while on battery |
| `--replace` | off | replace a Shift already on, rather than extend it |

**A Shift is always time-bound.** `--until-empty` combines with `--for` rather
than replacing it, so a queue of a thousand Jobs does not become a Shift with
no end. Without `--until-empty`, an empty queue leaves the Shift on and waiting
for work, and the desktop app says so: queue empty, time left.

**Empty** means the Account has no `pending` and no `active` Jobs. A Job held
back by the Ceiling or a cap is pending, and a Run still going may yet put its
Job back in the queue, so neither is empty. `blocked`, `exhausted`, `review`,
`done` and `cancelled` do not count: none runs again without a person.

**Starting a Shift on an Account that has one extends it.** Its end moves to
the later of the two, and nothing else changes. `--replace` replaces it
outright, which is how one is shortened or given other flags.

**When a Shift ends, its Runs are let finish.** Its end is a limit on starting
Runs, not a deadline for them. That holds whether it ran out of time, found its
queue empty, or was ended by `owl shift stop`.

### While a Shift is on

- **Returning to the machine freezes nothing on that Account**, whoever started
  the Run. A Run the Idle watcher started at 02:00 is covered by a Shift started
  at 09:00, and a Run a Shift covered is let finish after it ends rather than
  frozen.
- **The Ceiling applies** (ADR-0020). A Shift at its Ceiling starts nothing and
  says until when, and starts work after the reset if it has time left.
- **Caps apply** (ADR-0021). A Shift permits work; it does not widen it.
- **Power is ignored** unless `--require-power` was given.

### The one-off verbs

`owl start`, `pause`, `resume` and `stop` keep acting on Runs, and take no Shift
flags:

- `owl start` starts one Run for the Job at the head of the queue, as before.
- `owl pause` freezes every Run in flight **and holds every Shift** until
  `owl resume`. A held Shift starts nothing and its clock keeps running.
  Without the hold, frozen Runs that outlast the grace window end, free their
  slots, and the Shift starts the next Job in the middle of whatever the pause
  was for.
- `owl stop [account]` ends Runs in flight: they carry on for up to the grace
  window, then are signalled as ADR-0034 describes. `--force` signals them at
  once. Shifts are left on, so the next Job starts; `--end-shift` ends them
  too.

### Persistence

A Shift and the pause hold are stored in the database, the Shift with an
absolute end. Both survive a daemon restart. A Shift whose end passed while the
daemon was down is over.

## Consequences

- A one-hour Shift can keep the machine busy for noticeably longer than an
  hour, because its Runs finish. `owl shift status` and the desktop app have to
  say "Shift ended, 1 Run finishing", or it looks like the end was ignored.
- `owl stop` during a Shift stops *this* work, not work. That is deliberate and
  needs saying in its help: `owl pause` and `--end-shift` are how the machine
  is given back.
- A pause now outlives its grace window and a restart. A person who pauses and
  forgets finds nothing done until they resume, which `owl status` must make
  obvious.
- ADR-0011's freeze-on-return now asks whose Account a Run is on before it
  freezes it.
- Shift schedules, which start Shifts on a timetable, build on this: a Shift is
  already stored, bounded and started by something other than Idle. They are a
  separate decision.
- There is no way past the Ceiling. A deliberate, one-off boost is left for its
  own decision.

## Alternatives considered

**Stop or freeze Runs at the Shift's end.** An exact end, at the cost of a Run's
tail and a TTL attempt (ADR-0025), or of freezing an Agent the person just
watched working. Rejected: the person is present and asked for the work.

**Default to until-empty.** The natural end of "do my queue", and unbounded when
the queue is long. Rejected for a time bound that is always there.

**Replace a Shift already on.** The latest command as the latest intent. Kept
behind `--replace`, because a second `owl shift start` that shortens a Shift is
a surprise and one that extends it is not.

**Pause freezes Runs only**, leaving Shifts alone. Simpler, and a pause then
lasts only as long as the grace window.

**Shifts held in memory.** Simpler, and an upgrade in the middle of an
afternoon Shift ends it without a word.

**Shift flags on `owl start`.** One verb family for everything, and a one-off
start that is suddenly a period of work. Rejected: `owl start` means one Job.
