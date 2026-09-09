# ADR-0019: Accounts are first-class, with isolated tool configuration

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

One Claude subscription has one set of rate limits. Running Owl overnight on the
same account the user works on interactively means the two compete, and the user
loses - they discover it by sitting down to an exhausted account.

Multiple accounts solve that, but only if Owl can drive them independently and
in parallel. Claude Code supports this: `CLAUDE_CONFIG_DIR` relocates its entire
configuration and credential state, and `claude setup-token` issues a long-lived
token for a subscription.

There also has to be *something* for a rate-limit budget to attach to
(ADR-0020), and it has to be the thing the limit actually applies to.

## Decision

**Account** is a first-class entity: a name, the Driver it belongs to, an
Owl-owned configuration directory, a credential reference, its limit ceilings,
and an optional parallelism cap.

```
~/.local/share/coding-owl/accounts/<name>/    → CLAUDE_CONFIG_DIR
```

Owl sets the Driver's configuration-directory environment per Run, so Accounts
never collide. Credentials are referenced, not stored in `owl.db` - the OS
keychain holds the secret.

An Account also carries a `failover_allowed` flag. It is recorded and unused:
failover to another Account, Driver or model is out of MVP scope.

## Consequences

- Rate-limit budgets attach where the limit exists (ADR-0020).
- Accounts run in parallel safely, which is what makes ADR-0021's concurrency
  worth having.
- Owl owns the configuration directories, so an Owl Account is not the user's
  own `~/.claude` and cannot disturb their interactive setup.
- Account setup is a real onboarding step - `owl account add` has to walk
  through `setup-token` - and it is the first place a new user can get stuck.
- Secrets in the keychain mean the daemon needs keychain access at startup,
  which on macOS is a prompt the user must approve once.
- The configuration-directory environment variable is Claude-specific, so it
  belongs behind the Driver (ADR-0018), not in the scheduler.

## Alternatives considered

**Account plus Profile**, splitting subscription identity from invocation
settings so several Profiles can share one Account. More precise, and the only
correct model if two configurations ever draw on one subscription. Deferred: if
that becomes real, limits stay on the Account and Profiles gain a foreign key.

**Environment passthrough only** - Job config names the variables, Owl models
nothing. Rejected because it leaves nothing for a limit budget to attach to,
which forfeits ADR-0020 entirely.
