# Issue #62: End-of-run ErrNotQueued is swallowed

When a Run ends, its Job is moved on: to review, back into the queue, or
blocked (ADR-0011, ADR-0013). Reported at commit `701b936`: when that move
found the Job no longer in the queue - somebody had decided about it while the
Run was going - the error was logged and the Run ended as if nothing were
wrong. The Job was left as the decision left it, which is right; the Run said
nothing about it, which is not.

The fix is that a Run whose Job could not be moved on records that as its
outcome: the Run is `failed`, its reason names the Job and what the store
said, and the daemon's log line for the Run's end says so, consistent with how
a Run that could not be started is raised. A Run that had already failed keeps
its own reason and adds this one. The Job itself is left as it was found. The
cancel race that was the main way to reach this is issue #48 and out of scope.

The scenarios drive the `run` package against a real repository with the fake
Driver and Executor of the existing run tests. What the user reads is the Run's
outcome and reason, through `owl jobs show` and `owl runs`, and the daemon's
log.

## Scenarios

### S1 - a Run whose Job was decided about while it ran records that it could not move the Job on
Given a Run in progress whose Agent will exit cleanly, and a Job that somebody moved out of the queue while the Run was going
When the Agent exits and the Run ends
Then the Run's outcome is `failed`
And the Run's reason names the Job and says it is not in the queue
And the Job is left in the state the decision left it

### S2 - a Run that had already failed keeps its reason and adds that the Job could not be moved
Given a Run in progress whose Agent will exit non-zero, and a Job that somebody moved out of the queue while the Run was going
When the Agent exits and the Run ends
Then the Run's outcome is `failed`
And the Run's reason names the Agent's exit status
And the Run's reason also says the Job is not in the queue

### S3 - the daemon's log says the Run could not move its Job on
Given a Run in progress whose Job somebody moved out of the queue while the Run was going
When the Run ends
Then the daemon's log line for the Run's end reports the outcome `failed` and a reason saying the Job is not in the queue
