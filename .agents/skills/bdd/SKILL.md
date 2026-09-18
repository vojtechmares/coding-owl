---
name: bdd
description: Behavior-driven development for a Coding Owl GitHub issue - writes the behavior spec sheet tests/behavior/issue-<n>.md and one failing test per scenario, implements the feature with the tdd skill until they pass, then verifies it with the behavior-verifier, security-reviewer and correctness-reviewer agents. Use when the user wants to build an issue spec-first, mentions BDD, Given/When/Then scenarios or a behavior spec sheet, or asks to implement an issue the way the queue worker does.
argument-hint: "<issue number>"
---

# Behavior-driven development

The spec sheet is the contract: it is written before any code, the feature is
built until it holds, and independent agents check that it does. This is
steps 3 to 6 of `docs/agents/queue-worker.md` with a person present, so ask
when the issue is ambiguous instead of guessing.

Arguments: $ARGUMENTS

## Before you start

- You need an issue number `<n>`. If there is none, ask for one; a feature
  without an issue can be filed first with the `triage` or `to-issues` skill.
- Read it with `gh issue view <n> --comments`, then `CONTEXT.md` and the ADRs in
  `docs/adr/` it touches. Use their terms in the sheet and in test names.
- Work on a `feat/issue-<n>-<slug>` or `fix/issue-<n>-<slug>` branch cut from an
  up-to-date `main`, unless you are already on one.

## 1. Write the spec sheet

Create `tests/behavior/issue-<n>.md`:

```markdown
# Issue #<n>: <title>

<Prose: what changes, what is out of scope, shared setup and terms the
scenarios rely on (fixtures, stub agent, XDG layout).>

## Scenarios
### S1 - <name>
Given <precondition>
When <action>
Then <observable outcome>
```

- Cover the happy path, every edge case the issue names, and the failures a
  user would hit.
- Every Then must be observable from outside: CLI output, exit code, a file on
  disk, an API response. Never an internal function or struct.
- Reuse shared setup from earlier sheets by reference (for example "the XDG
  layout and stub agent of `tests/behavior/issue-5.md`").
- Show the scenario list to the user and settle open questions before writing
  tests. After the sheet is committed, never weaken it to make a test pass;
  the verifier diffs it against its first commit.

## 2. Write the failing behavior tests

- One test per scenario in `tests/behavior/issue<n>_test.go`, package
  `behavior_test`, named `TestS<k><WhatItChecks>` so each maps to `S<k>`.
  Open with the comment the other files use: `// Behavior tests for issue #<n>.
  Each TestS<n> maps to scenario S<n> in tests/behavior/issue-<n>.md.`
- Drive the built `owl` binary or the daemon API as the existing files do; use
  `tests/behavior/fakeclaude`, never real Claude Code.
- Assert the whole Then clause, from literals in the sheet, not values
  recomputed the way the code will.
- Run `go test ./tests/behavior/ -run '<your test names>'`. Every test must fail
  for the right reason: the behavior is missing, not a compile error or broken
  setup.
- Commit the sheet and the red tests together, e.g.
  `test(behavior): spec sheet and failing tests for issue #<n>`, with `--signoff`.

## 3. Implement with TDD

Invoke the `tdd` skill and follow it. The behavior tests are the acceptance
target; the red-green loop runs underneath them with unit tests at the seams you
agree on, one vertical slice at a time. Take scenarios one by one until every
behavior test passes. Then run `make test` and `make lint`, and commit in small
Conventional Commits with `--signoff`.

## 4. Verify with the review agents

Re-read the issue and `git diff main...HEAD`: every requirement has a scenario,
every scenario a passing test, nothing out of scope changed. Then:

```sh
VERDICTS="${TMPDIR:-/tmp}/owl-verify/issue-<n>/round-<r>"
rm -rf "$VERDICTS" && mkdir -p "$VERDICTS"
```

Launch `behavior-verifier`, `security-reviewer` and `correctness-reviewer` in
parallel with the Agent tool, by `subagent_type`, giving each the issue number,
branch, spec sheet path and `$VERDICTS`. Then wait for them in the same turn:

```sh
.agents/skills/bdd/scripts/wait-verdicts.sh "$VERDICTS" behavior security correctness
```

On `FAIL`, fix every finding in the code or tests, never the sheet, and run
a new round with `r` incremented. A `PASS` counts only for the commit it saw:
after any change, run all three again. On `TIMEOUT`, run the wait again for
the names it lists.

Done means all three returned `PASS` on the current commit. Carry on with the
pull request and CI steps of the workflow in `AGENTS.md`; merging is the
user's call.
