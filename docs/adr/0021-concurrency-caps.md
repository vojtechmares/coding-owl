# ADR-0021: Concurrency is capped globally, per Project and per Account

- **Status:** Accepted
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
| `global.max_parallel_runs` | 1 | Opt-in. Nothing runs in parallel until asked. |
| `project.max_parallel_runs` | 1 | Same-Project Runs collide on ports, fixtures and caches. |
| `account.max_parallel` | unlimited | Burn rate is already governed by ADR-0020's ceiling. |

Raising the global alone therefore parallelises *across* Projects, never within
one. Per-Project is raised deliberately, once a Project's checks are known to be
hermetic.

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
