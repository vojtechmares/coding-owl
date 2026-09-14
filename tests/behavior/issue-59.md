# Issue #59: gc.prune prunes the user's own worktrees

Garbage collection (ADR-0015) prunes the administrative entries of worktrees
whose directories are gone, so git stops counting a worktree nobody can use.
Reported at commit `701b936`: stale detection listed every linked worktree of
the Project, and pruning ran an unqualified `git worktree prune`, so a user's
own worktree - on an unmounted volume, say - had its entry taken away by Owl.
Disposing of a Job whose worktree was already gone pruned the same way.

The fix is that only worktrees under the Project's Owl worktree directory are
considered stale or pruned, and that pruning forgets those entries and no
other. Worktrees elsewhere are left untouched even when their directory is
missing. ADR-0015 is amended to say so. Reclaiming Job branches is out of
scope.

The scenarios drive the `gc`, `run` and `git` packages against a real
repository, as the existing tests of those packages do.

## Scenarios

### S1 - only Owl's stale worktree entry is pruned
Given a Project with a Job whose worktree is under the Owl worktree directory, and a worktree the user made elsewhere, both of whose directories have been removed
When a collection runs
Then git no longer counts the Job's worktree
And git still counts the user's worktree
And the report names the Project as pruned

### S2 - nothing is pruned when only the user's worktrees are stale
Given a Project with a Job whose worktree is in place, and a worktree the user made elsewhere whose directory has been removed
When a collection runs
Then git still counts the user's worktree
And the report names nothing as pruned

### S3 - disposing of a Job whose worktree is gone leaves the user's worktrees alone
Given a Job in review whose worktree directory has been removed, and a worktree the user made elsewhere whose directory has also been removed
When the Job is accepted
Then the Job is `done` and git no longer counts its worktree
And git still counts the user's worktree

### S4 - forgetting one worktree forgets that one, whatever state it is in
Given a repository with one worktree whose directory has been removed, one left with its directory but without the file linking it to the repository, and one whose directory is in place
When the first two are forgotten one by one
Then git no longer counts either of them
And git still counts the one in place
And forgetting the one in place is refused
