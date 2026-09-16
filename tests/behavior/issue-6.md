# Issue #6: Plan then execute: --plan/--no-plan, handoff on the branch, fresh context, model and effort per phase

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the daemon's `PATH` in place of
Claude Code. The XDG layout, the temporary repository and the stub are as
`tests/behavior/issue-5.md` describes them, with one addition: the stub writes
the files named by `$OWL_FAKE_CLAUDE_WRITE` into its working directory before it
emits its script, so a scenario can have a planning Agent leave a handoff
behind, and `$OWL_FAKE_CLAUDE_COMMIT` makes it commit them.

"The handoff" is `.coding-owl/HANDOFF.md` on the Job's branch (ADR-0026). One
`owl start` runs one phase of one Job: a Job that is planned needs two.

## Scenarios

### S1 - a Job is planned before it is executed
Given a running daemon, a registered Project and a Job added with neither `--plan` nor `--no-plan`
When `owl start` runs, the Run finishes, `owl start` runs again and that Run finishes
Then `owl jobs show <job>` reports two Runs, the first in phase `plan` and the second in phase `execute`
And the Job is in `review` after the second, and was `pending` again between them

### S2 - --no-plan goes straight to execution
Given a running daemon, a registered Project and a Job added with `--no-plan`
When `owl start` runs and the Run finishes
Then `owl jobs show <job>` reports one Run, in phase `execute`
And the Job is in `review`

### S3 - --plan and --no-plan together are refused
Given a running daemon and a registered Project
When `owl add "work" --plan --no-plan` runs
Then it exits with a non-zero code
And stderr names both flags
And `owl queue list` reports an empty queue

### S4 - the plan is committed as the handoff, and owl jobs show prints it
Given a running daemon and a Job whose planning Agent writes `.coding-owl/HANDOFF.md` saying `step one: read the tests` and commits it
When the planning Run finishes
Then `.coding-owl/HANDOFF.md` on the Job's branch holds that text
And `owl jobs show <job>` prints a plan section holding it

### S5 - a handoff the Agent left uncommitted is committed by Owl
Given a running daemon and a Job whose planning Agent writes the handoff and commits nothing
When the planning Run finishes
Then `.coding-owl/HANDOFF.md` is committed on the Job's branch
And `git status --porcelain` in the Job's worktree does not report it
And the Job is pending again, ready for its execution Run

### S6 - a planning Run that leaves no handoff blocks the Job
Given a running daemon and a Job whose planning Agent writes nothing
When the planning Run finishes
Then `owl jobs show <job>` reports the Job as `blocked`
And the reason names `.coding-owl/HANDOFF.md`
And no execution Run follows: `owl start` reports nothing pending

### S7 - the execution Run is a fresh invocation carrying the handoff
Given a Job whose planning Run left the handoff of S4
When the execution Run starts
Then the prompt in the Agent's argv contains `step one: read the tests`
And it names `.coding-owl/HANDOFF.md`
And it contains the Job's own prompt
And the argv carries no `--resume` and no `--continue`, and no session id is stored anywhere in `owl jobs show`

### S8 - a second execution Run reads the handoff as the first left it
Given a running daemon and a Job added with `--no-plan`, whose execution Agent writes the handoff `step two: fix the parser`, commits it, and then waits
When the daemon is stopped while that Run is in progress, started again, and `owl start` runs
Then the Job was pending again after the interruption
And the new Run is in phase `execute`
And its prompt contains `step two: fix the parser`

### S9 - a handoff edited by hand changes what the next Run reads
Given a Job whose planning Run left the handoff of S4, edited in its worktree to `changed by the user` and committed before the execution Run
When the execution Run starts
Then the prompt in the Agent's argv contains `changed by the user`
And it does not contain the text the planning Run had written

### S10 - model and effort default per phase
Given a running daemon, a registered Project with no configuration file, and a Job added with no `--model` or `--effort`
When the planning Run and then the execution Run happen
Then the planning Agent's argv carries `--model opus` and `--effort xhigh`
And the execution Agent's argv carries `--model opus` and `--effort high`

### S11 - a Project's configuration overrides the defaults
Given a registered Project whose base branch carries `.coding-owl.yaml` setting `phases.execute.effort` to `medium`
When the execution Run of a Job in that Project happens
Then its argv carries `--effort medium`
And it still carries `--model opus`, which the file does not set

### S12 - the global file overrides the defaults, and a Project overrides the global
Given `$XDG_CONFIG_HOME/coding-owl/config.yaml` setting `phases.plan.model` to `anthropic/claude-sonnet` and `phases.execute.effort` to `low`, and a Project whose `.coding-owl.yaml` sets `phases.plan.model` to `anthropic/claude-haiku`
When the planning Run and then the execution Run happen
Then the planning Agent's argv carries `--model haiku`
And the execution Agent's argv carries `--effort low`

### S13 - owl add --model and --effort override everything for that Job
Given the global file and the Project configuration of S12, and a Job added with `--model anthropic/claude-sonnet --effort max`
When the planning Run and then the execution Run happen
Then both Agents' argv carry `--model sonnet` and `--effort max`

### S14 - owl jobs show prints the effective model and effort per phase, and where each came from
Given the global file and the Project configuration of S12, and a Job added with no overrides
When `owl jobs show <job>` runs
Then it prints a phases section with a row for `plan` and a row for `execute`
And the plan row shows model `anthropic/claude-haiku` from the project and effort `xhigh` from the default
And the execute row shows model `anthropic/claude-opus` from the default and effort `low` from the global file

### S15 - a Run records its phase
Given the two Runs of S1
When `owl jobs show <job>` runs
Then its runs table has a phase column reading `plan` for the first Run and `execute` for the second

### S16 - a bad model or effort is refused before an Agent is started
Given a registered Project whose base branch carries `.coding-owl.yaml` setting `phases.plan.model` to one of `anthropic/--oops`, `--oops/claude-opus` or `--oops`
When `owl start` runs for a Job in that Project
Then it exits with a non-zero code
And stderr names the model it refused
And `owl jobs show <job>` reports the Job as still pending with no Run

A model that would reach the tool's argv as an option is the case this guards:
the vendor prefix means the written model need not start with a dash for a half
of it to, so each half is checked rather than the string. A model naming no
vendor at all is refused too, for saying nothing about who serves it.
