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

**Run**:
One attempt to carry out a Job. A Job may take several, because a Run can end
before its Job is finished. A Run is not a Session - several Runs share one.
_Avoid_: execution, attempt, invocation

**Session**:
The conversation an Agent holds, persisted by its tool and reused across every
Run of a Job, so work continues where it stopped. Only Drivers that declare the
capability have one.
_Avoid_: context, history, thread

**Idle**:
The state in which Coding Owl is allowed to work: by default no keyboard or
mouse input for ten minutes, and the machine on AC power. Configurable.
_Avoid_: away, inactive, asleep, free

**Verification**:
The check Owl runs after an Agent exits, deciding whether a Run's work is
acceptable. A Job is only done once it passes.
_Avoid_: validation, QA, review

**Garbage collection**:
The daemon's periodic reconciliation of worktrees on disk against Jobs in the
database - reclaiming what is finished and reporting what looks unfinished.
_Avoid_: cleanup, pruning, sweeping

### Capacity

**Account**:
A subscription Owl can run Agents as, with its own isolated tool configuration,
its own credential, and its own rate limits.
_Avoid_: profile, identity, login

**Ceiling**:
The share of an Account's rate-limit window that Owl will not schedule past,
measured against the account's total usage rather than Owl's alone.
_Avoid_: quota, budget, cap, allowance

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
