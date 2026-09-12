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
Given a daemon whose `maxParallelRuns` is 4 and four Projects with one Job each
When the machine goes idle
Then four Runs are in flight, one per Project
And `owl status` lists all four as running

### S3 - the global cap binds when there are more Projects than it allows
Given a daemon whose `maxParallelRuns` is 2 and three Projects with one Job each
When the machine goes idle
Then two Runs are in flight
And `owl status` names the global cap as what is holding the third back

### S4 - a Project that says its checks are hermetic runs two of its own Runs
Given a daemon whose `maxParallelRuns` is 4 and one Project whose `maxParallelRuns` is 2, with three Jobs queued
When the machine goes idle
Then two Runs are in flight, both in that Project
And the third Job is passed over for the Project's cap

### S5 - an ineligible Job at the head does not block a younger eligible one
Given two Projects, the older Job's Project holding its cap with a Run already in flight, and a younger Job in the other Project
When the scheduler looks again
Then the younger Job is started
And the older Job is still pending

### S6 - a Job that is passed over keeps its position
Given the queue of S5 after the younger Job was started
When `owl queue list` is read
Then the older Job is still ahead of anything queued after it
And its position is the one it had

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
And it names the cap that is binding - the narrowest one, so that raising the one it names is what changes the answer - rather than saying only that something is running

### S15 - a cap that is not one is refused
Given a daemon file whose `maxParallelRuns` is zero, negative, or not a number
When the daemon starts
Then it does not: a daemon whose own configuration cannot be read would run on numbers nobody wrote
And it says which setting it refused

### S16 - the desktop app shows what is running and why the rest is not
Given the desktop app
When its sources are read
Then they render the Runs in flight, each passed-over Job, and the reason it was passed over
