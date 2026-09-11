# Issue #11: Interruption: owl pause and resume, process-group freeze and release, SIGTERM escalation, requeue on daemon restart

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the daemon's `PATH` in place of
Claude Code. The XDG layout, the temporary repository and the stub are as
`tests/behavior/issue-5.md` describes them, and Jobs are queued with `--no-plan`
so that one `owl start` carries a Job into an execution Run.

The stub can start a child of its own that appends to a heartbeat file every few
milliseconds until it is stopped. That child is what makes a freeze observable
from the outside: freezing only the Agent would leave the child writing, which
is exactly what ADR-0011 says must not happen.

The grace window is the daemon's own setting, `graceWindow` in
`<config home>/coding-owl/config.yaml`, and defaults to fifteen minutes.

## Scenarios

### S1 - pause freezes the Agent and everything it started
Given a running daemon and a Run in progress whose Agent has started a child writing to a heartbeat file
When `owl pause` runs
Then it exits 0 and says which Run it froze
And the heartbeat file stops growing, and is still not growing a moment later
And the Run is still in progress, with no outcome

### S2 - resume continues the same Run
Given the frozen Run of S1
When `owl resume` runs
Then it exits 0 and names the same Run
And the heartbeat file grows again
And when the Agent finishes, that same Run is reported `succeeded` and its Job is in `review`

### S3 - owl status reports a frozen Run as frozen
Given the frozen Run of S1
When `owl status` runs
Then it reports the Run in progress as paused rather than running
And `owl jobs show` reports that Run as paused too
And after `owl resume` it reports it as running again

### S4 - pause with nothing running says so
Given a running daemon with no Run in progress
When `owl pause` runs
Then it exits with a non-zero code
And stderr says there is no run in progress

### S18 - stopping the daemon ends a Run that is not frozen
Given a running daemon and a Run in progress that nobody has frozen
When the daemon is sent SIGTERM
Then it exits within a few seconds
And that Run is `interrupted` and its Job `pending`
And the Agent and the child it started are both gone

### S19 - what the Agent started does not outlive its Run
Given a running daemon and a Job whose Agent starts a child and then exits of its own accord
When the Run has finished
Then the child is gone too, rather than left running with nobody watching it

### S20 - an Agent that will not stop when it is asked is killed
Given a running daemon whose `graceWindow` is a second, and a frozen Run whose Agent ignores being asked to stop
When the window has passed and a few seconds more
Then the Run ends `interrupted` and its Job is `pending`
And the Agent and the child it started are both gone
And `owl start` can begin a new Run

### S16 - a Run being verified is not reported as one that is not there
Given a running daemon and a Job whose Project configures a check that takes a few seconds, whose Agent has exited
When `owl pause` runs while the checks are running
Then it exits with a non-zero code
And stderr says that Run is being verified rather than saying there is no run at all
And `owl resume` says the same, in the words of the verb that was asked for

### S17 - what an earlier daemon was carrying out is queued again, however it died
Given a database in which a Job is `active` and its Run was already recorded as interrupted, which is a daemon that died halfway through recovering
When a daemon starts
Then that Job is `pending`
And a Job that was left in `review` is still in `review`

### S5 - resume with nothing frozen says so
Given a running daemon and a Run in progress that is not frozen
When `owl resume` runs
Then it exits with a non-zero code
And stderr says the run is not paused

### S6 - pausing a Run that is already frozen says so
Given the frozen Run of S1
When `owl pause` runs again
Then it exits with a non-zero code
And stderr says the run is already paused
And the Run is still frozen, and resuming it still works

### S7 - the grace window ends a frozen Run
Given a running daemon whose `graceWindow` is a second, and a frozen Run
When the window passes
Then the Run ends `interrupted` within a few seconds
And its Job is `pending` again
And the heartbeat file is not growing, and the Agent and the child it started are both gone

### S8 - what a terminated Run leaves behind is kept
Given the Job of S7
When it is inspected
Then its worktree is still on disk and its branch still exists
And the commit the Agent made before it was frozen is still on that branch

### S9 - the next Run continues from the handoff
Given the Job of S7, whose Agent wrote a handoff before it was frozen
When `owl start` runs again
Then a new Run begins for the same Job, with a different Run id
And the Agent's prompt holds what the handoff said
And the Job keeps the branch and worktree it already had

### S10 - the grace window is configurable
Given a daemon whose `graceWindow` is `2s`, and a frozen Run
When one second has passed
Then the Run is still in progress
And after the window has passed the Run is `interrupted`
And it ended no sooner than two seconds after the pause, and not materially later: the window that fired is the one that was configured

### S11 - a grace window that is not a duration is refused
Given a Run in progress, and a daemon configuration that then sets `graceWindow` to `soon`
When `owl pause` runs
Then it exits with a non-zero code
And stderr names the configuration file and the setting
And the Run is still running, not frozen

### S12 - resuming inside the window keeps the same Run
Given a daemon whose `graceWindow` is 30 seconds, and a Run frozen for a moment
When `owl resume` runs before the window passes
Then the Run id is the one that was frozen
And the Run ends `succeeded`, so the window did not end it

### S13 - a daemon restart ends the Runs that were going
Given a running daemon and a Run in progress
When the daemon is killed outright, so nothing records how that Run ended, and a daemon is started again
Then that Run is reported `interrupted`
And its Job is `pending` again
And `owl start` begins a new Run for it

### S14 - stopping the daemon while a Run is frozen does not hang
Given a running daemon and a frozen Run
When the daemon is sent SIGTERM
Then it exits within a few seconds
And the heartbeat file is not growing, and the Agent and the child it started are both gone
And the Run is `interrupted` and its Job `pending`

### S15 - pause and resume never touch the daemon itself
Given a running daemon and a frozen Run
When `owl pause` and `owl resume` have run
Then `owl daemon status` still answers throughout
