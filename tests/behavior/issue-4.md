# Issue #4: Queue: owl add, list, remove, reorder; local Source; source_ref upsert

All scenarios drive the built `owl` binary from the outside against a running
daemon, except S18, which drives the daemon through the shared client package,
and S19 and S20, which drive the queue Service in-process - the local Source
mints its own reference, so a fixed reference cannot be asked for from the
command line.

"XDG layout" is the temporary directory tree of issue #2: `HOME`,
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` point into it and
`XDG_RUNTIME_DIR` is unset. "Temporary repository" means a real `git init`
repository under that layout whose base branch `main` carries at least one
commit, as in issue #3. "Registered Project" means such a repository added with
`owl project add`.

`owl queue list` prints one header line and one row per Job, with the columns
`POSITION`, `ID`, `PROJECT`, `STATE` and `PROMPT`. Without `--all` it lists the
queue, which is the Jobs in state `pending`, in queue order; with `--all` it
lists every Job whatever its state, the queue first and the rest after it. A
Job that is not in the queue prints `-` for its position. A prompt is free
text, so it is shown as one line and cut to 60 characters, the last three of
which are `...` when there was more.

## Scenarios

### S1 - add inside a registered Project needs no --project
Given a running daemon and a registered Project `api` at `<tmp>/api`
When `owl add "fix the flaky test"` runs with `<tmp>/api` as the working directory
Then it exits 0
And `owl queue list` shows exactly one row, at position `1`, for Project `api`, state `pending`, with the prompt `fix the flaky test`

### S2 - add works from a subdirectory of the Project
Given a running daemon and a registered Project `api` at `<tmp>/api` containing a subdirectory `sub/deeper`
When `owl add "work"` runs with `<tmp>/api/sub/deeper` as the working directory
Then it exits 0
And `owl queue list` shows one Job for Project `api`

### S3 - add outside any Project and without --project is refused
Given a running daemon, a registered Project `api`, and a directory `<tmp>/elsewhere` that is inside no registered Project
When `owl add "work"` runs with `<tmp>/elsewhere` as the working directory
Then it exits with a non-zero code
And stderr says the directory is not inside a registered Project, names the directory, and names the `--project` flag
And `owl queue list` reports an empty queue

### S4 - --project queues against the named Project from anywhere
Given a running daemon, a registered Project `api`, and a directory `<tmp>/elsewhere` inside no registered Project
When `owl add "work" --project api` runs with `<tmp>/elsewhere` as the working directory
Then it exits 0
And `owl queue list` shows one Job for Project `api`

### S5 - --project naming an unregistered Project is refused
Given a running daemon with no Project named `ghost`
When `owl add "work" --project ghost` runs
Then it exits with a non-zero code
And stderr names `ghost`
And `owl queue list` reports an empty queue

### S6 - an empty prompt is refused
Given a running daemon and a registered Project `api`
When `owl add ""` runs with the Project directory as the working directory
Then it exits with a non-zero code
And stderr says the prompt is empty
And `owl queue list` reports an empty queue

### S7 - the innermost Project wins when one is registered inside another
Given a running daemon, a registered Project `outer` at `<tmp>/outer`, and a registered Project `inner` at `<tmp>/outer/inner` which is its own repository
When `owl add "work"` runs with `<tmp>/outer/inner` as the working directory
Then it exits 0
And `owl queue list` shows the Job for Project `inner`

### S8 - queue list shows position, Project, prompt and state in FIFO order
Given a running daemon and registered Projects `api` and `web`
When `owl add "first"` and `owl add "third"` run in `api` and `owl add "second"` runs in `web`, in that order with `second` added between them
Then `owl queue list` exits 0
And its header names `POSITION`, `ID`, `PROJECT`, `STATE` and `PROMPT`
And its rows are, in order, position `1` `api` `pending` `first`, position `2` `web` `pending` `second`, position `3` `api` `pending` `third`

### S9 - an empty queue says so
Given a running daemon and no Job ever added
When `owl queue list` runs
Then it exits 0
And stdout says the queue is empty
And no row is printed

### S10 - remove cancels a pending Job and closes the gap behind it
Given a running daemon and three pending Jobs `first`, `second`, `third` at positions 1, 2 and 3
When `owl queue remove <id of second>` runs
Then it exits 0
And `owl queue list` shows only `first` at position 1 and `third` at position 2
And `owl queue list --all` also shows the removed Job with state `cancelled` and position `-`
And the removed Job keeps the id it had

### S11 - remove refuses a Job that is not pending
Given the cancelled Job of S10
When `owl queue remove <the same id>` runs again
Then it exits with a non-zero code
And stderr names the id and says the Job is not pending
And `owl queue list --all` still shows exactly three Jobs, one of them cancelled

### S12 - remove refuses an unknown id
Given a running daemon with an empty queue
When `owl queue remove 999` runs
Then it exits with a non-zero code
And stderr names `999`

### S13 - reorder moves a Job and persists the new order
Given a running daemon and three pending Jobs `first`, `second`, `third` at positions 1, 2 and 3
When `owl queue reorder <id of third> 1` runs
Then it exits 0
And `owl queue list` shows `third` at 1, `first` at 2 and `second` at 3
And after the daemon is stopped and started again, `owl queue list` still shows that order

### S14 - reorder can move a Job to the end
Given the queue of S13 in its original order
When `owl queue reorder <id of first> 3` runs
Then it exits 0
And `owl queue list` shows `second` at 1, `third` at 2 and `first` at 3

### S15 - reorder refuses a position outside the queue
Given a running daemon and three pending Jobs at positions 1, 2 and 3
When `owl queue reorder <id of first> 0` runs, then `owl queue reorder <id of first> 4`, and then the same with the position `4294967297`, which is larger than the numbering the queue is carried in
Then each exits with a non-zero code
And each stderr names the range of positions the queue has, or says the position is not a position at all
And no stderr reports a different number from the one that was asked for
And `owl queue list` shows the original order every time

### S16 - reorder refuses an unknown id and a Job that is not pending
Given a running daemon with two pending Jobs and one cancelled Job
When `owl queue reorder 999 1` runs, and then `owl queue reorder <id of the cancelled Job> 1`
Then each exits with a non-zero code
And the first names `999`, and the second names its id and says the Job is not pending
And `owl queue list` shows the two pending Jobs in their original order

### S17 - the queue survives a daemon restart
Given a running daemon and two Jobs added in different Projects
When the daemon is stopped and started again against the same XDG layout
Then `owl queue list` shows both Jobs with the same ids, positions, Projects, prompts and states as before the restart

### S18 - every Job carries the local Source and a ulid reference
Given a running daemon and a registered Project
When two Jobs are added through the shared client package rather than the command line
Then each Job the API reports carries source `local`
And each carries a `source_ref` of 26 Crockford base32 characters, which is a ulid
And the two references differ

### S19 - producing the same Source reference twice yields one Job
Given a queue Service over a temporary database, a registered Project, and a Source named `test` whose reference is fixed at `issue:1`
When the Source produces a Job with the prompt `first`, and then produces again with the prompt `second`
Then the queue holds exactly one Job for that Source and reference
And that Job keeps the id and the position it was first given
And its prompt is `second`, because production is an upsert

### S20 - re-producing the reference of a Job that has left the queue does not revive it
Given the Job of S19, cancelled
When the same Source produces the same reference again with the prompt `third`
Then the queue still holds exactly one Job for that Source and reference
And that Job is still cancelled, still has no position, and still has the prompt it had when it was cancelled

### S21 - queue commands report a stopped daemon
Given a temporary XDG layout with no daemon running
When `owl add "work" --project api`, `owl queue list`, `owl queue remove 1` and `owl queue reorder 1 1` each run
Then each exits with a non-zero code
And each stderr says the daemon is not running and names the socket path

### S22 - removing a Project takes its Jobs with it, and says so
Given a running daemon, a registered Project `api` with the pending Jobs `first` and `third` and a cancelled Job `fourth`, and a registered Project `web` with the pending Job `second` between them
When `owl project remove api` runs
Then it exits 0
And stdout says that three Jobs went with the Project, counting the one that had already left the queue
And `owl queue list` shows only `second`, at position 1
And `owl queue list --all` shows no Job for `api`

### S23 - a long prompt is shown cut short
Given a running daemon, a registered Project, and a Job whose prompt is 100 characters of prose
When `owl queue list` runs
Then the prompt column shows the first 57 characters of the prompt followed by `...`
And the position, id, Project and state columns are unaffected
