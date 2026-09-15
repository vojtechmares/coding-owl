# Issue #101: An Agent killed by a signal is recorded as exiting with status -1

When an Agent is killed by a signal Owl did not send - the OOM killer, a crash,
somebody running `kill -9` on it - the host Executor reports the exit status Go
gives such a process, which is `-1`. That is also the status Owl records for a
Run whose Agent never had one, so the Job is blocked with the reason `the agent
exited with status -1` and nothing says the Agent was killed, let alone by what.
Found while reviewing issue #5 against `main`.

The fix is that the Executor, which owns the process (ADR-0012), reports the
signal an Agent was killed by, and a Run whose Agent was killed by one that Owl
did not send fails with a reason that names it. A Run Owl ends itself - the
daemon stopping, or the grace window passing (ADR-0011, ADR-0034) - is still
`interrupted`, whatever signal its Agent finally died of.

The scenarios drive the built `owl` binary against a daemon, with the stub agent
of issue #5 standing in for Claude Code. The stub learns two things for this:

- `$OWL_FAKE_CLAUDE_SIGNAL` names a signal, such as `SIGKILL`, that the stub
  kills itself with instead of exiting, so it ends the way a killed Agent does:
  without an exit status of its own;
- `$OWL_FAKE_CLAUDE_STDERR` is a line the stub writes to standard error just
  before it exits or kills itself, so a scenario can see what an Agent said
  follow the reason. It never names a signal, so a reason that names one got
  the name from Owl.

"The EXIT column" below is the Run's row in the runs table `owl jobs show`
prints, and `(none)` there means no exit status was recorded.

## Scenarios

### S1 - an Agent killed by a signal fails its Run and blocks its Job
Given a running daemon, a pending Job, and a stub agent that kills itself with `SIGKILL`
When `owl start` runs and the Run finishes
Then `owl jobs show <job-id>` reports the Job's state as `blocked`
And it reports one Run whose outcome is `failed`

### S2 - the reason names the signal and claims no exit status
Given a running daemon, a pending Job, and a stub agent that writes a line to standard error and then kills itself with `SIGKILL`
When `owl start` runs and the Run finishes
Then the reason `owl jobs show <job-id>` prints names `SIGKILL`
And it does not say the agent exited with a status, and does not mention `-1`
And the line the stub agent wrote to standard error follows the signal's name

### S3 - an Agent killed by a signal has no exit status recorded
Given a running daemon, a pending Job, and a stub agent that kills itself with `SIGKILL`
When `owl start` runs and the Run finishes
Then the EXIT column of its Run in `owl jobs show <job-id>` is `(none)`

### S4 - a signal Owl did not send is a failure, even one Owl sends itself
Given a running daemon, a pending Job, and a stub agent that kills itself with `SIGTERM`
When `owl start` runs and the Run finishes
Then `owl jobs show <job-id>` reports the Job as `blocked` and its one Run as `failed`
And the reason names `SIGTERM`, not `SIGKILL`
And the EXIT column of its Run is `(none)`

### S5 - a Run the daemon stopping ends is still interrupted
Given a running daemon, a pending Job, and a Run in progress whose stub agent is waiting for its release file
When the daemon is sent SIGTERM, and a daemon is started again
Then `owl jobs show <job-id>` reports that Run as `interrupted`, with the EXIT column `(none)`
And the Job is `pending` again
And the reason says the daemon stopped, and names no signal

### S6 - a Run the grace window ends is still interrupted, even when its Agent has to be killed
Given a running daemon whose `graceWindow` is a second, and a frozen Run whose stub agent ignores being asked to stop
When the window has passed and the Agent has been killed (ADR-0034)
Then `owl jobs show <job-id>` reports that Run as `interrupted`, with the EXIT column `(none)`
And the Job is `pending` again
And the reason says the grace window passed, and names no signal

### S7 - an Agent that exits non-zero is reported as before
Given a running daemon, a pending Job, and a stub agent that writes a line to standard error and then exits 3
When `owl start` runs and the Run finishes
Then `owl jobs show <job-id>` reports the Job as `blocked` and its one Run as `failed`
And the EXIT column of its Run is `3`
And the reason is `the agent exited with status 3`, followed by the line the stub agent wrote to standard error
