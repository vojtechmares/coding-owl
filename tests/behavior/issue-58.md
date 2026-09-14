# Issue #58: A poisoned head-of-queue Job blocks everything behind it

The scheduler always picks the oldest Job the caps allow. Reported at commit
`701b936`: when that Job's worktree or branch could not be created - the
branch already existed, or a worktree directory was left behind by an earlier
attempt - the failure came back from the start as a plain error. The Job
stayed pending at the head of the queue, the watcher backed off, picked it
again, and nothing behind it ever ran. A rebase that fails blocks the Job; a
worktree that cannot be made did not.

The fix is that a failure to create the Job's worktree or branch blocks the
Job with git's own words as the reason, exactly as a rebase failure does, so
the scheduler moves on and the user can retry the Job once the cause is
cleared. Cleaning up leftover directories is out of scope.

The run scenarios drive the `run` package against a real repository with the
fake Driver and Executor of the existing run tests. The end-to-end scenario
drives the built `owl` binary against a daemon, with the stub agent of issue
#5 standing in for Claude Code.

## Scenarios

### S1 - a Job whose branch already exists is blocked, and the Job behind it runs
Given a Project whose repository already has a branch named as the first Job's would be, and two pending Jobs
When a Run is started, and then another
Then the first Job is `blocked` with a reason naming its branch
And it has no Run
And the second Job runs and ends in `review`

### S2 - a Job whose worktree directory is already there is blocked by name
Given a Project and a pending Job, and a directory already at the path the Job's worktree would take
When a Run is started
Then the Job is `blocked` with a reason naming that path
And it has no Run

### S3 - `owl start` reports the blocked Job and the next start moves on
Given a running daemon, a Project whose repository already has a branch named as the first Job's would be, and two pending Jobs
When `owl start` is run, and then run again
Then the first `owl start` exits non-zero and names the branch
And `owl jobs show` reports the first Job `blocked` with a reason naming the branch
And the second `owl start` reports a Run started for the second Job
