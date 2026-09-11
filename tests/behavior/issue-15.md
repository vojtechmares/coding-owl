# Issue #15: Garbage collection: reconcile worktrees, prune, surface unfinished work, merge-detect implicit accept, owl gc

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on its `PATH` in place of Claude
Code. The XDG layout, the temporary repository, the stub and the harness
Account are as `tests/behavior/issue-5.md` and `tests/behavior/issue-14.md`
describe them.

Garbage collection is a plain task the daemon runs: no Agent is ever involved
(ADR-0015). It reclaims what nothing needs any more, prunes what git is still
counting, and **reports rather than removes** anything that looks like work
somebody has not finished with. It runs when the daemon starts, on an interval,
and on demand as `owl gc`.

"Its own worktree" is `$XDG_DATA_HOME/coding-owl/worktrees/<job-id>`, which is
where a Job's worktree lives (ADR-0014). The daemon's own configuration sets
how often the task runs and how long a Job may wait for a decision before it is
reported, with `garbageCollection: {interval, reviewAfter}`; these scenarios set
both short so that nothing waits on a clock.

## Scenarios

### S1 - a worktree whose Job is finished with is reclaimed
Given a Job that has been accepted, whose worktree was put back by hand
When `owl gc` runs
Then it exits 0
And stdout names the worktree it reclaimed
And that directory is gone

### S2 - a worktree whose Job was cancelled is reclaimed
Given a Job that was dropped, whose worktree was put back by hand
When `owl gc` runs
Then the worktree is gone
And stdout names it

### S3 - a worktree belonging to no Job at all is reclaimed
Given a directory under the worktree directory that is a worktree of the Project and belongs to no Job
When `owl gc` runs
Then it is gone
And stdout names it

### S4 - a directory that is not a worktree is left alone
Given a directory under the worktree directory that is not a git worktree
When `owl gc` runs
Then it is still there
And stdout does not name it

### S5 - a worktree holding uncommitted changes is reported, not reclaimed
Given a Job that has been accepted, whose worktree was put back by hand with a file nobody has committed in it
When `owl gc` runs
Then the worktree is still there, with that file in it
And stdout lists it as unfinished work, saying it holds changes nobody has committed

### S6 - a worktree whose Job is still going is left alone
Given a Job in review with its worktree, and a pending Job
When `owl gc` runs
Then both worktrees that exist are still there
And stdout does not list them as reclaimed

### S7 - stale worktree administrative entries are pruned per Project
Given a Job whose worktree directory was deleted by hand, so that git still lists a worktree nobody can use
When `owl gc` runs
Then `git worktree list` in the Project no longer reports it
And stdout says the Project was pruned

### S8 - a Job whose branch is merged into the base branch is accepted implicitly
Given a Job in review whose branch has been merged into the Project's base branch
When `owl gc` runs
Then `owl jobs show <job>` reports the Job as `done`
And its worktree is gone
And stdout says the Job was accepted because its branch is in the base branch

### S9 - a Job whose branch is not merged stays in review
Given a Job in review whose branch carries a commit the base branch does not
When `owl gc` runs
Then the Job is still `review`
And its worktree is still there

### S10 - a Job waiting for a decision too long is reported
Given a Job in review, and a daemon configured to report a Job that has waited longer than an instant
When `owl gc` runs
Then stdout lists the Job as unfinished work, saying it is waiting for a decision
And the Job is still `review`, and its worktree is still there

### S11 - a Job left active by a daemon that died is queued again, not reported
Given a Job whose Run was in progress when the daemon was killed outright, and a daemon running again
When `owl gc` runs
Then stdout does not list the Job: the daemon that started up ended its Run `interrupted` and returned it to `pending` (ADR-0011, issue #11 S17)
And garbage collection would report it only if no daemon had done so

### S12 - owl status lists unfinished work
Given the unfinished work of S5, and a Job a dead daemon left active as in S11
When `owl status` runs
Then it lists the unfinished work with what is unfinished about it
And the Job the next daemon queued again is not among it
And what is merely awaiting a decision is listed apart from them

### S13 - owl gc with nothing to do says so
Given a running daemon whose Projects and worktrees are all in order
When `owl gc` runs
Then it exits 0
And stdout says there was nothing to do

### S14 - garbage collection runs when the daemon starts
Given a worktree whose Job is finished with, and a daemon that is not running
When a daemon is started
Then that worktree is gone without anybody asking
And the daemon's log says what it reclaimed

### S15 - garbage collection runs again on its interval
Given a running daemon configured to collect often, and a worktree that becomes reclaimable after it started
When the interval has passed
Then that worktree is gone without anybody asking

### S16 - garbage collection never runs an Agent
Given the collections of S14 and S15
When the stub agent's record is read
Then it recorded no invocation

### S17 - owl gc reports a stopped daemon
Given a temporary XDG layout with no daemon running
When `owl gc` runs
Then it exits with a non-zero code
And stderr says the daemon is not running and names the socket path
