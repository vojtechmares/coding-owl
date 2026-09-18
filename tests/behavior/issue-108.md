# Issue #108: Enforce the append-only migration policy with a static check

Migrations are embedded `.sql` files under `internal/store/migrations/`, applied
in file name order by `internal/store.migrate`, which tracks progress in
SQLite's `PRAGMA user_version` as the **index into the sorted file list**. That
is sound only while the list is append-only: a migration is added at the end and
never inserted between others, renamed, deleted or edited. Nothing enforced the
policy, so breaking it failed silently.

The change is a committed manifest of the migrations - one line per migration,
its sha256 and its file name, in sorted order, at
`internal/store/migrations.sha256` - and a test in `internal/store` that reads
`migrationsFS` and the manifest and fails when the policy is broken. The test
helper `migrateBefore` also stops treating the file count as the schema version
and derives it from the migration it is told to stop before.

Out of scope: changing the version scheme, recording names or checksums in the
database, and adopting a migration library.

Each scenario copies the repository's tracked and untracked-but-not-ignored
files to a temporary directory, mutates `internal/store/migrations/` and
`internal/store/migrations.sha256` in the copy, and runs `go test` there. The
verdict is that nested run's exit code and output, which is what CI sees. S1
needs no mutation and runs in the repository itself.

## Scenarios

### S1 - the migrations as committed satisfy the policy
Given the repository unmodified
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in it
Then it exits 0
And the output carries `--- PASS: TestMigrationsAreAppendOnly`

### S2 - editing an existing migration fails
Given a copy of the repository with a line appended to `internal/store/migrations/0005_checks.sql`
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in the copy
Then it exits non-zero
And the output carries `--- FAIL: TestMigrationsAreAppendOnly`
And the output names `0005_checks.sql`
And the output carries the sha256 the manifest records for it
And the output carries the sha256 of the edited contents

### S3 - inserting a migration between two existing ones fails
Given a copy of the repository with a new `internal/store/migrations/0005a_inserted.sql` and the manifest untouched
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in the copy
Then it exits non-zero
And the output carries `--- FAIL: TestMigrationsAreAppendOnly`
And a line of the output names `0005a_inserted.sql` and says it `sorts before` a migration that is already recorded

### S4 - renaming an existing migration fails
Given a copy of the repository where `internal/store/migrations/0011_usage.sql` has been renamed to `0011a_usage.sql`
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in the copy
Then it exits non-zero
And the output carries `--- FAIL: TestMigrationsAreAppendOnly`
And a line of the output names `0011_usage.sql` and says it is `no longer present`

### S5 - deleting an existing migration fails
Given a copy of the repository with `internal/store/migrations/0009_verifier.sql` removed
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in the copy
Then it exits non-zero
And the output carries `--- FAIL: TestMigrationsAreAppendOnly`
And a line of the output names `0009_verifier.sql` and says it is `no longer present`

### S6 - appending a new migration and updating the manifest passes
Given a copy of the repository with a new `internal/store/migrations/0014_probe.sql` holding valid SQL
And its sha256 line appended to `internal/store/migrations.sha256`
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in the copy
Then it exits 0
And the output carries `--- PASS: TestMigrationsAreAppendOnly`

### S7 - the failure names the policy and the fix
Given the copy of S2
When `go test -count=1 -v -run TestMigrationsAreAppendOnly ./internal/store/` runs in the copy
Then the output carries `Migrations are append-only`
And the output carries `add a new migration at the end and append its line to internal/store/migrations.sha256`
And the output carries `never edit the manifest to match`

### S8 - a database is brought to a named migration, not to a file count
Given a copy of the repository where `internal/store/migrations/0013_run_system_prompt.sql` has been renamed to `0013a_run_system_prompt.sql`
When `go test -count=1 -v -run TestARunFromBeforePromptsWereRecordedHasNone ./internal/store/` runs in the copy
Then it exits non-zero
And the output carries `--- FAIL: TestARunFromBeforePromptsWereRecordedHasNone`
And the output names `0013_run_system_prompt.sql` and says it `cannot stop before` it
