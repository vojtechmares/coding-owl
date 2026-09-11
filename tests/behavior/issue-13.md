# Issue #13: TTL: per-Job attempt limit, --ttl, owl jobs extend, exhausted state

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the daemon's `PATH` in place of
Claude Code. The XDG layout, the temporary repository and the stub are as
`tests/behavior/issue-5.md` describes them.

A Job's TTL is the number of Runs it may still take (ADR-0025). It counts Runs,
not failures: a Run that succeeded spends one exactly as a Run that was
interrupted does. `owl jobs show` reports what a Job has left.

## Scenarios

### S1 - a Job is queued with ten attempts
Given a running daemon and a registered Project
When `owl add "work"` runs
Then `owl jobs show <job>` reports ten attempts left

### S2 - owl add --ttl says how many attempts a Job gets
Given a running daemon and a registered Project
When `owl add "work" --ttl 3` runs
Then `owl jobs show <job>` reports three attempts left

### S3 - a TTL that is not a number of attempts is refused
Given a running daemon and a registered Project
When `owl add "work" --ttl 0`, `--ttl -1` and `--ttl many` each run
Then each exits with a non-zero code
And each stderr says what it wanted instead
And no Job was queued

### S4 - a Run that succeeded spends an attempt
Given a running daemon and a Job with three attempts whose Agent exits cleanly
When the Run has finished
Then `owl jobs show <job>` reports two attempts left

### S5 - a Run that failed spends an attempt too
Given a running daemon and a Job with three attempts whose Agent exits non-zero
When the Run has finished
Then the Job is `blocked`
And it reports two attempts left

### S6 - a Run that was interrupted spends an attempt
Given a running daemon and a Job with three attempts whose Run is interrupted by the daemon stopping
When a daemon is running again
Then the Job is `pending`
And it reports two attempts left

### S7 - a Job that runs out of attempts is exhausted rather than pending
Given a running daemon and a planned Job with one attempt left
When its planning Run has finished
Then `owl jobs show <job>` reports the Job as `exhausted` with no attempts left
And it is not `blocked`, because nothing is wrong with the work

### S8 - an exhausted Job is never scheduled
Given the exhausted Job of S7 and a second Job queued behind it
When `owl start` runs
Then it starts a Run for the second Job
And the exhausted Job is still `exhausted`

### S9 - owl jobs extend returns an exhausted Job to the queue
Given the exhausted Job of S7
When `owl jobs extend <job>` runs
Then it exits 0 and says how many attempts the Job now has
And `owl jobs show <job>` reports the Job as `pending` with ten attempts left
And `owl start` then starts a Run for it

### S10 - owl jobs extend adds to what is left rather than replacing it
Given a running daemon and a Job with two attempts left that is not exhausted
When `owl jobs extend <job> --ttl 5` runs
Then the Job has seven attempts left
And its state is unchanged

### S11 - extending refuses a number that is not attempts to add
Given a running daemon and a Job
When `owl jobs extend <job> --ttl 0` and `--ttl -3` each run
Then each exits with a non-zero code
And the Job's attempts are unchanged

### S12 - extending refuses a Job that is not there
Given a running daemon
When `owl jobs extend 999` runs
Then it exits with a non-zero code
And stderr names `999`

### S13 - owl status tells exhausted from blocked
Given a running daemon with one exhausted Job and one blocked Job
When `owl status` runs
Then it counts one Job in each of those states, separately
And it lists the exhausted Job with its Project and its prompt, apart from the blocked one

### S14 - an exhausted Job keeps its place in the queue
Given a queue holding a Job that becomes exhausted and a Job queued after it
When the first is extended
Then `owl queue list` shows it ahead of the second again

### S15 - a Job that finishes with attempts left is not exhausted
Given a running daemon and a Job with three attempts whose Run ends cleanly
When the Run has finished
Then the Job is `review`, not `exhausted`

### S16 - what a Job has left survives the daemon restarting
Given a Job that has spent an attempt
When the daemon is stopped and started again
Then `owl jobs show <job>` reports the same number of attempts left
