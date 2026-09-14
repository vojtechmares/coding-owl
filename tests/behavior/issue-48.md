# Issue #48: A Job cancelled while its Run is starting is resurrected as active and then lost

Starting a Run is a scan of the queue, then minutes of slow work - the
worktree, the rebase, the Skills, the Project's setup commands - and only then
a Run row and an unconditional move of the Job to `active`. Reported at commit
`701b936`: a `queue remove` that lands in that window cancels the Job, and the
start then overwrites the cancellation. The Job is `active` with no position,
counted by nothing, invisible to `owl queue list`, stuck when its Run ends
because leaving the queue needs a position, and put back in the queue by the
next daemon. Reordering and disposal have the same read-then-write shape.

The fix is that the Job is claimed - moved from `pending` to `active` on the
condition that it still is pending - before any slow work, that every later
move of a Job says which state it moves from, and that a claim that finds the
Job already decided about gives the Job up without a Run or a worktree.

The store scenarios exercise the store's own API. The run scenarios drive the
`run` package against a real store and repository with the fake Driver and
Executor of the existing run tests. The end-to-end scenarios drive the built
`owl` binary against a daemon, with the stub agent of issue #5 standing in for
Claude Code and a setup command that sleeps standing in for slow work.

## Scenarios

### S1 - a Job moves state only from the state the move names
Given a Job in each of the states `pending`, `active`, `blocked`, `review`, `done`, `cancelled` and `exhausted`
When it is moved from state `from` to another state, for every pair of states
Then the move applies exactly when the Job's state is `from`
And a Job whose state is not `from` is left in the state it was in

### S2 - a Job leaves the queue only from the state the departure names
Given a pending Job in the queue
When it is dequeued as leaving from `active`
Then the store refuses with "not in the queue"
And the Job is still `pending` at the position it had

### S3 - a Job is moved in the queue only from the state the move names
Given a pending Job in the queue that has been claimed as `active`
When it is moved to another position as a `pending` Job
Then the store refuses with "not in the queue"
And the Job keeps its position

### S4 - a Job cancelled between being picked and being claimed gets no Run and no worktree
Given a pending Job, and a Driver whose check cancels that Job while the start is deciding whether it can run
When the Run is started
Then the start reports that nothing started, without an error
And the Job has no Run
And the Job is `cancelled`
And no worktree directory exists for the Job

### S5 - a Job cancelled before its claim is not put back in the queue on restart
Given the Job of S4
When a daemon recovers what an earlier daemon left behind
Then the Job is still `cancelled`
And it is not in the queue

### S6 - a Job being set up cannot be cancelled
Given a running daemon, a registered Project whose setup command sleeps, and a pending Job
When `owl start` is running the setup command and `owl queue remove <job>` is run
Then `owl queue remove` exits non-zero and says the Job is not pending, it is active
And when the setup command ends the Run goes on, and the Job ends in `review`

### S7 - a Job being set up cannot be reordered
Given the Job of S6, while its setup command is running
When `owl queue reorder <job> 1` is run
Then it exits non-zero and says the Job is not pending, it is active

### S8 - a Job whose slow work is refused is pending again, not active
Given a pending Job in a Project that declares a Skill nobody can fetch
When the Run is started
Then the start reports the refusal
And the Job is `pending` at the position it had
And the daemon does not report itself as carrying the Job
