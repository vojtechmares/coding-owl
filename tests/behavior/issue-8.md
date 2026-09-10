# Issue #8: Disposal: owl jobs accept and drop; owl status lists what awaits a decision

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the daemon's `PATH` in place of
Claude Code. The XDG layout, the temporary repository and the stub are as
`tests/behavior/issue-5.md` describes them, and Jobs are queued with `--no-plan`
so that one `owl start` carries a Job through to a decision.

"A Job in review" means one whose execution Run finished cleanly and whose
Project configured no failing checks, so Verification left it there (ADR-0013).

## Scenarios

### S1 - accept removes the worktree, keeps the branch, and finishes the Job
Given a running daemon and a Job in `review` whose Agent committed `work.txt` on its branch
When `owl jobs accept <job>` runs
Then it exits 0
And the Job's worktree is gone from disk, and `git worktree list` in the Project no longer reports it
And the Job's branch still exists and still carries that commit
And `owl jobs show <job>` reports the Job as `done` with no worktree

### S2 - drop removes the worktree and the branch, and cancels the Job
Given a running daemon and a Job in `review` whose Agent committed `work.txt` on its branch
When `owl jobs drop <job>` runs
Then it exits 0
And the Job's worktree is gone from disk, and `git worktree list` in the Project no longer reports it
And the Job's branch no longer exists in the Project
And `owl jobs show <job>` reports the Job as `cancelled`

### S3 - accept refuses a Job that is not in review, and names its state
Given a running daemon and a Job that is still `pending`
When `owl jobs accept <job>` runs
Then it exits with a non-zero code
And stderr names the Job's current state
And `owl jobs show <job>` reports the Job as still `pending`

### S4 - drop refuses a Job that is not in review, and names its state
Given a running daemon and a Job that Verification left `blocked`
When `owl jobs drop <job>` runs
Then it exits with a non-zero code
And stderr names the Job's current state
And the Job's worktree is still there

### S5 - accept and drop refuse an unknown Job
Given a running daemon
When `owl jobs accept 999` and `owl jobs drop 999` each run
Then each exits with a non-zero code
And each stderr names `999`

### S6 - accept refuses to throw away uncommitted work, unless told to
Given a running daemon and a Job in `review` whose worktree holds an uncommitted file
When `owl jobs accept <job>` runs
Then it exits with a non-zero code
And stderr says the worktree has changes that are not committed
And the worktree is still there, with the file in it
And `owl jobs accept <job> --force` then exits 0 and removes it

### S7 - drop refuses to throw away uncommitted work, unless told to
Given a running daemon and a Job in `review` whose worktree holds an uncommitted file
When `owl jobs drop <job>` runs
Then it exits with a non-zero code
And the worktree and the branch are both still there
And `owl jobs drop <job> --force` then exits 0, removing both

### S8 - owl status counts the Jobs by state
Given a running daemon with a Job in `review`, a Job that is `blocked`, and a Job still `pending`
When `owl status` runs
Then it exits 0
And it reports one Job in each of those states, by name and count

### S9 - owl status lists what awaits a decision and what is blocked
Given the daemon of S8
When `owl status` runs
Then it names the Job in `review` with its Project and its prompt
And it names the `blocked` Job with the reason it is blocked

### S10 - owl status shows a Run in progress
Given a running daemon and a Job whose Agent is waiting, so its Run is still going
When `owl status` runs
Then it reports that Run with its id, its Job and the phase it is carrying out

### S11 - owl status with nothing to report says so
Given a running daemon with no Jobs at all
When `owl status` runs
Then it exits 0
And it says there is nothing queued, running or waiting for a decision

### S12 - disposal never pushes anything anywhere
Given a running daemon, a Project with a remote pointing at an empty bare repository, and one Job in `review` accepted and another dropped
When both commands have run
Then the bare repository holds no refs at all
And the Project's own checkout is still on its base branch

### S13 - the disposal commands report a stopped daemon
Given a temporary XDG layout with no daemon running
When `owl jobs accept 1`, `owl jobs drop 1` and `owl status` each run
Then each exits with a non-zero code
And each stderr says the daemon is not running and names the socket path

### S14 - an accepted Job's work survives the worktree
Given the accepted Job of S1
When the Project's branch is read with git
Then the commit the Agent made is still reachable, with the file it wrote
And the Job's worktree directory does not exist
