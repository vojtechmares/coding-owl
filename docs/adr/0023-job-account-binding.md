# ADR-0023: A Job runs on its Project's Account

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Accounts (ADR-0019) isolate tool configuration by relocating it with
`CLAUDE_CONFIG_DIR`. Claude Code stores session transcripts *inside* that tree,
at `~/.claude/projects/<slugified-cwd>/<session-id>.jsonl`.

So a Session belongs to exactly one Account. Since a Job carries its Session
across nights (ADR-0012), a Job that changed Account between Runs would find
`--resume` unable to locate its conversation. Binding is therefore not a
preference, it is a constraint - the only question is where the binding is
declared.

Separately, not all work should draw on the same subscription. Client work kept
on a designated account is a billing and policy matter, not an optimisation.

## Decision

A Project names its Account in configuration, and every Job in that Project
inherits it for its whole life.

```yaml
account: work
```

## Consequences

- Which subscription a repository's work draws on is knowable by reading its
  config, which is what makes client separation enforceable rather than
  incidental.
- `--resume` always resolves, because a Job never changes Account.
- **Capacity does not pool.** A Project whose Account sits at its ceiling
  (ADR-0020) waits all night while another Account has headroom going spare.
  This is the accepted cost.
- `owl status` must therefore distinguish "idle, nothing queued" from "queued,
  but this Project's Account is at its ceiling until 04:00". Without that the
  daemon looks broken on a night it is behaving correctly.
- Adding an Account does nothing for existing Projects until they are
  reconfigured to use it.

## Alternatives considered

**Auto-assign on first Run, then pin.** The scheduler picks whichever eligible
Account has the most headroom and records it. Pools capacity and never asks the
user to think about accounts when queueing. Rejected because which subscription
a Job ran on becomes incidental, and client separation would need enforcing some
other way.

**Chosen per Job at enqueue.** Maximum control at the moment of most knowledge.
Rejected as friction on the single most frequent action.

**A list of permitted Accounts per Project**, choosing on first Run and pinning
after - preserving both separation and pooling. Deferred; the natural fix if the
lack of pooling turns out to bite.
