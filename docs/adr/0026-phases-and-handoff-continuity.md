# ADR-0026: Plan then execute, each Run in a fresh context, continuity by handoff document

- **Status:** Accepted
- **Date:** 2026-09-09
- **Supersedes:** the conversation-reuse half of ADR-0012

## Context

ADR-0012 made Claude Code's Session the continuity mechanism: a Job stored one
`session_id` and every later Run continued it with `--resume`. That worked, and
it bound Owl tightly to one tool's session store - which sits badly with
ADR-0018's goal of driving opencode, pi and others.

Separately, a Job needs thinking through before it is worked. A vague prompt
costs a whole night and returns a branch that misunderstood the task.

## Decision

### Phases

A Job is **planned before it is executed**. Planning is the default;
`--plan` and `--no-plan` are mutually exclusive flags on `owl add`.

The planning Run produces a plan. That plan becomes the execution prompt and the
initial **handoff document** - it is not continued conversationally. Execution
always begins in a **new context**.

### Continuity

**Every Run starts fresh.** There is no `--resume` anywhere. A Run orients from:

1. `.coding-owl/HANDOFF.md`, committed on the Job's branch
2. the branch's git log and working tree

and keeps that document current *as it goes*, not at the end.

The handoff document is committed rather than kept in Owl's state directory,
because a Run usually ends by `SIGTERM` (ADR-0011): a commit is atomic where a
half-written file is not. It also survives worktree reclamation (ADR-0015) and
is readable and editable by the user.

## Consequences

- Continuity no longer depends on any tool's session store, so ADR-0018's
  `resumable_session` capability is not needed and Drivers without one are
  first-class.
- ADR-0023 pins a Job to its Project's Account. Its *session-locality*
  justification is now void; the decision stands on client separation alone.
- The handoff document is a genuine review artifact. Reading it beside the diff
  answers "did it understand the task", which no amount of commit archaeology
  does as well.
- The user can steer a Job between nights by editing `HANDOFF.md` on the branch.
  That is a feature, and it is also a way to confuse an Agent.
- **Re-orientation costs tokens every night**, and it is real: reading a handoff
  and a git log is not free, and it counts against the ceiling in ADR-0020.
- Nuance is lost. Night three no longer remembers why night one abandoned an
  approach unless the handoff says so, which puts real weight on ADR-0017's
  contract to record assumptions and decisions.
- Planning costs an extra Run, and under ADR-0025 that Run decrements the Job's
  TTL like any other.
- `.coding-owl/HANDOFF.md` appears in the review diff. That is accepted noise.

## Alternatives considered

**New context only between phases** - planning gets its own Session, execution
resumes one across nights, as ADR-0012 described. Keeps full conversational
detail within the work phase, which no handoff document realistically matches.
Rejected because it keeps continuity tied to a tool that can resume.

**Resume when possible, handoff as fallback.** Best available fidelity, degrading
rather than breaking. Rejected because it means two continuity paths where the
fallback is the rarely-exercised one, and rarely-exercised paths are where the
bugs live.

**Keeping the handoff outside the repository**, in Owl's state directory and
exposed with `--add-dir`. Keeps the review diff purely about the work. Rejected
for the atomicity argument above.
