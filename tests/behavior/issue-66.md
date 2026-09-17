# Issue #66: a worktree looks clean because the repository hides untracked files

Owl decides whether a worktree holds work by running `git status --porcelain`
in it, and `git worktree remove` without `--force` is the second half of that
guarantee (ADR-0015: reclaiming disk must never destroy work silently).
Reported at commit `701b936`: both readings obey the repository's own
`status.showUntrackedFiles`, so in a repository that sets it to `no` a worktree
holding nothing but files git does not know about reads as clean. Confirmed by
reproduction on git 2.55.0: disposal and garbage collection deleted the
worktree and the untracked file in it without a word.

The fix is that Owl asks for untracked files wherever the answer decides
whether a worktree is deleted, rather than taking the repository's word for it.
`normal` is asked for rather than `all`: the question is only whether anything
is there, and it stops at an untracked directory instead of walking every file
inside one. A tracked file the user changed was never hidden and is unaffected.

What a repository deliberately ignores is still ignored - a `.gitignore` is the
user saying those files do not count, and Owl places its own worktree excludes
on that understanding (ADR-0033). Only the blanket "do not mention untracked
files at all" setting is overridden.

The git scenarios exercise that package's own API against a repository that
sets the config. The end-to-end scenarios drive the built `owl` binary against
a daemon, with the stub agent of issue #5 standing in for Claude Code, on a
Project whose repository sets it.

## Scenarios

### S1 - the clean check sees untracked work the repository hides
Given a repository with `status.showUntrackedFiles=no` and a worktree holding one file git does not know about
When Owl reads whether that worktree is clean
Then it reports the worktree as not clean

### S2 - removing a worktree refuses untracked work the repository hides
Given the same repository and worktree
When Owl removes that worktree without forcing it
Then the removal fails
And the untracked file is still on disk

### S3 - forcing still removes it
Given the same repository and worktree
When Owl removes that worktree with force
Then the worktree is gone

### S4 - a tracked file the user changed is still seen
Given a repository with `status.showUntrackedFiles=no` and a worktree with an edit to a committed file
When Owl reads whether that worktree is clean
Then it reports the worktree as not clean

### S5 - what the repository ignores is still ignored
Given a repository with `status.showUntrackedFiles=no` whose `.gitignore` covers `ignored.txt`, and a worktree holding only that file
When Owl reads whether that worktree is clean
Then it reports the worktree as clean

### S6 - accept refuses untracked work in such a Project
Given a running daemon and a Job in review whose Project sets `status.showUntrackedFiles=no`, with an untracked file in its worktree
When the user accepts the Job without forcing it
Then the command exits non-zero and says the worktree holds changes nobody has committed
And the untracked file is still on disk

### S7 - drop refuses untracked work in such a Project
Given the same daemon, Project and Job
When the user drops the Job without forcing it
Then the command exits non-zero
And the worktree and the untracked file are still on disk

### S8 - garbage collection reports such a worktree rather than reclaiming it
Given a running daemon and a Project that sets `status.showUntrackedFiles=no`, and a worktree whose Job is finished with holding an untracked file
When the user runs `owl gc`
Then the untracked file is still on disk
And the worktree is listed under unfinished work, saying what is uncommitted about it
