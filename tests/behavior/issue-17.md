# Issue #17: Agent verifier: opt-in fresh-context reviewer

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on its `PATH` in place of Claude
Code, and a Project whose configuration is committed on its base branch, as
`tests/behavior/issue-5.md` and `tests/behavior/issue-7.md` describe them. The
last one drives the desktop app's Go side in-process, as
`tests/behavior/issue-9.md` describes.

The agent Verifier is a second pair of eyes that never shares the context that
produced the work (ADR-0013): a fresh Agent Session on the Job's own Driver and
Account, given the plan and the diff, asked for a verdict and its findings. It
is opt-in per Project, it runs after the Project's own checks, and what it said
is attached to the Run beside them. A verdict that refuses the work blocks the
Job, as a check that fails does; there is no retry.

"The verifying Agent" below means the invocation of the stub agent whose prompt
asks it to judge the work, as against the invocation that carries the Job out.

## Scenarios

### S1 - a Project that does not ask for the agent Verifier does not get it
Given a Project with a check that passes and no `verification` in its configuration
When a Job runs and the Run finishes
Then the Agent was invoked exactly once
And `owl jobs show <job>` reports the check and nothing else
And the Job is in review

### S2 - a Project that asks for it gets a second, separate session
Given a Project configured with `verification:` asking for an agent verifier
When a Job runs and the Run finishes
Then the Agent was invoked twice, the second time with a prompt asking it to judge the work
And the verifying Agent ran in the Job's worktree, on the Account the Job runs on
And its invocation carries no flag that resumes or continues an earlier session

### S3 - the verifying Agent is given the plan and the diff
Given a Project asking for an agent verifier, and a planned Job whose Agent writes a handoff and commits a file
When the Job's execution Run finishes
Then its prompt quotes what the planning Run recorded as the plan, as the plan and set apart from the diff
And quotes the diff, with the name of the file the Agent committed and the line it added
And says, outside everything it quotes, where to write its verdict

### S4 - a verdict that passes leaves the Job in review
Given a Project asking for an agent verifier, whose Agent writes a passing verdict and a note
When the Job runs
Then the Job is in review
And `owl jobs show <job>` reports the agent verifier as passed, beside the Project's own check
And what it wrote is there to read

### S5 - a verdict that refuses the work blocks the Job with the findings attached
Given a Project asking for an agent verifier, whose Agent writes a failing verdict with two findings
When the Job runs
Then the Job is blocked
And the reason names the agent verifier
And `owl jobs show <job>` reports it as failed, with both findings

### S6 - an Agent that leaves no verdict blocks the Job
Given a Project asking for an agent verifier, whose Agent writes nothing
When the Job runs
Then the Job is blocked
And `owl jobs show <job>` reports the agent verifier as failed, saying no verdict was left

### S7 - it leaves nothing behind in the worktree
Given the Run of S4
Then `git status --porcelain` in the Job's worktree reports nothing
And the file the verifying Agent was asked to write is not in the worktree

### S8 - a check that failed still blocks a Job the agent verifier passed
Given a Project with a failing check, asking for an agent verifier whose Agent passes the work
When the Job runs
Then the Job is blocked
And both are reported: the check failed and the agent verifier passed

### S9 - an Agent that will not finish is stopped, and the Job is blocked
Given a Project asking for an agent verifier with a timeout of one second, whose Agent waits far longer
When the Job runs
Then the Job is blocked within the time a Run takes rather than the time that Agent would have taken
And `owl jobs show <job>` reports the agent verifier as failed, saying it was stopped

### S10 - a planning Run is not judged
Given a Project asking for an agent verifier and a planned Job whose Agent writes a handoff
When the planning Run finishes
Then the Agent was invoked once
And the Job is pending again, with no agent verifier reported

### S11 - the desktop app shows the findings beside the check output
Given the Job of S5
When the app is asked for that Job's detail
Then the detail carries both the Project's check and the agent verifier
And the findings are in what it carries

### S12 - the verifying Agent's own contract is shown, like the Agent's
Given the Job of S4, whose Project asks for an agent verifier
When `owl jobs show <job>` runs
Then it prints the verifier's system prompt as well as the Agent's
And a Project that asks for no agent verifier has none to print
