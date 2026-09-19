# ADR-0021: Concurrency is capped globally, per Project and per Account

- **Status:** Accepted
- **Amended:** 2026-09-12, to write the three settings as the files spell them.
  It wrote them in snake_case, which no file Owl reads uses.
- **Amended:** 2026-09-19, to change the global and per-Account defaults. See the
  note under Decision.
- **Date:** 2026-09-09
- **Supersedes:** the "one Run at a time" clause of ADR-0011

## Context

ADR-0011 fixed concurrency at one Run. With Accounts (ADR-0019) that is
needlessly serial: two Accounts have independent limits and an idle machine can
drive both.

Parallelism is not uniformly safe, though, and the danger is specific to Runs
that share a Project. Two worktrees of one repo will each happily run
`go test ./...`, and if those tests bind a port, use a fixed test database, or
share a build cache, they corrupt each other. Two Runs in *different* Projects
have no such problem.

## Decision

Three caps; the scheduler takes the minimum that applies.

| Cap | Default | Why |
| --- | --- | --- |
| `maxParallelRuns`, daemon file | 1 | Opt-in. Nothing runs in parallel until asked. |
| `maxParallelRuns`, Project file | 1 | Same-Project Runs collide on ports, fixtures and caches. |
| `accounts.<name>.maxParallel` | unlimited | Burn rate is already governed by ADR-0020's ceiling. |

Raising the global alone therefore parallelises *across* Projects, never within
one. Per-Project is raised deliberately, once a Project's checks are known to be
hermetic.

> **Amended 2026-09-19.** The defaults of the global and per-Account caps
> change: `maxParallelRuns` in the daemon file now defaults to **2**, and
> `accounts.<name>.maxParallel` to **1**. A person runs several Accounts in
> order to run them side by side, and under the original defaults that took
> three numbers kept in sync by hand. Two Accounts now run side by side out of
> the box, one Run each, while the global cap still bounds host load: a third
> Account shares those two slots until the global cap is raised. The
> per-Project default of 1 is unchanged, so two Runs still never share a
> repository unless a Project says they may.

## Consequences

- The safe thing happens by default and the sharp thing requires a second,
  Project-specific decision - made by someone who knows whether those tests can
  run twice at once.
- "I set global to 4 and nothing changed" is a real support question when the
  user has one Project. `owl status` must say which cap is binding.
- Parallel Runs on one Account burn its window faster, and the ceiling then
  stops them all at once. That is correct but abrupt.
- ADR-0007's worktree-per-Job finally earns its keep: parallelism needs no
  further isolation work.
- Concurrency multiplies host CPU contention, which ADR-0011's freeze-on-return
  must handle for *every* running Run, not just one.

## Alternatives considered

**A single global cap with per-Project override inheriting it.** Two knobs and
one number to reason about. Rejected because raising it immediately permits
several Runs inside one repo, and the resulting corrupted test database is a
deeply unobvious failure.

**Derive concurrency from Accounts** - one Run per Account with headroom, no
number to pick. Elegant, and ties throughput to the genuinely scarce resource.
Rejected because it removes the opt-in setting entirely and stays permanently
serial for a single-account user with an idle machine.
