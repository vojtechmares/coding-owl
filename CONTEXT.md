# Coding Owl

Coding Owl runs coding agents on your machine while it is otherwise idle, so
that work you queued up happens while you are away and is waiting for review
when you come back.

## Language

### The work

**Project**:
A git repository registered with Coding Owl, against which Jobs are queued. It
carries its own base branch, branch prefix and Verification checks.
_Avoid_: workspace, codebase, target

**Job**:
A standing intent to do one piece of work in one Project: the prompt, its
configuration, and its lifecycle.
_Avoid_: task, ticket, item, request

**Label**:
A name a Job carries saying what kind of work it is. A Job may carry any
number, each once, and they are the user's to invent. A Label narrows what is
listed and nothing else: the scheduler does not read one, so it is not a way
to make a Job urgent - that is the queue's order (ADR-0025).
_Avoid_: tag, category, priority, tier

**Focus**:
A Project marked to go first on its Account; each Account has at most one. Its
runnable Jobs are scheduled ahead of other Projects' Jobs, and the rest of the
queue runs whenever it has nothing runnable. Standing until cleared, not
bounded by time.
_Avoid_: priority, fast lane, pin, filter

**Run**:
One attempt to carry out a Job. A Job may take several, because a Run can end
before its Job is finished. Each Run holds a Session of its own, and what
carries between them is the Handoff (ADR-0026).
_Avoid_: execution, attempt, invocation

**Session**:
The conversation a single Run holds. It does not outlive that Run - what carries
between Runs is the Handoff.
_Avoid_: context, history, thread

**Plan**:
What a Job's first Run decides it will do. It becomes the execution prompt and
the first Handoff, and is never continued conversationally.
_Avoid_: spec, design, proposal

**Handoff**:
The document on a Job's branch carrying intent and progress from one Run to the
next, since no conversation survives between them.
_Avoid_: notes, state, scratchpad, summary

**Idle**:
One of the two states in which Coding Owl is allowed to work, the other being a
Shift: by default no keyboard or mouse input for ten minutes, and the machine
on AC power. Configurable.
_Avoid_: away, inactive, asleep, free

**Shift**:
A bounded period during which Owl starts Runs on one Account whether or not the
machine is Idle; Runs it started are let finish after it ends. It ends when its time runs out, or, if asked to, when the
Account's queue is empty; otherwise an empty queue leaves it on and waiting for
work.
_Avoid_: override, sprint, session, burst

**Shift schedule**:
A standing definition that Shifts are started from: which Account, for how
long, and when. A Shift schedule is created and deleted; a Shift is started and
ended. Not yet built.
_Avoid_: shift (for the definition), rota, preset

**Verification**:
The check Owl runs after an Agent exits, deciding whether a Run's work is
acceptable. A Job is only done once it passes.
_Avoid_: validation, QA, review

**Garbage collection**:
The daemon's periodic reconciliation of worktrees on disk against Jobs in the
database - reclaiming what is finished and reporting what looks unfinished.
_Avoid_: cleanup, pruning, sweeping

**Skill**:
A reusable bundle of instructions or tools that Owl installs for an Agent,
declared per Project and pinned to a version.
_Avoid_: plugin, extension, pack

### Capacity

**Account**:
A subscription Owl can run Agents as, with its own isolated tool configuration,
its own credential, and its own rate limits.
_Avoid_: profile, identity, login

**Ceiling**:
The share of an Account's rate-limit window that Owl will not schedule past,
measured against the account's total usage rather than Owl's alone.
_Avoid_: quota, budget, cap, allowance

**Standing instructions**:
Text an Account carries that every Run on it reads, whichever Project the Run
is for. Kept in the Account's configuration directory as the file that
Account's coding tool reads instructions from, so the tool reads them itself
and Owl injects nothing (ADR-0037). Distinct from a Project's unattended
clauses, which hold for that Project alone.
_Avoid_: memory, profile, preferences, prompt, rules

### Doing the work

**Agent**:
A running instance of a coding tool, doing work on the user's behalf. Claude
Code is the first, not the only one.
_Avoid_: bot, instance, assistant, worker

**Driver**:
A plugin that knows how to operate one coding tool: how to build its command,
read its output, resume its Session, and what it is capable of.
_Avoid_: backend, adapter, provider, tool

**Executor**:
Where an Agent runs - as a host process now, in a container later. Orthogonal
to the Driver: any Driver composes with any Executor.
_Avoid_: runner, launcher, sandbox

**Verifier**:
A plugin that performs Verification - by running a Project's own checks, or by
having a fresh Agent review the diff.
_Avoid_: checker, validator, reviewer

**Runner**:
A machine that registers with a daemon and executes work on it, rather than on
the user's own computer. Not yet built.
_Avoid_: agent, node, worker

> **Note on `AGENTS.md` and the `ready-for-agent` label.** Those refer to AI
> agents working *on* this repository, not to the Agents this project runs.
> They follow a cross-project filename convention and are deliberately left
> alone; they are not part of this glossary's language.
