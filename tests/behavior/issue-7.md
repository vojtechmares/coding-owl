# Issue #7: Verification: shell checks with expect and timeout, all failures reported, review vs blocked; per-Project setup commands

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the daemon's `PATH` in place of
Claude Code. The XDG layout, the temporary repository and the stub are as
`tests/behavior/issue-5.md` describes them; the stub also records the names of
the files in its working directory, so a scenario can see what a setup command
left there before the Agent started.

Jobs are queued with `--no-plan` unless a scenario says otherwise, so each one
takes a single execution Run. Checks and setup commands are configured in
`.coding-owl.yaml` on the Project's base branch:

```yaml
apiVersion: codingowl.dev/v1
setup:
  - touch prepared.txt
checks:
  - name: build
    run: "true"
  - name: fmt
    run: echo untidy.go
    expect: empty_output
  - name: test
    run: "false"
    timeout: 30s
```

## Scenarios

### S1 - every check runs, even after one has failed
Given a running daemon and a Project whose base branch configures the checks `first` (exits 1), `second` (exits 0) and `third` (exits 1), and a Job against it
When the execution Run finishes
Then `owl jobs show <job>` reports all three checks, `first` and `third` failed and `second` passed
And the Job is `blocked`

### S2 - all checks passing lands the Job in review
Given a running daemon and a Project whose base branch configures two checks that both exit 0, and a Job against it
When the execution Run finishes
Then `owl jobs show <job>` reports both checks passed
And the Job is `review`

### S3 - a Project with no checks is left where the Agent left it
Given a running daemon and a Project whose base branch configures no checks, and a Job against it
When the execution Run finishes cleanly
Then the Job is `review`
And `owl jobs show <job>` reports no checks

### S4 - expect empty_output fails a check that exits zero but prints
Given a running daemon and a Project configuring a check `fmt` that runs `echo untidy.go` with `expect: empty_output`, and a check `quiet` that runs `true` with the same expectation
When the execution Run finishes
Then `fmt` is reported failed, and why it failed mentions the output it printed
And `quiet` is reported passed
And the Job is `blocked`

### S5 - a hung check is killed at its timeout and reported failed
Given a running daemon and a Project configuring a check `hang` that runs `sleep 60` with `timeout: 1s`
When the execution Run finishes
Then `hang` is reported failed within a few seconds
And why it failed names the timeout
And the Job is `blocked`

### S6 - a blocked Job shows every failing check's name and output
Given the Job of S1, whose `first` check prints `first is unhappy` and whose `third` check prints `third is unhappy`
When `owl jobs show <job>` runs
Then its checks section names `first` and `third` as failed
And it holds the text each of them printed

### S7 - setup commands run in the worktree before the Agent
Given a running daemon and a Project whose base branch configures `setup` holding `touch prepared.txt`, and a Job against it
When `owl start` runs and the Run finishes
Then the files the stub agent recorded in its working directory include `prepared.txt`
And `prepared.txt` is in the Job's worktree

### S8 - a setup failure blocks the Job before any Run
Given a running daemon and a Project whose base branch configures `setup` holding a command that exits 1
When `owl start` runs
Then it exits with a non-zero code
And stderr names the setup command that failed
And `owl jobs show <job>` reports the Job as `blocked` with no Run
And the stub agent was never invoked

### S9 - checks are taken from the base branch, not from the worktree
Given a running daemon and a Project whose base branch configures a check `guard` that exits 1, and a Job whose Agent rewrites `.coding-owl.yaml` in its worktree to configure no checks at all
When the execution Run finishes
Then `guard` is still reported failed
And the Job is `blocked`

### S10 - checks run after an execution Run, not after a planning Run
Given a running daemon, a Project configuring a check that exits 1, and a Job added with neither `--plan` nor `--no-plan` whose planning Agent writes the handoff
When the planning Run finishes
Then the Job is `pending` with no checks reported
And after the execution Run that follows, the Job is `blocked` with that check reported failed

### S11 - a check that fails does not stop the Agent's own work being kept
Given the blocked Job of S1
When the Job's branch is inspected
Then the commit the Agent made is still on it
And the Run is reported `succeeded` even though the Job is `blocked`, because the Agent did its part and Verification is what refused it

### S12 - a check configuration Owl cannot read is refused before the Agent starts
Given a running daemon and a Project whose base branch configures a check with no `run` command
When `owl start` runs for a Job against it
Then it exits with a non-zero code
And stderr names the configuration file and says what is wrong with the check
And the stub agent was never invoked
And `owl queue list` still shows the Job waiting

### S13 - a check's timeout defaults when the configuration does not set one
Given a running daemon and a Project configuring a check with no `timeout`
When the execution Run finishes
Then the check is reported with an outcome rather than hanging the Run
And `owl jobs show <job>` reports the Job as `review`

### S14 - the Verification commands run in the Job's worktree
Given a running daemon and a Project configuring a check `where` that runs `test -f prepared.txt` after a setup command created that file
When the execution Run finishes
Then `where` is reported passed
And the Project's own checkout does not hold `prepared.txt`

### S15 - the reason a Job is blocked names the checks that refused it
Given the blocked Job of S1
When `owl jobs show <job>` runs
Then it prints a reason naming `first` and `third`
