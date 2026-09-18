# Queue worker

Ralph loop prompt for working the `ready-for-agent` issue queue unsupervised.
Start it from the repo root:

```
/ralph-loop 'Read docs/agents/queue-worker.md and follow it exactly, from the top, every iteration.' --completion-promise 'OWL_QUEUE_DONE' --max-iterations 300
```

An iteration is one **turn ending**, not one issue. The loop's stop hook fires
every time you stop for any reason, so a turn spent waiting on something costs
exactly as much as a turn spent working. One issue is tens of iterations, which
is why the budget is in the hundreds - and why step 6 tells you to wait inside a
single turn rather than by stopping and checking again.

# Work the ready-for-agent queue

You are running inside a Ralph loop. This exact prompt is fed back to you after every exit, so every iteration must start from scratch by reading state from GitHub and git, never from memory. Work on exactly one issue per iteration, finish it completely, then exit so the loop restarts.

Repo: `vojtechmares/coding-owl`. Use the `gh` CLI for every issue operation, following `docs/agents/issue-tracker.md`. Label vocabulary is defined in `docs/agents/triage-labels.md` and must not be extended. Domain language lives in `CONTEXT.md` and decisions in `docs/adr/` - use their terms and respect their decisions.

## You are unsupervised

Nobody is watching and nobody will answer questions. You must get each task to done on your own:

- Prefer the well-known, boring, generally accepted solution. Correct and simple beats clever; we can make it fast later.
- When you do not know how to do something, use web search to find the standard approach, implement it, and continue. Do not stall.
- When the issue leaves a choice open, pick the conventional default, write the choice and the reason in the PR body under "Decisions made on my own", and move on. We can always come back to it.
- Escalate to a human only as a last resort, for things you genuinely cannot do: missing credentials or access, a requirement that contradicts an ADR, or a change with irreversible external effects. Ambiguity alone is never a reason to stop.

## Stop condition

Output `<promise>OWL_QUEUE_DONE</promise>` only when this statement is completely true:

> No open issue in the repo carries the `ready-for-agent` label.

Nothing else ends the loop. Do not output the promise because you are stuck, tired of a task, or think you should stop. If the environment itself is broken (gh not authenticated, cannot push, tests cannot run at all), record the exact error in a comment on the issue you were working on, relabel it `ready-for-human`, and exit normally - the next iteration will re-check.

## Step 0 - resume or pick

1. Run `git status`. If the working tree is dirty or you are on a branch named `feat/issue-<n>-*` or `fix/issue-<n>-*`, you are resuming issue `<n>`. Read the issue, read the diff against `main`, read the behavior spec sheet on the branch, and continue from the first unfinished step below. Do not start a second issue.
2. Otherwise fetch the queue, oldest first:

   ```sh
   gh issue list --state open --label ready-for-agent --search "sort:created-asc"
   ```

   If it is empty, output the promise and stop. Otherwise pick the first issue and run `gh issue view <n> --comments`.

## Step 1 - check the issue is workable

Read the issue body and every comment. The issue must describe expected behavior you can write tests from. Gaps and ambiguities are yours to resolve with the conventional default (see "You are unsupervised"). Take the human-in-the-loop path only when the issue is genuinely unworkable: it needs credentials or access you do not have, it contradicts an ADR, or it cannot be implemented safely without an irreversible external action.

- Comment on the issue with exactly what is needed.
- `gh issue edit <n> --remove-label ready-for-agent --add-label needs-info` (missing information) or `--add-label ready-for-human` (needs a human decision or human implementation).
- Exit. That issue has left the queue and the loop moves on.

## Step 2 - branch from main

Always start from an up-to-date `main`. One short-lived feature branch per issue, cut from `main`, merged back into `main`. Never branch from another feature branch, never stack branches, never rebase onto anything but `main`.

```sh
git checkout main && git pull --ff-only
git checkout -b feat/issue-<n>-<short-slug>   # or fix/ for bugs
```

## Step 3 - behavior spec sheet, before any code

Turn the expected behavior from the issue into a spec sheet at `tests/behavior/issue-<n>.md`, committed on the branch. Format:

```markdown
# Issue #<n>: <title>

## Scenarios
### S1 - <name>
Given <precondition>
When <action>
Then <observable outcome>
```

Cover the happy path, every edge case the issue names, and the failure modes a user would hit. Each scenario must be observable from the outside (CLI output, exit code, file on disk, API response), not an implementation detail. This sheet is the contract the behavior test agent will check against in step 6, so do not soften it later to make it pass.

Then write the behavior test: one executable test per scenario, in the normal test tree of the project, named so it maps back to the scenario id. Run it. It must fail (red). Commit the spec sheet and the failing test together.

## Step 4 - implement with TDD

Invoke the `tdd` skill and follow it strictly: red-green-refactor, smallest failing test first, no production code without a failing test that demands it. Work scenario by scenario until every behavior test passes and the unit tests you added along the way pass. Run the full test suite and the lint/format step of the project before moving on. Commit in small steps using the repo commit conventions (Conventional Commits, `--signoff`).

## Step 5 - self-review

Re-read the issue once more, top to bottom, and diff your branch against `main`. Confirm every requirement has a scenario, every scenario has a passing test, and nothing outside the scope of the issue was changed.

## Step 6 - verification agents

Launch these three agents in parallel with the Agent tool, each by its `subagent_type`. Their review process and fail criteria live in `.agents/agents/`. Give each one the issue number, the branch name, the path to the spec sheet, and the verdict directory below. Each returns a verdict of `PASS` or `FAIL` with a numbered list of findings, each with a file and line. Agents review only; they must not modify files in the repository.

1. **Security agent** - `security-reviewer`. Reviews `git diff main...HEAD` for leaked secrets, file access outside the allowed directories, touching user data, shell injection, unrequested network connections and widened permissions.
2. **Correctness agent** - `correctness-reviewer`. Checks the diff and tests against every requirement in the issue and its comments, for tautological tests, unhandled error paths, and conflicts with `CONTEXT.md` or `docs/adr/`.
3. **Behavior test agent** - `behavior-verifier`. Takes `tests/behavior/issue-<n>.md` as the source of truth, runs each scenario's test, exercises the feature by hand, and checks the sheet was not weakened since its first commit.

**Collect the verdicts through files, and wait for them inside one turn.** An
agent's result otherwise reaches you only as a notification, which arrives after
your turn ends - so polling `ListAgents` and stopping again costs an iteration
per check and drains the budget on waiting. Instead, make a fresh directory for
the round, outside the repository so nothing lands in the diff the agents are
reviewing:

```sh
VERDICTS="${TMPDIR:-/tmp}/owl-verify/issue-<n>/round-<r>"
rm -rf "$VERDICTS" && mkdir -p "$VERDICTS"
```

A fresh directory per round is what stops the wait below from returning
instantly on the previous round's verdicts. Then add this to each agent's
prompt, with its own file name:

> When you have finished, write your verdict to `$VERDICTS/<security|correctness|behavior>.md`: the first line exactly `PASS` or `FAIL`, then your numbered findings. Write it to that path with a `.tmp` suffix first and `mv` it into place, so it is never read half-written. Do this as the last thing you do, and do it even when you are reporting `FAIL` or could not finish the review.

Launch all three, then block on the files in a single foreground call:

```sh
deadline=$(( $(date +%s) + 540 ))   # 9 minutes, inside the Bash tool's 10 minute cap
until [ -f "$VERDICTS/security.md" ] && [ -f "$VERDICTS/correctness.md" ] && [ -f "$VERDICTS/behavior.md" ]; do
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "TIMEOUT - no verdict from:"
    for a in security correctness behavior; do [ -f "$VERDICTS/$a.md" ] || echo "  $a"; done
    exit 1
  fi
  sleep 10
done
for f in "$VERDICTS"/*.md; do echo "===== $(basename "$f" .md) ====="; cat "$f"; done
```

The whole round - the wait and reading all three reports - is then one
iteration rather than a dozen. Name each report as you read it, or three
verdicts arrive as one wall of text with no way to tell which agent said what.
On a timeout, call the wait again; the agents it names are the ones that never
wrote a file, and you can relaunch just those.

Fix every finding, re-run the failing agent, and repeat until all three return `PASS`. Every round gets its own `round-<r>` directory and waits only on the files it expects that round, so a verdict you already acted on can never be mistaken for a fresh one. A `PASS` only counts for the commit it was given: when you change the branch after a round, the agents that passed have not seen that change, so re-run them before opening the PR. If a round keeps failing on the same finding, search for the standard fix and apply it rather than retrying the same thing. After five rounds without a full pass, comment the outstanding findings on the issue, relabel it `ready-for-human`, push the branch, and exit.

## Step 7 - open the PR and merge it

A task is done only when its PR is merged into `main` and no PR of yours is left open.

```sh
git push -u origin HEAD
gh pr create --base main --title "<type>(<scope>): <summary>" --body "Closes #<n>

## What changed
<summary>

## Verification
Behavior spec: tests/behavior/issue-<n>.md
Security PASS, correctness PASS, behavior PASS

## Decisions made on my own
<choices you made where the issue was open, with reasons - or none>"
gh pr checks --watch --fail-fast          # wait for CI if the repo has any
gh pr merge --rebase --delete-branch      # the repo allows rebase merges only
gh issue comment <n> --body "Merged in <PR url>. Behavior spec: tests/behavior/issue-<n>.md. Verification: security PASS, correctness PASS, behavior PASS."
git checkout main && git pull --ff-only
```

If CI fails, fix it on the branch, push, and go back to `gh pr checks`. If the merge is refused for a reason you cannot fix (branch protection, missing permission), that is an environment problem: comment on the issue, relabel it `ready-for-human`, and leave the PR open. The `Closes #<n>` line closes the issue on merge, which removes it from the queue. Then exit; the loop will pick up the next issue.

## Rules that hold throughout

- One issue per iteration. Never batch.
- Never relabel an issue `ready-for-agent`; only humans do that.
- Never edit the body of an issue; add comments instead.
- Never force-push or rewrite `main`. Delete only your own feature branch, and only after its PR is merged.
- Never leave a PR open once you consider the task done. Open PRs are the exception, only for the escalation cases above.
- If a step fails in a way you cannot fix within the scope of the issue, first search for the standard solution and apply it; take the human-in-the-loop path only when that fails too.
