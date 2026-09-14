# Issue #53: The pre-Run fetch never feeds the rebase

Before every Run, Owl fetches the Project's base branch and rebases the Job's
branch onto it (ADR-0016). Reported at commit `701b936`: the fetch updates the
remote-tracking ref, `refs/remotes/<remote>/<base>`, while the rebase replays
onto the local branch, `refs/heads/<base>`. Nothing connects the two, so a Run
rebases onto whatever the user last pulled, and the fetch changes nothing.

The fix is that the rebase replays onto the remote-tracking ref when the
fetch succeeded, and onto the local base branch when the Project has no
remote or the fetch failed. The local base branch is never moved by Owl (out
of scope), and `branch.<base>.merge` is not consulted (out of scope).

The git scenarios exercise that package's own API. The run scenario drives the
`run` package against a real repository with the fake Driver and Executor of
the existing run tests. The end-to-end scenarios drive the built `owl` binary
against a daemon, with the stub agent of issue #5 standing in for Claude Code,
on the Project of issue #12 that tracks a bare repository somebody else pushes
to.

## Scenarios

### S1 - the fetch reports the ref it updated
Given a repository whose base branch tracks a remote
When the base is fetched
Then the fetch reports `refs/remotes/<remote>/<base>` as the ref it updated
And a repository with no remote reports no ref

### S2 - a commit pushed to the remote since the clone is under the Job branch after a Run
Given a Project cloned from a bare repository, a pending Job, and a commit somebody else pushed to the bare repository's base branch since the clone
When the Job is run
Then the pushed commit is an ancestor of the Job's branch
And the Project's own base branch still points where it did

### S3 - the same, end to end
Given a Project cloned from a bare repository, a Job whose first Run has ended, and a commit pushed to the bare repository since
When `owl start` runs again for that Job
Then the pushed commit is an ancestor of the Job's branch
And the Project's own base branch still points where it did

### S4 - a Project with no remote rebases onto the local base
Given a Project with no remote, a Job whose first Run has ended, and a commit added to the local base branch since
When `owl start` runs again for that Job
Then the local base branch's tip is an ancestor of the Job's branch
And the local base branch's tip is unchanged

### S5 - ADR-0016 names the ref the rebase targets
Given the decision record for rebasing at the start of every Run
When it is read
Then it names `refs/remotes/<remote>/<base>` as what the rebase targets when the fetch succeeded
And `refs/heads/<base>` as what it targets otherwise
