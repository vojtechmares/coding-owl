# Issue #19: Idle: darwin Detector, configurable policy, Idle-gated scheduling, auto-freeze on return with grace window

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on its `PATH` in place of Claude
Code, and a Project whose configuration is committed on its base branch, as
`tests/behavior/issue-5.md` and `tests/behavior/issue-7.md` describe them. The
last one drives the desktop app's Go side in-process, as
`tests/behavior/issue-9.md` describes.

Owl works only when the machine is otherwise idle (ADR-0011). The idle
`Detector` reads the machine on darwin by asking the system how long it has
been since any keyboard or mouse input and whether it is on AC power; both
scenarios below and a person read the same numbers. The stub machine stands
where the system's own `ioreg` and `pmset` do, first on the daemon's `PATH`,
and a scenario changes what the machine says by writing the file the stub
reads - which is how a scenario steps away from the machine and comes back to
it.

"Idle" below means what the policy says it means: by default no input for ten
minutes and the machine on AC power. "In use" means the machine is not Idle,
whichever half of the policy refuses it.

## Scenarios

### S1 - a machine in use starts nothing
Given a queued Job, and a machine that had input a minute ago while on AC power
When the daemon has had time to look at the machine several times
Then no Run has started and the Job is still pending

### S2 - a machine on battery starts nothing, however long it has been idle
Given a queued Job, and a machine idle for an hour and running on battery
When the daemon has had time to look at the machine several times
Then no Run has started and the Job is still pending

### S3 - a machine idle long enough on AC power starts a Run with nobody asking
Given a queued Job, and a machine idle for eleven minutes on AC power
When the daemon looks at the machine
Then a Run starts without `owl start` being called
And the Job runs to completion as it would have if somebody had asked

### S4 - the policy is what the configuration says
Given global configuration with an `idle` policy of one second and no power requirement, a queued Job, and a machine idle for two seconds on battery
When the daemon looks at the machine
Then a Run starts, because the policy this machine is held to is the configured one

### S5 - `owl start` works whatever the machine is doing
Given a queued Job and a machine in use
When `owl start` is called
Then a Run starts, and it is not frozen by the machine being in use

### S6 - coming back to the machine freezes the Run in flight
Given a Run the daemon started because the machine was idle
When the machine reports input again
Then the Run is reported as paused, and its Agent's process group has been stopped
And the Job is still the one in progress rather than queued again

### S7 - going away again within the grace window continues the same Run
Given a Run frozen because the machine came back into use
When the machine goes idle again inside the grace window
Then the same Run continues, in the same Run, and is no longer reported as paused
And the Job finishes without a second Run being started

### S8 - the grace window passing ends the Run and queues the Job again
Given global configuration with a grace window of a second, and a Run frozen because the machine came back into use
When the grace window passes with the machine still in use
Then the Run ends as `interrupted` and its Job is pending again
And the Job's branch and worktree are still there, so the next Run carries on in place

### S9 - `owl pause` is not undone by the machine going idle
Given a Run the user paused with `owl pause`, on a machine in use
When the machine goes idle again
Then the Run is still paused, because the user's decision is not the machine's to reverse
And `owl resume` continues it
And a Run the machine froze, which the user then asked to pause as well, is not continued either

### S10 - `owl status` reports the machine as the system reports it
Given a machine idle for twelve minutes on AC power
When `owl status` is run
Then it says the machine is idle, how long it has been without input, and that it is on AC power
And with the machine in use on battery it says both of those instead

### S11 - `owl status` says why nothing is running
Given a queued Job and a machine in use
When `owl status` is run
Then it says nothing is running because the machine is in use
And with the machine idle and nothing queued it says nothing is running because nothing is queued
And with the machine idle and a Job that cannot run, it says what refused it rather than blaming the machine

### S12 - a machine Owl cannot read starts nothing, and says so
Given a queued Job and a machine whose idleness cannot be read
When the daemon has had time to look at the machine several times
Then no Run has started, and `owl status` says the machine could not be read
And what the tool said on its way out reaches the terminal as text rather than as an instruction to it
And a Run already in flight is left alone rather than frozen on a reading nobody got

### S13 - a policy changed while the daemon is running is the one it holds the machine to
Given a running daemon, a queued Job, and a machine idle for two seconds on battery, which the default policy refuses
When the global configuration is given an `idle` policy that machine satisfies
Then a Run starts without the daemon being restarted, as a changed grace window takes effect without one

### S14 - the next Job starts as soon as the one before it has ended
Given two queued Jobs and a machine that stays idle throughout
When the first Run ends
Then the second Run starts without waiting on anything
And while the first is still going, nothing is reported as holding work back: a Run in progress is work happening, not work refused

### S15 - a Run that starts as the machine comes back into use is frozen anyway
Given a Project whose setup takes a moment, a queued Job, and a machine that has gone idle
When the machine comes back into use while the Run is still starting
Then the Run is frozen as soon as it has started, rather than left going on a machine somebody is at

### S16 - freezing does not depend on reading the configuration
Given a Run the daemon started on an idle machine, and a global configuration that has since stopped parsing
When the machine comes back into use
Then the Run is frozen anyway, on the grace window Owl falls back to
And `owl pause` still refuses, naming the file, because a person can be told and can fix it

### S17 - stopping the daemon while a Run is being got ready does not hang
Given a Project whose setup takes half a minute, and a machine that has gone idle
When the daemon is stopped while that setup is running
Then it stops within the few seconds every other way of stopping it takes

### S18 - the app shows whether the machine is idle
Given the desktop app on a daemon whose machine is idle
When the app is asked for the overview
Then it carries the machine's idle state, how long it has been without input, and its power state
And the frontend sources render that state in the window
