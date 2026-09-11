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

An Account is always a **subscription** in the MVP - authenticated with
`claude setup-token`, and governed solely by the utilization ceiling of
ADR-0020. API-key Accounts, whose scarcity is money rather than rate and whose
control would be `--max-budget-usd` and a rolling cap, are a second `kind` for
later. The desktop chat's Anthropic and OpenRouter keys (ADR-0022) are model
credentials, not Accounts, and stay in separate configuration.

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

### Where the secret goes when there is no keychain

Owl is a macOS program (ADR-0002, ADR-0010), and on macOS the keychain is where
an Account's secret lives. Everywhere else - a Linux continuous integration
runner, a developer running the test suite - there is no keychain Owl drives,
and it keeps the secret in a file under the data home that only its owner can
read, saying so in its log at startup.

The daemon's own configuration chooses with `credentialStore: keychain` or
`credentialStore: file`, defaulting to the keychain where there is one. The
knob exists because the behaviour suite must be able to run without putting a
test token in anybody's real keychain, and because a user running the daemon
somewhere without one deserves a working Owl rather than a refusal. A file is
weaker than a keychain and is not the default anywhere a keychain exists.

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
