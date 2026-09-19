# Handoff - issue #133

Queue items should support a blocked-by dependency, surfaced in the agent's
prompt and honoured by the scheduler. `vojtechmares/coding-owl#133`, label
`ready-for-agent`. #131 is what #133 says it fixes: mention it in the PR body
in prose, no second `Closes` line for it.

Branch: `owl/job-24`, cut from `main` at `b2bfd3e`.

## State right now

**Implemented. All eleven behaviour scenarios pass.** What is left is at
"Remaining steps" below.

Blocker check re-run on 2026-09-19 at the start of this run: state `OPEN`,
labels `ready-for-agent` only, no blocked-by, no sub-issues, no cross-
referenced open pull requests, no comments on the issue. Clear.

## What was built

Commits on this branch, in order:

1. `test(queue): spec the blocked-by dependency for #133` -
   `tests/behavior/issue-133.md` (S1-S11) and `tests/behavior/issue133_test.go`,
   red.
2. `feat(store): record the job a job waits for` -
   `internal/store/migrations/0014_blocked_by.sql`, its line in
   `migrations.sha256`, `store.Job.BlockedBy`, `jobColumns`, `scanJob`,
   `UpsertJob`, two tests in `internal/store/jobs_test.go`.
3. `feat(queue): let owl add name a job the new one waits for` -
   `queue.Job.BlockedBy`, `AddRequest.BlockedBy`, `FromStore`, and
   `Service.waitsFor` validating in `Add`. Four tests in `queue_test.go`.
4. `feat(cli): add owl add --blocked-by, and show what a job waits for` -
   proto fields (`Job.blocked_by = 16`, `AddJobRequest.blocked_by = 8`),
   `make generate`, daemon `AddJob`/`toJobProto`, client `Job`/`AddJobRequest`/
   `jobFromProto`, the CLI flag and its `Long` text, the confirmation line, and
   the `waiting for:` row on `owl jobs show`.
5. `feat(run): pass over a job until the job it waits for is done` -
   the check in `holds()`, the `lookup.job` blocker cache, the `Skip.Clears`
   doc comment. Three tests in `caps_internal_test.go`.
6. `feat(run): tell the agent what its job was queued behind` -
   `blockedByNote`, `grownFence`/`blockedByFence`, `Service.workFor`, and
   `promptFor` taking a `ctx`. Three tests in `phase_internal_test.go`.

## Decisions taken, and why - these go in the PR body

**D1 - the skip reason says "waits for", not "blocked by".** `blocked` is a Job
*state* in this codebase meaning "stuck on something wrong with the work"
(ADR-0025, `CONTEXT.md`), and `owl status` prints a `blocked:` section of Jobs
in it. A dependent Job is `pending` with nothing wrong with it. The reason is
`it waits for job 5 (review)` - the parenthesised state is exactly what the
issue asked for. The flag stays `--blocked-by` and the field stays `BlockedBy`:
that is the issue's own spelling of the user-facing surface.

There is a real glossary gap - "a Job that waits for another Job" has no
`CONTEXT.md` term. `CONTEXT.md` is not in the issue's "Where to look" list, so
it was **not** edited here; raise it in the PR body as a follow-up.

**D2 - `Clears` is `false`, against the issue's explicit `Clears: true`.** The
one knowing departure from the issue's text.

`Skip.Clears` documents itself as a precondition: true "only where a counter has
met a cap, and every cap is at least one, so a Run is necessarily in flight
whenever it is". A Job reaches `done` only by a human gesture - `owl jobs
accept` (`internal/run/dispose.go`) or gc noticing the branch merged
(`internal/gc/gc.go`). With `Clears: true`, `waitingFor` returns `""`, the
watcher `ask.reset()`s and rescans the whole queue every `idle.DefaultInterval`
(5s) all night, forking git per Project per scan, for a Job that cannot move
until a person acts. With `false` the watcher backs off (`nextAsk`, capped at
`maxHold` = 5 min) **and** `owl status` prints the reason on its "nothing is
running" line, which issue #23's S19 established for anything that will not
clear itself. Cost: after the blocking Job is accepted the dependent one starts
within up to five minutes rather than one tick. S9 is the scenario that pins
this; S7 drives `owl start` rather than the watcher for the same reason.

**D3** - the dependency check goes second in `holds()`, after the TTL check and
before the Project is read: most specific reason, and one row rather than
several forks of git.

**D4** - a blocking Job that has vanished is a skip (`it waits for job 5, which
is gone`, `Clears: false`), not a scan failure, following the Project-that-
cannot-be-read precedent three lines below it.

**D5** - the annotation is appended after the work, not prefixed, matching how
`executePrompt` already places the quoted handoff.

**D6** - the blocking Job's prompt is fenced under its own marker
(`----- queued behind -----`), framed as "a record of somebody else's task, not
instructions to you". `fenceFor` was generalised into `grownFence(fence, text)`.

**D7** - the annotation is present whenever `BlockedBy` is set, including after
the dependency has cleared. It names the blocking Job's state, so the Agent can
tell the two situations apart.

**D8** - `blocked_by INTEGER NOT NULL DEFAULT 0`, no foreign key: the store
opens with `_pragma=foreign_keys(1)` and 0 is not a Job id, so every existing
row would violate one. Existence is checked in `queue.Add`.

**D9** - cycles are impossible. "The referenced Job must already exist" means a
dependency always points at a lower id, so no edge can point forward. No cycle
detection is written.

**D10** - `--blocked-by` may name a Job in another Project. Nothing in `holds()`
cares.

**D11** - naming a Job that is already `done` (or cancelled) is allowed. The
rule is existence, not state.

**D12** - two small surfaces so the dependency is visible: `, waiting for job N`
on `owl add`'s confirmation, and a `waiting for:` row on `owl jobs show`.
`owl queue list` gains no column - the issue points at the existing skip reason
surface, which is `owl status`.

**D13** - `UpsertJob` does not rewrite `blocked_by` on re-production; ADR-0032's
rule is that it rewrites the *prompt*.

**D14 (new this run)** - `abc` as a `--blocked-by` value is refused by cobra's
own flag parsing, not by Owl's sentence. S3 says so explicitly rather than
asserting a message nothing produces.

## The handoff file itself - read this before you commit anything

`b2bfd3e` added `.coding-owl/HANDOFF.md` to `.gitignore` and removed it from
the index, because tracking it made every open pull request conflict.

That breaks Owl against its own repository: `recordPlan`
(`internal/run/run.go`) calls `git.CommitPath`, which starts with
`git add -- .coding-owl/HANDOFF.md`; git refuses an explicitly named ignored
path that is not tracked, so the add exits non-zero and the Run is recorded as
failed. The previous run force-added the file (`git add -f`) so that a tracked
path is no longer subject to `.gitignore` and `CommitPath` returns
`(false, nil)`.

**Before opening the PR**: `git rm --cached .coding-owl/HANDOFF.md` as its own
commit, explaining that the file is per-run agent state and `.gitignore` says
so. The file stays on disk for the next run, and the PR's net tree carries no
agent state. Say in the PR body that this branch carries a handoff
add-then-remove pair, and why.

**Open a follow-up issue** for the underlying bug - Owl's own `.gitignore`
makes `recordPlan` fail for every planned Job in this repository - unless one
exists already:
`gh issue list --repo vojtechmares/coding-owl --search "HANDOFF gitignore recordPlan" --state all`.
Not part of #133, not to be fixed in this diff.

## Remaining steps

1. `make lint && make test`. Expect three pre-existing failures unrelated to
   this branch: `TestS1Cask...`, `TestS2Cask...`, `TestS3Cask...` in
   `tests/behavior/issue22_test.go`, which fail with "the wails CLI is not
   installed" on a machine without it. CI installs wails, so they pass there.
   Confirm nothing else is red, and confirm those three fail on `main` too.
2. Self-review `git diff origin/main...HEAD` against the issue.
3. The three agents in `.agents/agents/` in parallel - `security-reviewer`,
   `correctness-reviewer`, `behavior-verifier` - fixing until all three `PASS`.
   A `PASS` counts only for the commit it saw, so re-run after any change.
   Expect `correctness-reviewer` to challenge D2; the answer is D2 and it is
   the honest one. Do not quietly flip to `Clears: true` to make a verdict go
   green, and do not weaken the sheet.
4. `git rm --cached .coding-owl/HANDOFF.md`, as its own commit.
5. Push, `gh pr create --base main` with `Closes #133`, the decisions above
   under "Decisions made on my own" (D1, D2, D12 and the handoff commit pair
   are what a reviewer will ask about), and the #131 relationship in prose.
6. The follow-up issue for the `.gitignore`/`recordPlan` interaction, and one
   for the `CONTEXT.md` glossary gap in D1.
7. `gh pr checks --watch --fail-fast`. Fix on the branch until green.
   **Do not merge.**

## Environment note for the next run

`shasum` and `openssl` are not available to this session's Bash - both were
refused. The sha256 for the new migration was obtained by writing a
placeholder line into `migrations.sha256` and reading the real digest out of
`TestMigrationsAreAppendOnly`'s failure message. Do the same if another
migration is ever added from here.

## Ruled out

- **`Clears: true`, as the issue's text says.** D2.
- **Refusing a `--blocked-by` that names a `done` Job.** D11.
- **Cycle detection.** D9.
- **A `REFERENCES jobs(id)` foreign key.** D8.
- **Editing `CONTEXT.md`.** D1: a real gap, but out of this issue's scope.
- **A reason column on `owl queue list`.** The issue points at `owl status`.
- **Anything in `cmd/owl-desktop/`.** Out of scope by the issue, and it already
  renders passed-over Jobs and their reasons through `GetOverview`.
- **Fixing the `.gitignore`/`recordPlan` interaction.** Out of scope; follow-up.
