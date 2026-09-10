# Issue #5: Run a Job: worktree and branch, Driver x Executor, Claude Code in print mode, unattended contract, owl start, owl logs -f

All scenarios drive the built `owl` binary from the outside against a running
daemon, except S21, which is about the test suite itself.

Since issue #6, a Job is planned before it is carried out unless it is added
with `--no-plan`. These scenarios are about running a Job rather than about
planning one, so every Job they run is queued with `--no-plan` and is a single
Run. A Job's own prompt reaches the Agent inside the prompt that phase builds
around it.

"XDG layout" is the temporary directory tree of issue #2: `HOME`,
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` point into it and
`XDG_RUNTIME_DIR` is unset. "Temporary repository" means a real `git init`
repository under that layout whose base branch `main` carries at least one
commit. "Registered Project" means such a repository added with
`owl project add`, and "pending Job" one queued against it with `owl add`.

**The stub agent.** No scenario runs Claude Code. The harness builds a small
program named `claude` into a directory it puts first on the daemon's `PATH`,
so the Claude Code Driver finds it where it would find the real one. The stub:

- prints the version in `$OWL_FAKE_CLAUDE_VERSION` (default `2.1.267 (Claude
  Code)`) for `--version`, and exits 0;
- otherwise writes the argv it was given, its own pid, its parent's pid and its
  working directory as JSON to `$OWL_FAKE_CLAUDE_ARGV`, then emits the lines of
  the file named by `$OWL_FAKE_CLAUDE_SCRIPT` on stdout, one per line, and exits
  with `$OWL_FAKE_CLAUDE_EXIT` (default 0);
- treats a script line reading `#wait` as "block until the file named by
  `$OWL_FAKE_CLAUDE_WAIT` exists", so a scenario can hold a Run open.

## Scenarios

### S1 - owl start runs the oldest pending Job in a worktree of its own
Given a running daemon, a registered Project `api`, and the pending Jobs `first` and `second` in that order
When `owl start` runs
Then it exits 0
And stdout names the Job it started and the id of the Run it started
And once the Run has finished, `git worktree list` in the Project reports a worktree at `$XDG_DATA_HOME/coding-owl/worktrees/<job-id>`
And that worktree is checked out on the branch `owl/job-<job-id>`
And `second` is still pending, and no Run was started for it

### S2 - the user's own checkout is untouched
Given a running daemon, a registered Project on branch `main` with an uncommitted file in its working tree, and a pending Job
When `owl start` runs and the Run finishes
Then the Project's own checkout is still on `main`
And its uncommitted file is still there, with the same contents
And `git status --porcelain` in the Project reports only that file

### S3 - the Agent is a direct child of the daemon
Given a running daemon and a pending Job
When `owl start` runs and the Run finishes
Then the parent pid the stub agent reported is the daemon's own pid

### S4 - a clean exit lands the Run in succeeded and the Job in review
Given a running daemon, a pending Job, and a stub agent that exits 0
When `owl start` runs and the Run finishes
Then `owl jobs show <job-id>` reports the Job's state as `review`
And it reports one Run whose outcome is `succeeded`
And `owl queue list` no longer shows the Job

### S5 - a non-zero exit fails the Run and blocks the Job with the reason
Given a running daemon, a pending Job, and a stub agent that exits 3
When `owl start` runs and the Run finishes
Then `owl jobs show <job-id>` reports the Job's state as `blocked`
And it reports one Run whose outcome is `failed`
And the reason it prints names the exit status 3

### S6 - the Run's structured output is persisted per Run
Given a running daemon, a pending Job, and a stub agent scripted to emit three stream-json lines
When `owl start` runs and the Run finishes
Then a file exists at `$XDG_STATE_HOME/coding-owl/logs/<run-id>.jsonl`
And it holds exactly the lines the stub emitted, in order

### S7 - owl logs prints the whole log of a finished Run
Given the finished Run of S6
When `owl logs <run-id>` runs
Then it exits 0
And stdout is exactly the lines the stub emitted, in order

### S8 - owl logs -f streams events while the Run is in progress
Given a running daemon, a pending Job, and a stub agent scripted to emit one line, wait for a release file, then emit a second line and exit 0
When `owl start` runs and `owl logs <run-id> -f` is started while the Run is in progress
Then the first line reaches its stdout before the release file is created
And after the release file is created, the second line reaches its stdout
And the command exits 0 once the Run has ended

### S9 - the effective system prompt carries the unattended contract
Given a running daemon and a pending Job
When `owl jobs show <job-id>` runs
Then it exits 0
And it prints a system prompt that says the Agent is running unattended and nobody can answer a question
And that prompt names `.coding-owl/HANDOFF.md`
And it says to record assumptions rather than stall, to commit as it goes, and never to guess at anything destructive or irreversible

### S10 - a Project's own clauses are appended to the contract
Given a running daemon and a registered Project whose base branch carries `.coding-owl.yaml` with `unattendedClauses` holding `run gofmt before committing`, and a pending Job against it
When `owl jobs show <job-id>` runs
Then the system prompt it prints contains the standing contract
And the Project's clause appears after it

### S11 - the invocation is print mode with structured streaming output and no permission prompts
Given a running daemon and a pending Job
When `owl start` runs and the Run finishes
Then the argv the stub agent recorded contains `--print`, `--output-format stream-json`, `--verbose` and `--permission-prompts none`
And it contains neither `--dangerously-skip-permissions` nor `--allow-dangerously-skip-permissions`
And the Job's prompt is in the argv

### S12 - the contract reaches the Agent
Given the Run of S11
Then the argv the stub agent recorded contains `--append-system-prompt`
And the value that follows it is the same system prompt `owl jobs show` printed

### S13 - a spend cap is applied only when the Project configures one
Given a running daemon and a registered Project whose base branch carries `.coding-owl.yaml` with `budgetUSD: 5`, and a pending Job against it
When `owl start` runs and the Run finishes
Then the argv the stub agent recorded contains `--max-budget-usd 5`
And for a Project whose configuration sets no budget, the argv contains no `--max-budget-usd`

### S14 - a Claude Code outside the supported range refuses to start the Run
Given a running daemon, a pending Job, and a stub agent reporting version `1.9.0`
When `owl start` runs
Then it exits with a non-zero code
And stderr names the version it found and the range Owl supports
And `owl jobs show <job-id>` still reports the Job as pending with no Run
And no worktree was created for the Job

### S15 - the branch is cut from the base branch, under the Project's prefix
Given a running daemon and a registered Project whose base branch `main` carries `.coding-owl.yaml` with `branchPrefix: nightly/`, and a pending Job against it
When `owl start` runs and the Run finishes
Then the Job's worktree is on the branch `nightly/job-<job-id>`
And that branch points at the same commit as `main`, since the stub agent committed nothing

### S16 - owl start with nothing pending says so
Given a running daemon whose queue is empty
When `owl start` runs
Then it exits 0
And stdout says there is nothing pending to run
And no Run is recorded

### S17 - only one Run at a time
Given a running daemon, two pending Jobs, and a stub agent that waits for a release file
When `owl start` runs and a second `owl start` follows while the first Run is in progress
Then the second exits with a non-zero code
And stderr says a Run is already in progress and names the Job it is for
And the second Job is still pending

### S18 - owl jobs show reports the Job, its Runs and where the log is
Given the finished Run of S4
When `owl jobs show <job-id>` runs
Then it prints the Job's id, Project, state, branch and worktree
And a Runs section naming the Run's id, its attempt number `1`, its outcome, the status the Agent exited with, and the path of its log
And `owl jobs show` for an unknown Job exits with a non-zero code naming the id

### S19 - owl logs on an unknown Run fails clearly
Given a running daemon
When `owl logs 999` runs
Then it exits with a non-zero code
And stderr names `999`

### S20 - the Run commands report a stopped daemon
Given a temporary XDG layout with no daemon running
When `owl start`, `owl logs 1` and `owl jobs show 1` each run
Then each exits with a non-zero code
And each stderr says the daemon is not running and names the socket path

### S21 - the real Claude Code smoke test is skipped unless it is enabled
Given the environment variable that enables it is unset
When the test suite is run with `-v` for that test alone
Then it reports itself skipped
And the skip message names the variable that would enable it

### S22 - the Agent works in the Job's worktree
Given a running daemon and a pending Job
When `owl start` runs and the Run finishes
Then the working directory the stub agent reported is the Job's worktree
And it is not the Project's own path

### S23 - a Run the daemon never finished does not hold the queue
Given a running daemon whose stub agent is waiting, two pending Jobs, and a Run in progress for the first
When the daemon is killed outright, so nothing records how that Run ended, and a daemon is started again
Then `owl jobs show <first job>` reports that Run's outcome as `interrupted`
And that Job is `pending` again, in the place it held, because a Run nobody finished is a Job to carry on with (ADR-0011, issue #11 - when this was written the Job was left where it was and the second Job ran instead)
And `owl start` starts a new Run rather than refusing because one is in progress

### S24 - the daemon stops cleanly while a Run is being followed
Given a running daemon, a pending Job whose stub agent is waiting for its release file, and an `owl logs <run-id> -f` attached to that Run
When the daemon is sent SIGTERM
Then the daemon exits 0
And the follower's stream ends rather than hanging until it is killed
