# Behavior spec sheet - issue #133

Queue items should support a blocked-by dependency, surfaced in the agent's
prompt and honoured by the scheduler.

A Job may name one other Job it waits for, by that Job's numeric id. The
scheduler passes the dependent Job over until the Job it waits for is `done`,
exactly as it passes over a Job whose Project is at its cap: the Job keeps its
queue position and the next runnable Job is started instead. When the dependent
Job does run, its Agent is told what it was queued behind.

Terms are `CONTEXT.md`'s: Job, Run, Agent, Project, queue, position. A Job that
is passed over for a dependency is `pending`, not `blocked` - `blocked` is the
state of a Job stuck on something wrong with its work (ADR-0025), and nothing
is wrong with a Job that is merely waiting its turn. The skip reason therefore
says the Job *waits for* another, and names that Job's state, as the issue
asked.

Each scenario is observable from outside: the exit code and output of an `owl`
command, or the argv the Agent was invoked with.

## S1 - a Job is queued behind another by its Job id

**Given** a registered Project with a Job already queued against it

**When** `owl add "..." --blocked-by <that job's id>` is run

**Then** it exits 0 and says which Job the new one waits for, and
`owl jobs show <new id>` reports that it is waiting for that Job.

## S2 - a dependency on a Job that is not there is refused

**Given** a registered Project and a queue with no Job numbered 999

**When** `owl add "..." --blocked-by 999` is run

**Then** it exits non-zero, says there is no job 999 to wait for, and nothing
is queued: `owl queue list` holds exactly what it held before.

## S3 - a dependency that is not a Job id is refused

**Given** a registered Project

**When** `owl add "..." --blocked-by <0, -1 or abc>` is run

**Then** each exits non-zero and nothing is queued. `0` and `-1` are refused
with "job ids count from one"; `abc` is not a number at all and is refused by
the flag itself, naming `--blocked-by`.

## S4 - the scheduler passes over a Job whose dependency is not done

**Given** two Projects, each free to run, a Job queued against the first and a
Job queued against the second that waits for the first

**When** the machine goes idle and Owl starts what it can

**Then** only the Job that waits for nothing is running, and the dependent Job
is in `owl status`'s passed-over list.

## S5 - `owl status` says which Job is waited for, and its state

**Given** the arrangement of S4, with the blocking Job's Run in flight

**When** `owl status` is read

**Then** the dependent Job's reason in the passed-over table names the blocking
Job's id and the state that Job is in.

## S6 - a passed-over Job keeps its queue position

**Given** a Job that waits for another, and a third Job queued after it whose
own Project is free

**When** the machine goes idle and Owl starts what it can

**Then** the dependent Job is still pending, still at the position it had, and
still ahead of the Job queued after it.

## S7 - `review` is not `done`; accepting the blocking Job clears the wait

**Given** a Job that waits for another, and the blocking Job finished and
waiting for a decision

**When** `owl status` is read, then the blocking Job is accepted and `owl start`
is run

**Then** before the decision the dependent Job is passed over with `review` in
its reason, and after `owl jobs accept` the dependent Job is the one `owl start`
starts.

## S8 - `owl start` refuses for the same reason and names the Job waited for

**Given** a queue holding only a Job that waits for a Job that is not done

**When** `owl start` is run

**Then** it exits non-zero and says the Job waits for that Job, naming its id.

## S9 - a dependency nothing will clear is said out loud, not polled for

**Given** a queue holding only a Job that waits for a Job that has been
cancelled

**When** `owl status` is read

**Then** its "nothing is running" line says the Job waits for that Job, and the
same reason is in the passed-over list. A reason Owl expects to clear itself is
not reported there, so a dependency reported on that line is one the watcher
backs off from rather than looking for again every few seconds (ADR-0011).

## S10 - the Agent is told what its Job was queued behind

**Given** a Job that waits for another, and the blocking Job accepted so that
the dependent Job can run

**When** the dependent Job's Agent is invoked

**Then** the prompt it is given carries its own work, the blocking Job's id, the
state that Job is in, and that Job's prompt, and the quoted prompt is fenced off
from Owl's own words so that it cannot be read as instructions.

## S11 - a Job with no dependency is untouched

**Given** a Job added without `--blocked-by`

**When** the machine goes idle

**Then** the Job runs as it did before, and `owl jobs show` says nothing about
waiting for another Job.
