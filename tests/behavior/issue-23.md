# Issue #23: Concurrency and FIFO skip-ineligible - global, Project and Account caps, min-wins, skip reasons, binding cap named

More than one Run at a time, safely (ADR-0021, ADR-0025).

Three caps, and the scheduler takes the minimum that applies: `maxParallelRuns`
in the daemon's own file, defaulting to one; `maxParallelRuns` in a Project's
`.coding-owl.yaml`, defaulting to one, so same-repo Runs never collide; and
`maxParallel` on an Account, defaulting to unlimited, because the ceiling
already governs burn rate.

The queue is scanned oldest-first and the first Job whose caps and Account
ceiling permit is started. A Job that is passed over keeps its position and Owl
records why, because a queue that skips is no longer a literal plan and the
ordering would otherwise look arbitrary (ADR-0025).

The scenarios drive the built `owl` binary against a daemon, with the stub agent
of issue #5 standing in for Claude Code, as the issue #11 and #19 scenarios do.
A Run is held open by a stub agent that waits, so that a scenario can see two of
them at once.

## Scenarios

### S1 - one Project runs one Run however high the global cap is
Given a daemon whose `maxParallelRuns` is 4 and one Project with two Jobs queued
When the machine goes idle
Then exactly one Run is in flight
And the Project's own cap is what `owl status` names as binding

### S2 - four Projects run four Runs when the global cap allows it
Given a daemon whose `maxParallelRuns` is 4, which looks at the machine every three seconds, and four Projects with one Job each
When the machine goes idle
Then four Runs are in flight, one per Project, within ten seconds - which is less than the four looks a watcher that started one Run per look would need
And `owl status` lists all four as running

### S3 - the global cap binds when there are more Projects than it allows
Given a daemon whose `maxParallelRuns` is 2 and three Projects with one Job each
When the machine goes idle
Then two Runs are in flight
And `owl status` names the global cap as what is holding the third back

### S4 - a Project that says its checks are hermetic runs two of its own Runs
Given a daemon whose `maxParallelRuns` is 4, one Project whose `maxParallelRuns` is 2 with three Jobs queued, and another that says nothing with two
When the machine goes idle
Then three Runs are in flight: two in the hermetic Project and one in the other
And each Project's passed-over Job names that Project's own cap, because reading one Project's file never answers for another's

### S5 - an ineligible Job at the head does not block a younger eligible one
Given two Projects, the older Job's Project holding its cap with a Run already in flight, and a younger Job in the other Project
When the scheduler looks again
Then the younger Job is started
And the older Job is still pending

### S6 - a Job that is passed over keeps its position
Given three Projects and a daemon that runs two Runs at once, so that one Job is passed over and another stays queued behind it
When `owl queue list` is read
Then each passed-over Job is at the position it had
And one that was queued after another is still behind it

### S7 - `owl status` says why each passed-over Job was passed over
Given a Job passed over for its Project's cap and a Job passed over for the global cap
When `owl status` is read
Then each is listed with its own reason
And each reason names the cap that stopped it

### S8 - a Job whose Account is over its ceiling is passed over, and says so
Given a Project on an Account held to a five-hour ceiling its Run reports past, and a second Project on an Account with headroom
When the machine goes idle
Then the Run that went past its ceiling is ended and its Job waits, while the other carries on
And the waiting Job is passed over, with a reason naming its Account and its ceiling

### S9 - an Account's own cap holds across Projects
Given an Account whose `maxParallel` is 1, two Projects on it, and a daemon whose `maxParallelRuns` is 4
When the machine goes idle
Then one Run is in flight
And the other Job is passed over for the Account's cap

### S10 - an Account with no cap of its own is held only by the others
Given two Projects on one Account, a daemon whose `maxParallelRuns` is 4 and no `maxParallel` on the Account
When the machine goes idle
Then two Runs are in flight

### S11 - concurrent Runs each stream their own log
Given two Runs in flight in different Projects
When `owl logs` is read for each of them
Then each carries only what its own Agent wrote

### S12 - pause freezes every Run in flight, and resume continues them all
Given two Runs in flight
When `owl pause` runs
Then both are reported paused
And `owl resume` continues both, and both finish

### S13 - somebody coming back to the machine freezes every Run in flight
Given two Runs the daemon started because the machine was idle
When somebody uses the machine again
Then both are frozen
And leaving again continues both

### S14 - owl start refuses when everything is at its cap, and says which
Given every cap taken by the Runs in flight
When `owl start` runs
Then it exits non-zero
And it names the cap that is binding - the narrowest one that applies, so that a person is told about the one nearest to them rather than one they may have to raise twice - rather than saying only that something is running

### S15 - a cap that is not one is refused
Given a daemon file whose `maxParallelRuns` is zero, negative, or not a number
When the daemon starts
Then it does not: a daemon whose own configuration cannot be read would run on numbers nobody wrote
And it names the setting and the value it refused

### S16 - the desktop app shows what is running and why the rest is not
Given the desktop app
When its sources are read
Then they render the Runs in flight, each passed-over Job, and the reason it was passed over

### S17 - a Project's cap that is not one refuses the Project rather than the daemon
Given a Project whose `.coding-owl.yaml` has a `maxParallelRuns` that is not one
When `owl project show` reads it
Then it exits non-zero, naming the file, the setting and the value
And the daemon is otherwise fine, because one Project's file is not everybody's

### S18 - nothing runs in parallel until somebody asks
Given a daemon file that says nothing about `maxParallelRuns`, and two Projects with a Job each
When the machine goes idle
Then one Run is in flight
And the other Job is passed over for Owl's own cap of one

### S19 - what will not clear itself is said out loud as well as listed
Given a Project on an Account whose window its Run reports past, on an idle machine
Then `owl status` says nothing is running and why, because a window with hours to run is not something looking again in a moment will change
And the same reason is in the passed-over list, which is where a Job's own reason lives

A cap that is taken is never on that line, because a cap can only be taken
while something is running, and the line is not printed then.

### S20 - a Project nobody can read is passed over, not a wall the rest queue behind
Given a Project whose `.coding-owl.yaml` Owl cannot read, with the oldest Job, and a second Project that is fine
When the machine goes idle
Then the Job in the Project that is fine runs
And the older one is passed over, with a reason saying its Project could not be read

### S21 - a failure to start is raised, not buried
Given a Project on an idle machine, and a worktree directory Owl cannot make
When the scheduler tries to start it
Then the daemon says so in its log at the level it prints by default, because a
Job that is waiting is what `owl status` is for and something that broke is not
