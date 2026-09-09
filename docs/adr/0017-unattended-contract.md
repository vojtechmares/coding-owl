# ADR-0017: Owl appends a standing unattended contract to every Run

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Interactive Claude Code asks when it is unsure. Unattended it cannot: Owl runs
Agents with `--permission-prompts none` (ADR-0012), so anything that would
prompt is denied silently, and there is nobody awake to answer a clarifying
question anyway.

An Agent that meets ambiguity at 2am therefore either guesses, or stops having
done almost nothing. The user finds out at 9am either way, and only one of those
outcomes is recoverable.

## Decision

Every Run carries Owl's standing **unattended contract**, injected by the
Driver in whatever way its tool supports - `--append-system-prompt` for
`claude-code` (ADR-0018):

- you are running unattended; nobody can answer a question
- make reasonable assumptions rather than stalling, and write them down
- commit incrementally, with messages that explain the reasoning
- when genuinely blocked, stop and state the blocker plainly
- never guess at anything destructive or irreversible

A Project may append its own clauses through `.coding-owl.yaml`. `owl jobs show
<id>` prints the effective system prompt, so nothing Owl injects is hidden.

## Consequences

- Every Job inherits sane unattended behaviour without the user restating it,
  and the night they forget to is no longer the night that is wasted.
- "Commit as you go" is load-bearing elsewhere: it is what makes ADR-0011's
  `SIGTERM` escalation cheap, and what lets ADR-0015's garbage collection tell
  finished work from stray uncommitted changes.
- "Record your assumptions" gives the morning review something specific to
  check, which is most of the difference between a diff you can trust and one
  you have to re-derive.
- Owl now injects text the user did not write, making it a hidden variable in
  every Job's behaviour. That is mitigated by printing it on demand and by
  keeping it short and stable - it is a contract, not a style guide.
- Changing the contract changes behaviour across every Project at once, so it
  is versioned with the config `apiVersion` (ADR-0014) and changed deliberately.
- A per-Project append can weaken the contract. Because Project config is read
  from the base branch (ADR-0014), an Agent cannot weaken the contract it is
  itself running under.

## Alternatives considered

**Inject nothing; the Job prompt is the whole contract.** The most transparent
arrangement possible - an Agent that misbehaves does so because of text the user
can read. Rejected because it pushes a fixed checklist into every prompt by
hand, and the failure mode of forgetting is an entire wasted night.

**Per-Project preambles only, with no global default.** Rules live next to the
code they govern and are version-controlled with it, which fits ADR-0014 neatly.
Rejected because a freshly registered Project would have no guardrails at all,
which is precisely when they matter most.
