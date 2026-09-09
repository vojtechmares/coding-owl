# ADR-0020: Scheduling respects a per-Account utilization ceiling

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

The point of running work overnight is that the machine is idle. The point is
emphatically *not* to arrive in the morning to an account with no capacity left
for the user's own work.

Claude Code reports limit state in structured form: a `five_hour` window and a
`seven_day` window - the latter split into `seven_day_opus` and
`seven_day_sonnet` - each carrying a `utilization` figure and a `resets_at`
timestamp. Crucially, utilization is **account-wide**: it already includes the
user's interactive sessions.

There is no on-disk cache of it. The numbers arrive in the Run's own output
stream, so Owl learns them by running, not by asking.

## Decision

Each Account configures a **ceiling** on total utilization per window:

```yaml
limits:
  five_hour_max: 60%
  weekly_max:    50%
```

Owl records utilization and `resets_at` from every Run's stream. Before starting
a Run it checks the last observed figure, discarding it if the window has since
reset. Above the ceiling, the Account is not scheduled.

Because Owl owns the process (ADR-0012), it also watches the live stream and
**terminates a Run that crosses the ceiling mid-flight** - outcome
`interrupted`, resumed after `resets_at`.

## Consequences

- Owl yields to the user automatically. If the user burned 70% themselves, Owl
  stands down without being told, because the ceiling is on the total rather
  than on Owl's own share.
- The observed figure is stale between Runs. It errs optimistically - the user
  may have spent more since - which is why the ceiling should sit meaningfully
  below 100% rather than at 95%.
- A Driver without `usage_reporting` (ADR-0018) cannot be scheduled under a
  ceiling at all, only reactively against rate-limit errors.
- Per-tier weekly windows mean the ceiling may need to be per-tier too. The MVP
  takes the *most* utilized applicable window and compares that, which is
  conservative and wrong in nobody's favour.
- Mid-Run termination costs the tail of that Run's work. ADR-0017's "commit as
  you go" is what keeps that cheap.
- `owl status` has to explain *why* nothing is running, or the daemon looks
  broken on a night when it is behaving correctly.

## Alternatives considered

**Cap Owl's own consumption** rather than the account total - "Owl may use 30%
of the week". Cleanly attributable, and unaffected by what the user does. It
reserves nothing, though: the user burning 80% still leaves Owl free to spend
its untouched 30% and exhaust the account.

**Both a total ceiling and an Owl-share cap**, whichever binds first. The most
expressive, and where this may end up. Deferred: four numbers per Account is a
lot to tune before there is any operating experience.

**Purely reactive** - run until a rate-limit error, then back off until
`resets_at`. Zero configuration and maximum throughput, and it guarantees the
user finds the wall the hard way.
