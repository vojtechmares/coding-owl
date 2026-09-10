# Issue #12: Rebase the Job branch at every Run start; conflict blocks with the paths named

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the daemon's `PATH` in place of
Claude Code. The XDG layout, the temporary repository and the stub are as
`tests/behavior/issue-5.md` describes them.

A Job takes several Runs here, which is what makes a rebase possible at all, so
Jobs are queued planned rather than with `--no-plan`: the planning Run's branch
is cut fresh from the base, the Job goes back in the queue, and the base moves
before its execution Run starts.

"The Project's base branch" is the local branch the Project was registered
against. Owl rebases the Job's own branch onto it and never moves anything else.

## Scenarios

### S1 - the base branch moving between Runs reaches the next Run's worktree
Given a Job whose first Run has ended, and a commit added to the Project's base branch since
When `owl start` runs again for that Job
Then it exits 0
And the file that commit added is in the Job's worktree
And the Job's branch has the base branch's tip as an ancestor

### S2 - what the Agent committed survives the rebase
Given the Job of S1, whose Agent committed a file in its first Run
When the second Run has started
Then that file is still in the worktree with the contents the Agent gave it
And the Job's branch still carries the Agent's commit, on top of the new base

### S3 - Verification runs against the rebased state
Given a Project whose base branch gains both a commit and a check that only passes when that commit is present, and a Job whose first Run has ended
When the second Run finishes
Then the check passed
And the Job is `review`

### S4 - a conflicting rebase blocks the Job, naming the paths
Given a Job whose Agent changed a file, and a commit on the base branch changing the same lines of that file
When `owl start` runs again for that Job
Then it exits with a non-zero code
And stderr names the conflicting file
And `owl jobs show <job>` reports the Job as `blocked`, with a reason naming that file
And no Run was started for that attempt

### S5 - a conflict leaves no rebase in progress
Given the blocked Job of S4
When its worktree is inspected with git
Then `git status` reports no rebase in progress
And the worktree is back on the Job's branch, at the commit the Agent left

### S6 - every conflicting path is named, not only the first
Given a Job whose Agent changed two files, and a commit on the base branch changing the same lines of both
When `owl start` runs again for that Job
Then the reason `owl jobs show` prints names both files

### S7 - the first Run has nothing to rebase
Given a registered Project and a pending Job that has never run
When `owl start` runs
Then it exits 0 and the Run proceeds
And the Job's branch points at the base branch's tip plus whatever the Agent did

### S8 - only the Job's branch is rebased
Given the Job of S1, a second branch pointing at one of the Job's own commits, and the Project configured to carry branches along with a rebase
When the second Run has started
Then the Project's own checkout is still on its base branch, at the same commit
And that second branch still points where it did
And the Project's working tree still holds its uncommitted file, unchanged

### S9 - rebasing never pushes anything
Given a Project whose remote is an empty bare repository, and the Job of S1
When the second Run has started
Then the bare repository holds no refs at all

### S10 - the base branch is fetched before the rebase
Given a Project cloned from a bare repository, and a Job whose first Run has ended, and a commit pushed to that bare repository since
When `owl start` runs again for that Job
Then the Project's `refs/remotes/origin/<base>` names that commit
And the Project's own base branch still points where it did, because Owl does not move it

### S11 - a Project with no remote rebases without complaint
Given the Job of S1 in a Project that has no remote at all
When `owl start` runs again
Then it exits 0 and the Run proceeds

### S12 - a remote that cannot be reached does not stop the Run
Given a Project whose remote points at a directory that does not exist, and a Job whose first Run has ended
When `owl start` runs again
Then it exits 0 and the Run proceeds
And the Job's branch is rebased onto the local base branch

### S13 - a worktree left mid-rebase is reported, not taken over
Given a Job whose worktree has a rebase in progress that nobody finished
When `owl start` runs for it
Then it exits with a non-zero code
And `owl jobs show <job>` reports the Job as `blocked`, saying a rebase is already in progress
And that rebase is still in progress, untouched

### S14 - a Job blocked by a conflict keeps its work
Given the blocked Job of S4
When the Project is inspected
Then the Job's worktree is still on disk and its branch still exists
And the commit the Agent made is still on that branch

### S15 - a worktree that is not on the Job's branch is not rebased
Given a Job whose worktree an Agent left checked out on another branch
When `owl start` runs for it
Then it exits with a non-zero code
And `owl jobs show <job>` reports the Job as `blocked`, saying the worktree is not on the Job's own branch
And that other branch was not rebased

### S16 - a conflict in changes nobody committed blocks the Job and keeps them
Given a Job whose worktree holds an uncommitted change, and a commit on the base branch changing the same lines
When `owl start` runs for it
Then it exits with a non-zero code
And the reason names the file and says the changes are in the repository's stash
And `git stash list` in the Project reports that stash

### S17 - a rebase that cannot be carried out blocks the Job and leaves no rebase in progress
Given a Job whose worktree holds a file nobody put in git's hands, and a commit on the base branch adding that same path
When `owl start` runs for it
Then it exits with a non-zero code
And `owl jobs show <job>` reports the Job as `blocked`, saying the rebase could not be carried out
And the worktree has no rebase in progress, and is back on the Job's branch

### S18 - a worktree that is not there any more blocks the Job rather than the queue
Given a Job whose worktree has been removed from disk
When `owl start` runs for it
Then it exits with a non-zero code
And `owl jobs show <job>` reports the Job as `blocked`, saying the worktree is not one git can work in
And a second `owl start` runs the next Job in the queue rather than failing on the same one

