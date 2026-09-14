# Issue #50: On daemon stop and verifier timeout, SIGKILL reaches only the Agent's pid, not its group

ADR-0034 says a Run being ended by a daemon that is stopping escalates from
`SIGTERM` on the process group to `SIGKILL` on the process group, and that this
was already the executor's behaviour. Reported at commit `701b936`: it was not.
The host executor and the shell runner both left cancellation to `os/exec`,
which after the grace period kills the one pid it started and nothing else. A
grandchild that ignores `SIGTERM` - a stuck test runner - survived the daemon
stopping, and survived a Verification check timing out.

The fix is that the executor and the shell runner own their own cancellation:
`SIGTERM` to the group, the grace period of five seconds, then `SIGKILL` to the
group. The grace period itself is not changed (out of scope), so "within the
grace period plus a margin" below means within eight seconds.

The executor and shell scenarios exercise those packages' own APIs with shell
scripts standing in for an Agent and a check. The end-to-end scenario drives
the built `owl` binary against a daemon with the stub agent of issue #5, which
learns that when it is told to ignore `SIGTERM`, the child it starts ignores it
too: an Agent that will not stop when asked is rarely alone in that.

## Scenarios

### S1 - a cancelled Agent's child that ignores SIGTERM is killed within the grace period
Given an Agent started by the host executor that starts a child ignoring `SIGTERM`, records the child's pid, and waits
When the executor's context is cancelled
Then the child is gone within the grace period plus a margin
And Wait returns within that time

### S2 - a cancelled Agent that stops when asked is not made to wait for the kill
Given an Agent started by the host executor that exits on `SIGTERM`
When the executor's context is cancelled
Then Wait returns well before the grace period ends

### S3 - a timed-out check's child that ignores SIGTERM is killed within the grace period
Given a shell command that starts a child ignoring `SIGTERM`, records the child's pid, and waits past its timeout
When the command is run with a short timeout
Then the run returns as timed out within the grace period plus a margin of the timeout
And the child is gone by then

### S4 - a timed-out check that stops when asked returns at its timeout
Given a shell command that sleeps and exits on `SIGTERM`
When the command is run with a short timeout
Then the run returns as timed out well before the grace period ends

### S5 - stopping the daemon kills what an Agent started, even when neither will stop when asked
Given a running daemon and a Run in progress whose Agent ignores `SIGTERM` and has started a child that ignores it too
When the daemon is sent `SIGTERM`
Then the daemon exits cleanly
And the Agent and its child are both gone within the grace period plus a margin
