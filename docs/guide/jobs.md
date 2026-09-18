# Working with Jobs

A Job is a standing intent to do one piece of work in one Project: the prompt,
its configuration and its lifecycle. This page is the day-to-day of them -
queueing, ordering, watching a Run, and deciding what to keep. If you have not
queued one yet, [Getting started](getting-started.md) is the shorter road in.

## Queueing a Job

```
owl add "Add a --json flag to the status command"
```

Run inside a registered Project and the Job is queued against that Project;
anywhere else, name one:

```
owl add "Rotate the log files nightly" --project my-app
```

The Job goes behind everything already waiting, and Owl prints its id and its
position. A Job is planned before it is carried out, in a Run of its own, and
the plan becomes the first Handoff on the Job's branch. Work that needs no
thinking through first skips that Run:

```
owl add "Bump the Go version in the toolchain line" --no-plan
```

Three more flags are worth knowing. `--model` and `--effort` decide, for this
Job alone, what every phase of it runs as, over what the Project or Owl would
otherwise choose. `--ttl` is how many Runs the Job may take before it is
exhausted; it gets ten unless you say otherwise.

```
owl add "Port the queue tests to table tests" --model anthropic/claude-opus --effort xhigh --ttl 3
```

## The queue

```
owl queue list
```

One row per Job waiting, in the order they will run, with the position, the id,
the Project, the state and the beginning of the prompt. Jobs that have left the
queue - done, cancelled, blocked - are left out; `--all` brings them back, with
a dash where their position was.

```
owl queue list --all
```

The queue is first in, first out, and there is no priority: moving a Job is how
it is made urgent
([ADR-0025](../adr/0025-queue-order-and-job-ttl.md)). Positions count from one,
and the Jobs the moved one passes shift to make room.

```
owl queue reorder 7 1
```

A Job that should not run at all leaves the queue. This is for work that is
pending; a Job that has already run is accepted or dropped instead.

```
owl queue remove 7
```

## Running now, and giving the machine back

Owl starts work by itself once the machine is Idle - ten minutes without
keyboard or mouse input, on AC power, unless the daemon's configuration says
otherwise. To watch a Job run now instead of waiting for that:

```
owl start
```

The Job at the head of the queue gets a git worktree and a branch of its own,
so an Agent never touches your checkout. The command waits while the Project's
setup commands prepare that worktree and returns once the Agent has started;
the Run itself continues in the daemon, and it prints the run id to follow it
with.

Your first input event gives the machine back on its own, but the two overrides
are there when you want them:

```
owl pause
owl resume
```

`owl pause` freezes every Run in flight - the Agents and everything they
started, test runners and compilers included - so the machine is yours again
immediately. Nothing is lost: `owl resume` continues the same Runs where they
were. A Run that stays frozen for the whole grace window, fifteen minutes
unless `graceWindow` says otherwise, is ended rather than left holding its
sockets, and its Job goes back in the queue to be carried on from its Handoff.

## Following a Run

```
owl logs <run> -f
```

The output is the Agent's own structured stream, one JSON event per line,
exactly as it was captured. With `-f` it keeps printing until the Run ends;
without it, it prints what has been written so far and stops. Ctrl-C stops
watching and does nothing to the Run.

## The morning after

```
owl status
```

The machine comes first - whether anybody has touched it lately and what it is
drawing from - because it is the answer to why the rest of the report says what
it says. Then the Runs in progress, the Jobs the scheduler passed over and why,
what each Account has spent against its Ceiling, how the Jobs stand by state,
and the lists that want a person: unfinished work, Jobs awaiting a decision,
blocked Jobs with what refused them, and Jobs out of attempts.

```
owl jobs show 7
```

One Job in full: its state, its branch and worktree, what its diff comes to,
every Run with how it ended, the Skills each Run read, what its Agent was
allowed to do, what Verification said check by check, the plan, the Handoff as
it stands on the branch, and the system prompt each Run was given. Nothing Owl
puts in front of an Agent is hidden
([ADR-0017](../adr/0017-unattended-contract.md)).

## Keeping or refusing the work

A Job that passed Verification is in review and waiting on you
([ADR-0013](../adr/0013-verification-gates-completion.md)).

```
owl jobs accept 7
```

The branch stays exactly where the Agent left it, with every commit on it:
merging, rebasing or pushing it is yours to do, and Owl never does it for you.
Only the worktree goes, freeing the disk it held.

```
owl jobs drop 7
```

Both go: the branch is deleted whether or not it was merged anywhere, and the
worktree with it.

Either way, a worktree holding changes nobody has committed is refused, because
reclaiming it would destroy them. Look at what is there first, then say you
meant it:

```
owl jobs accept 7 --force
owl jobs drop 7 --force
```

## When a Job runs out of attempts

A Run that was interrupted costs an attempt exactly as a failure does, so a Job
can reach the end of its ten with the work half done. Extending adds to what is
left rather than replacing it, and a Job that had run out goes back into the
queue at the place it kept.

```
owl jobs extend 7
owl jobs extend 7 --ttl 3
```

## Where to go next

- [Projects and Accounts](projects-and-accounts.md) - the repositories Jobs are
  queued against and the subscriptions they run on.
- [Guide](../../README.md) - the risks, the install, every configuration field
  and the full command list.
