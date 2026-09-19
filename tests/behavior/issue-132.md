# Issue #132: Jobs carry labels, settable at queue time and after, filterable in owl queue list

A Job is a standing intent to do one piece of work in one Project. Nothing on
it says what kind of work it is, so a queue of thirty Jobs cannot be narrowed
to the ones a person is thinking about right now.

The change is that a Job carries free-form **labels**. They are set at queue
time with `owl add --label`, changed afterwards with `owl jobs label add` and
`owl jobs label remove`, shown by `owl jobs show` and in a `LABELS` column of
`owl queue list`, and narrow that listing through a repeatable `--label`
filter. A label is a name and nothing else: it carries no meaning for the
scheduler, which goes on scanning the queue in order (ADR-0025). Routing work
to an Account or a Project by label is out of scope and tracked separately.

A label carries no whitespace, so that every place a Job's labels are printed
beside other columns stays one unambiguous line. Surrounding whitespace is
trimmed rather than refused, because a shell leaves it behind; whitespace
inside is refused, because it would make one label look like two.

The scenarios drive the built `owl` binary against a daemon started as a
subprocess, in the XDG layout `tests/behavior/issue-2.md` describes, the way a
user would. The existing columns of `owl queue list` are S15's subject, because
issue #4 fixed them and this change adds to them rather than replacing them.

## Scenarios

### S1 - a Job queued with --label carries those labels
Given a registered Project and a running daemon
When `owl add "fix the flaky test" --label bug --label urgent` runs
Then it exits 0
And `owl jobs show <id>` prints a `labels:` line naming `bug` and `urgent`
And the Job's row in `owl queue list` names `bug` and `urgent` in its `LABELS` column

### S2 - a Job queued without --label carries none
Given a registered Project and a running daemon
When `owl add "fix the flaky test"` runs
Then `owl jobs show <id>` prints `labels: (none)`
And the Job's row in `owl queue list` has `-` in its `LABELS` column

### S3 - a label is added to a Job already queued
Given a Job carrying no labels
When `owl jobs label add <id> bug` runs
Then it exits 0 and its output names the Job and the labels it carries afterwards
And `owl jobs show <id>` names `bug`

### S4 - a label is removed from a Job
Given a Job carrying `bug` and `urgent`
When `owl jobs label remove <id> urgent` runs
Then it exits 0
And `owl jobs show <id>` names `bug` and not `urgent`

### S5 - adding a label a Job already carries changes nothing
Given a Job carrying `bug`
When `owl jobs label add <id> bug` runs
Then it exits 0
And `owl jobs show <id>` names `bug` exactly once

### S6 - removing a label a Job does not carry is refused
Given a Job carrying `bug`
When `owl jobs label remove <id> urgent` runs
Then it exits non-zero, and standard error names `urgent`
And `owl jobs show <id>` still names `bug`

### S7 - a label that is empty or holds whitespace is refused, and one with whitespace around it is trimmed
Given a registered Project and a running daemon
When `owl add "work" --label "   "` runs
Then it exits non-zero, standard error says a label may not be empty, and nothing is queued
And `owl add "work" --label "two words"` is refused, with standard error saying a label may not hold whitespace
And `owl jobs label add <id> "two words"` on a queued Job is refused the same way
And `owl add "work" --label "  bug  "` queues a Job whose only label is `bug`

### S8 - labelling a Job that does not exist is refused
Given a running daemon and no Job with id 999
When `owl jobs label add 999 bug` runs
Then it exits non-zero and standard error names the id
And `owl jobs label remove 999 bug` is refused the same way

### S9 - owl queue list --label shows only the Jobs carrying that label
Given three queued Jobs, one carrying `bug`, one carrying `chore` and one carrying neither
When `owl queue list --label bug` runs
Then it prints the row for the Job carrying `bug` and no other row

### S10 - repeating --label narrows to the Jobs carrying all of them
Given a Job carrying `bug` and `urgent`, and a Job carrying `bug` alone
When `owl queue list --label bug --label urgent` runs
Then it prints only the row for the Job carrying both

### S11 - a filter nothing matches says the queue is empty
Given queued Jobs, none carrying `nope`
When `owl queue list --label nope` runs
Then it exits 0, says the queue is empty, and prints no row

### S12 - the filter applies to --all as well
Given a queued Job carrying `bug` and a cancelled Job carrying `bug`
When `owl queue list --all --label bug` runs
Then it prints both, and the cancelled one has `-` where a position would be
And `owl queue list --label bug` prints only the queued one

### S13 - labels survive a daemon restart
Given a Job carrying `bug`
When the daemon is stopped and started again
Then `owl jobs show <id>` still names `bug`
And `owl queue list --label bug` still finds the Job

### S14 - labels belong to one Job
Given two Jobs both carrying `bug`
When `owl jobs label remove <id of the first> bug` runs
Then `owl jobs show <id of the second>` still names `bug`
And `owl queue list --label bug` finds the second Job and not the first

### S15 - the listing keeps the columns it had
Given queued Jobs, labelled and unlabelled
When `owl queue list` runs
Then its header names `POSITION`, `ID`, `PROJECT`, `STATE`, `LABELS` and `PROMPT`, in that order
And every row still reports the position, id, Project, state and prompt issue #4 fixed
