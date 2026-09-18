# AGENTS.md

## Development

Every change is developed and verified the same way, whether a person is
watching or not. `docs/agents/queue-worker.md` is the unattended version of
this workflow; with a person present, ask when the issue is ambiguous instead
of picking a default.

### Workflow

1. **Issue.** Work starts from a GitHub issue. Read it with
   `gh issue view <n> --comments`, then `CONTEXT.md` and the ADRs in
   `docs/adr/` it touches, and use their terms.
2. **Branch or worktree.** Cut one short-lived branch per issue from an
   up-to-date `main`: `feat/issue-<n>-<slug>`, or `fix/issue-<n>-<slug>` for
   bugs. For work alongside another checkout, use a git worktree under
   `.claude/worktrees/` instead. Never branch from another feature branch or
   stack branches, and never commit to `main` directly.
3. **Spec, implement, verify.** Follow the `bdd` skill (below).
4. **Pull request.** Push the branch and open a PR against `main` with
   `Closes #<n>` in the body, what changed, the verification verdicts, and any
   choice the issue left open with the reason for it.
5. **CI.** Wait for it with `gh pr checks --watch --fail-fast`. On a failure,
   fix it on the branch, push, and wait again. The work is done when CI is
   green.
6. **Merge.** The user decides whether and when a PR is merged; do not merge
   it unless they say so. The repo allows rebase merges only
   (`gh pr merge --rebase --delete-branch`). The queue worker is the exception:
   it merges its own PRs.

Commits follow Conventional Commits and are signed off (`--signoff`). Commit
in small steps. Never force-push or rewrite `main`.

### Spec, implement, verify

The `bdd` skill runs this for an issue; `/bdd <n>` starts it.

- **Behavior spec sheet first.** Before any code, write
  `tests/behavior/issue-<n>.md`: Given/When/Then scenarios `S<k>` covering the
  happy path, the edge cases the issue names and the failures a user would
  hit, each observable from outside (CLI output, exit code, file on disk, API
  response). Then one failing test per scenario, `TestS<k>...` in
  `tests/behavior/issue<n>_test.go`. Commit the sheet and the red tests
  together. The sheet is the contract: never weaken it to make a test pass.
- **Implement with TDD.** The `tdd` skill drives the implementation: red to
  green, one vertical slice at a time, until every behavior test passes. Then
  `make test` and `make lint`, which CI runs too.
- **Self-review.** Re-read the issue and `git diff main...HEAD`: every
  requirement has a scenario, every scenario a passing test, nothing out of
  scope changed.
- **Verification agents.** Before opening the PR, run the three review agents
  in `.agents/agents/` in parallel and fix findings until all return `PASS`:
  - `security-reviewer` - secrets, file access outside allowed directories,
    user data, shell injection, unrequested network access, widened
    permissions.
  - `correctness-reviewer` - every requirement of the issue met, tests that
    can fail, error paths handled, `CONTEXT.md` terms and ADRs respected.
  - `behavior-verifier` - every scenario on the sheet tested, passing and
    confirmed by hand, and the sheet not weakened since its first commit.

  Each writes its verdict to a fresh per-round directory outside the repo;
  `.agents/skills/bdd/scripts/wait-verdicts.sh` waits for them in one call. A
  `PASS` counts only for the commit it saw, so re-run the agents after any
  change.

## Agent skills

### Issue tracker

Issues live as GitHub issues in `vojtechmares/coding-owl`, via the `gh` CLI.
See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical vocabulary, unchanged: `needs-triage`, `needs-info`,
`ready-for-agent`, `ready-for-human`, `wontfix`.
See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root.
See `docs/agents/domain.md`.

### Queueing issues for Owl

`scripts/queue-ready-for-agent.sh` copies `ready-for-agent` issues into the
Coding Owl queue, one Job per issue, with a prompt that checks the issue for
blockers first and leaves a blocked one to be queued again later.
See `docs/agents/queue-from-github.md`.
