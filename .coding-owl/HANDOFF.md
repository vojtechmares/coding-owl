# Handoff - issue #108, enforce the append-only migration policy with a static check

Written by the planning Run on 2026-09-18. Nothing of that session survives, so
this file is the whole of what you are given. Read it top to bottom before
touching anything.

Issue: https://github.com/vojtechmares/coding-owl/issues/108
Branch: `owl/job-1`, sitting on `origin/main` at `fdbb234` with a clean tree.

## Where this Run got to

Planning only. The plan below is committed; no code, no spec sheet, no tests
have been written yet. Start at "First steps".

## The blocker check, already done

Done on 2026-09-18 and it came back clear - do not redo it unless you want to:
issue open, labels `["ready-for-agent"]` only, no `blocked_by`, no open
sub-issues, no open cross-referenced pull requests. The body names #77 ("see
#77, closed on that basis"); #77 is CLOSED. Nothing in the body or comments
(there are none) says anything must land first.

## What the issue asks for

Migrations are append-only by policy: a migration is added at the end and never
inserted between others, renamed, deleted or edited. `internal/store.migrate`
(`internal/store/store.go:78-124`) depends on that: it tracks progress in
`PRAGMA user_version` as the **index into the sorted file list**, so the version
says how many migrations ran, not which ones. Under append-only that is sound
and #77 was closed on that basis. Nothing enforces the policy, and breaking it
fails silently. Four deliverables:

1. A committed manifest of the migrations - file name and sha256 per line, in
   sorted order.
2. A test in `internal/store` that reads `migrationsFS` and fails when an entry
   moved, changed, was renamed or was deleted, and when a file that is not in
   the manifest sorts before one that is. Its failure message must name the
   policy and say the fix is to append and update the manifest, never to edit
   the manifest to match.
3. `migrateBefore` in `internal/store/migrate_internal_test.go:42` must stop
   hard-coding `PRAGMA user_version = len(names)`; it must derive the version
   from the named migration instead.
4. A note in `docs/adr/0008-sqlite-queue-behind-source-interface.md` recording
   the append-only policy and that a migration library was considered and not
   adopted.

Out of scope, stated in the issue: changing the version scheme, recording names
or checksums in the database, adopting `golang-migrate`/`goose`/any library.

Acceptance criteria are the seven checkboxes at the bottom of the issue. Read
them again before opening the pull request; the scenarios below map onto them
one for one.

## Decisions taken while planning, with the reasons

These are settled. Re-open one only if you find it does not work, and say so in
the pull request if you do.

### D1 - the behavior scenarios drive `go test`, they do not call a function

The bdd skill is explicit: "Every Then must be observable from outside: CLI
output, exit code, a file on disk, an API response. Never an internal function
or struct." The outside surface of this feature is what CI does, so each
scenario copies the repository to a temp directory, mutates
`internal/store/migrations/` there, runs

    go test -count=1 -v -run <TestName> ./internal/store/

in the copy, and asserts on the exit code and the output.

**Ruled out:** exporting a `CheckAppendOnly(fs.FS, manifest)` from
`internal/store`, or a new `internal/migrationpolicy` package, so that
`tests/behavior` could call it with an `fstest.MapFS`. It is faster and it is
how several behavior tests here already work (they import `internal/store`,
`internal/queue`, `internal/run`), but it puts production API in the tree for
no reason but testing, and it proves the checker returns an error rather than
proving CI fails. Rejected on the skill's wording. The consequence is that
**all the new checking code can live inside the test file** - no new production
Go code at all, which is also the smallest diff that satisfies the issue.

Cost: seven nested `go test` runs. Measured here, a warm fully-cached
`go test ./internal/store/` is 0.3s and a cold one 18s; each scenario changes an
embedded file so the `store` package and its test binary recompile and relink,
the rest of the build cache is shared and hits. Copying is 485 files. Budget
roughly 5s a scenario. If it turns out much worse, say so in the pull request
rather than silently dropping scenarios.

### D2 - the manifest is `internal/store/migrations.sha256`

Beside the directory, not inside it: anything inside `migrations/` looks like
something that gets applied. `//go:embed migrations/*.sql` would not match it
anyway, and `migrate()` filters on `.sql`, so either would be safe - this is
about what a reader assumes.

Format, one migration per line, sorted, `shasum`-compatible so a line can be
pasted straight in:

    <64 hex sha256><two spaces><file name>

The parser skips blank lines and lines starting with `#`, which lets the file
carry a short header telling you to **append** a line rather than regenerate
the file. Names are bare (`0001_projects.sql`), which is what
`cd internal/store/migrations && shasum -a 256 *.sql` emits (`sha256sum *.sql`
on Linux). Generate the thirteen lines with that command; do not hand-write the
hashes.

### D3 - rule 2 is implemented exactly as the issue words it

"Anything not in the manifest sorts strictly after every entry that is." So a
newly appended migration whose line has **not** been added to the manifest
still passes. That is the issue's wording and the acceptance criteria only
require that appending *and updating* passes.

It is a real gap - an agent that appends `0014_foo.sql` and forgets the
manifest line leaves that migration editable forever - and the stricter rule
(fail when a migration on disk has no manifest line) would close it. Do not
take that liberty here; it contradicts a sentence the issue actually wrote.
Instead, **file a small follow-up issue** proposing the stricter rule, and say
in the pull request that you did. That is what AGENTS.md asks for things noticed
outside the scope.

### D4 - ADR-0008 is amended, not superseded

`docs/adr/README.md` has a field for exactly this case: a record that said
something incomplete about its own mechanism keeps its status and gains an
`**Amended:**` line. ADR-0021 is the example to copy. So:

- add `- **Amended:** 2026-09-18, ...` under `- **Status:** Accepted`, in
  ADR-0021's phrasing - the strategy the record called for is now named;
- rewrite the Consequences bullet that says only "A schema migration strategy is
  needed from the very first release" so it names the strategy: embedded `.sql`
  files applied in sorted order, progress in `PRAGMA user_version` as the index
  into that list, append-only, enforced by `internal/store/migrations.sha256`
  and the test beside it;
- add an "Alternatives considered" entry for a migration library
  (`golang-migrate`, `goose`): not adopted, the hand-rolled runner is correct
  under append-only, and swapping it once v0.1.0 is on user machines needs its
  own back-fill; revisiting it is a new issue with an ADR of its own.

The index table in `docs/adr/README.md` keeps `Accepted` - no edit needed there.

### D5 - `migrateBefore` derives the version from the named migration

New shape, in `internal/store/migrate_internal_test.go`: take the sorted list of
all `.sql` migrations, find the index of `migration` in it, `t.Fatalf` naming
the unknown migration if it is not there, apply `names[:idx]`, and set
`PRAGMA user_version = idx`. Under append-only `idx` equals the old
`len(names)`, so nothing about the existing test changes; the point is that the
version is now the position of a migration that exists rather than a count of
files, and that stopping before a migration that is not there fails loudly
instead of quietly testing nothing.

### D6 - no CHANGELOG entry

CHANGELOG.md follows Keep a Changelog and its `[0.1.0]` entries are all things a
user of Owl can see. This change is a developer-facing test, a manifest and an
ADR note - nothing a user observes. Skipping it is the decision; put it under
"Decisions made on my own" in the pull request.

### D7 - the branch stays `owl/job-1`

AGENTS.md asks for `feat/issue-<n>-<slug>`, but that is the Ralph-loop queue
worker's convention. This Run is a Coding Owl Job, Owl owns the branch and the
worktree (ADR-0007), and the Job prompt says to push `HEAD`. Renaming would
fight the tool. Push `owl/job-1` and open the pull request from it.

## The behavior spec sheet

Write `tests/behavior/issue-108.md` with prose first (what changes, what is out
of scope, and the shared setup: "each scenario copies the repository's tracked
and untracked-but-not-ignored files to a temp directory, mutates
`internal/store/migrations/` and `internal/store/migrations.sha256` there, and
runs `go test` in the copy"), then these scenarios. Once committed the sheet is
the contract and may never be weakened - the behavior-verifier diffs it against
its first commit.

- **S1 - the migrations as committed satisfy the policy.** Given the repository
  unmodified, when `go test -count=1 -v -run TestMigrationsAreAppendOnly
  ./internal/store/` runs, then it exits 0 and the output carries
  `--- PASS: TestMigrationsAreAppendOnly`. (No copy needed; run it in
  `repoDir`.)
- **S2 - editing an existing migration fails.** Given a copy with a line
  appended to `internal/store/migrations/0005_checks.sql`, when the same command
  runs there, then it exits non-zero, `--- FAIL: TestMigrationsAreAppendOnly` is
  in the output, and the output names `0005_checks.sql` and both the recorded
  and the computed sha256.
- **S3 - inserting a migration between two existing ones fails.** Given a copy
  with a new `internal/store/migrations/0005a_inserted.sql` and the manifest
  untouched, when the command runs, then it exits non-zero and the output names
  `0005a_inserted.sql` and says it sorts before a migration that is already
  recorded. (`0005a_inserted.sql` sorts strictly between `0005_checks.sql` and
  `0006_ttl.sql`: `a` is 0x61, `_` is 0x5F.)
- **S4 - renaming an existing migration fails.** Given a copy where
  `0011_usage.sql` has been renamed to `0011a_usage.sql`, when the command runs,
  then it exits non-zero and the output names `0011_usage.sql` as recorded but
  no longer present.
- **S5 - deleting an existing migration fails.** Given a copy with
  `0009_verifier.sql` removed, when the command runs, then it exits non-zero and
  the output names `0009_verifier.sql` as recorded but no longer present.
- **S6 - appending a new migration and updating the manifest passes.** Given a
  copy with a new `internal/store/migrations/0014_probe.sql` holding valid SQL
  and its sha256 line appended to `internal/store/migrations.sha256`, when the
  command runs, then it exits 0 and the output carries
  `--- PASS: TestMigrationsAreAppendOnly`.
- **S7 - the failure names the policy and the fix.** Given the copy of S2, when
  the command runs, then the output says migrations are append-only, that the
  fix is to add a new migration at the end and append its line to
  `internal/store/migrations.sha256`, and that the manifest must not be edited
  to match. Name the exact substrings the test asserts, here in the sheet, so
  the test asserts from the sheet and not from the implementation.
- **S8 - a database is brought to a named migration, not to a file count.**
  Given a copy where `0013_run_system_prompt.sql` has been renamed to
  `0013a_run_system_prompt.sql`, when
  `go test -count=1 -v -run TestARunFromBeforePromptsWereRecordedHasNone
  ./internal/store/` runs there, then it exits non-zero and the output names
  `0013_run_system_prompt.sql` as a migration it cannot stop before. (Today that
  run passes, because `migrateBefore` counts files rather than locating the one
  it was given.)

Mapping to the acceptance criteria: S2 -> edit, S3 -> insert, S4+S5 -> rename
and delete, S6 -> append, S7 -> message, S8 -> `migrateBefore`, S1 -> the check
is real and green on `main`'s migrations.

## Traps to avoid

- **`go test -run NoSuchTest` exits 0.** Before the implementation exists, a
  scenario that only asserts "exit 0" passes vacuously, and S1 and S6 would
  never be red. That is why every scenario runs with `-v` and asserts on the
  `--- PASS:` / `--- FAIL:` line for the named test, not on the exit code alone.
- **Copy untracked files too.** Use `git -C <repoDir> ls-files -zco
  --exclude-standard`, not plain `ls-files`. During the red-green loop the new
  test file and the manifest are uncommitted, and a copy of committed files only
  would run the nested `go test` against a tree that does not have them. Skip
  entries that no longer exist on disk (tracked-but-deleted paths are listed).
  Ignored paths - `website/node_modules`, `cmd/owl-desktop/frontend/node_modules`
  - are excluded by `--exclude-standard`, which is the point; 485 files copy in
  well under a second.
- **Helper names are package-wide.** Every behavior test is `package
  behavior_test`, so suffix helpers: `copyRepoFor108`, `runStoreTestFor108`.
  `repoDir` already exists (defined in `tests/behavior/issue2_test.go`'s
  `TestMain`) - use it, do not redefine it.
- **Give the nested `go test` a context timeout** (five minutes is plenty) so a
  hang costs one scenario and not the whole suite.
- **S2 and S7 share a mutation.** Run it once behind a `sync.OnceValues` and let
  both scenarios assert on the captured output, so the pair costs one nested
  run, while each test still stands on its own.
- **Make the appended SQL in S6 valid** (`CREATE TABLE _issue108_probe (id
  INTEGER PRIMARY KEY);`). `-run` filters the other tests out, but a valid file
  costs nothing and keeps the copy usable.
- **Report every violation, not the first.** Collect the problems, then one
  `t.Fatalf` with all of them followed by the policy paragraph. An insert
  legitimately produces two lines (an entry whose index moved, and an extra file
  that sorts too early); that is fine, assert on the specific line the sheet
  names.

## The check itself

All of it goes in a new `internal/store/migrations_internal_test.go`, package
`store`, test `TestMigrationsAreAppendOnly`. It reads `migrationsFS` (the issue
asks for that specifically) and `os.ReadFile("migrations.sha256")` - a Go test's
working directory is its package directory, so the relative path is right.

Sorted `.sql` names from `migrationsFS`, manifest parsed from the file, then:

1. every manifest entry whose name is not on disk -> renamed or deleted;
2. every manifest entry present whose sha256 differs -> edited, print both;
3. every manifest entry present at a different index -> something moved in
   ahead of it;
4. every `.sql` on disk that is not in the manifest and does not sort strictly
   after the last manifest entry -> inserted.

Then the shared paragraph, roughly:

> Migrations are append-only: a migration is added at the end and never
> inserted between others, renamed, deleted or edited. internal/store.migrate
> tracks progress as the index into the sorted file list (PRAGMA user_version),
> so a database that already applied a migration never sees a later change to
> it. The fix is to add a new migration at the end and append its line to
> internal/store/migrations.sha256 - never to edit the manifest to match.

Also fail, clearly, when the manifest is missing or a line will not parse.

## First steps

1. `gh issue view 108 --repo vojtechmares/coding-owl --comments` - confirm
   nothing was added to the issue since 2026-09-18, and redo the blocker check
   if the labels changed.
2. Read `AGENTS.md`, `CONTEXT.md`, `docs/adr/0008-sqlite-queue-behind-source-interface.md`
   and `docs/adr/README.md`. Then `internal/store/store.go:76-124` and
   `internal/store/migrate_internal_test.go`.
3. Invoke the `bdd` skill (`/bdd 108`) and follow it with this plan as the
   input: write `tests/behavior/issue-108.md` from the scenarios above, then
   `tests/behavior/issue108_test.go` with `TestS1..TestS8`, run
   `go test ./tests/behavior/ -run 'TestS[1-8].*'` and check every one fails for
   the right reason, then commit both together:
   `test(behavior): spec sheet and failing tests for issue #108 --signoff`.
4. Implement in this order, committing each: the manifest
   (`chore(store): record the migrations in a checksum manifest`), the check
   (`test(store): fail when the append-only migration policy is broken`),
   `migrateBefore` (`test(store): stop before a named migration, not a count`),
   the ADR (`docs(adr): record the append-only migration policy in ADR-0008`).
5. `make lint && make test`. Both must be green before you stop; Owl's
   Verification runs after you exit.
6. Run `behavior-verifier`, `security-reviewer` and `correctness-reviewer` in
   parallel per the `bdd` skill, into
   `${TMPDIR:-/tmp}/owl-verify/issue-108/round-<r>`, and wait with
   `.agents/skills/bdd/scripts/wait-verdicts.sh "$VERDICTS" behavior security
   correctness`. Fix findings in code or tests, never in the sheet, and re-run
   all three after any change.
7. File the D3 follow-up issue.
8. `git push -u origin HEAD` and open the pull request against `main`, body
   starting `Closes #108`, with "Decisions made on my own" covering D1 (behavior
   scenarios drive `go test` rather than calling a function), D2 (manifest
   location and format), D3 (rule 2 taken literally, follow-up issue filed), D4
   (ADR amended rather than superseded), D6 (no CHANGELOG entry) and D7 (the
   branch name). Do not merge it.
9. Keep this file current as you go - tick off what is done, and write down
   anything you decided differently and why.
