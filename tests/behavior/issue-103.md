# Issue #103: owl jobs show prints today's system prompt, not the one each Run was given

`owl jobs show` printed the system prompt rebuilt from the Project's
`.coding-owl.yaml` as it is on the base branch now and the contract of the Owl
binary running now. Nothing recorded what a Run was actually given, so after a
Project changed its `unattendedClauses`, or an upgrade changed the contract, a
Job that had already run showed a prompt its Agent never saw. The Skills a Run
read (ADR-0024) and the permissions it was granted (ADR-0035) are recorded per
Run; the system prompt (ADR-0017) now is too.

The change is that every Run records, when it starts, the exact system prompt
its Agent is given, and `owl jobs show` prints what each Run was given from that
record. The next Run's prompt is printed as well where it is worth knowing - for
a Job with no Runs, and for a pending Job whose next Run would be given
something different - labelled as what the next Run will be given. The prompt an
Agent is given does not change. The agent Verifier's system prompt is out of
scope.

All scenarios drive the built `owl` binary, or the daemon's API the way the
desktop app does, from the outside against a running daemon. They reuse the
XDG layout, temporary repositories and stub agent of issue #5 (see
`tests/behavior/issue-5.md`): no scenario runs Claude Code, and the stub records
the argv it was given, so "the prompt the Agent recorded" is the value that
follows `--append-system-prompt` in that argv. A "planned Job" is one queued
without `--no-plan`, whose stub agent writes and commits the handoff, so its
planning Run succeeds and returns it to the queue to be carried out (ADR-0026).

"The standing contract" is Owl's unattended contract. `owl jobs show` prints a
system prompt under one of these headings, each on a line of its own and each
followed by the prompt:

- `run <run-id> was given this system prompt:` for a prompt one Run was given;
- `runs <run-id>, <run-id> were given this system prompt:` for a prompt several
  Runs were given, naming every one of them;
- `the next run will be given this system prompt:` for the prompt the Job's next
  Run would be given, read from the Project's base branch as it is now.

A Run with no recorded prompt is reported on one line, with no prompt after it:
`the system prompt run <run-id> was given was not recorded`, or `the system
prompt runs <run-id>, <run-id> were given was not recorded` for several.

"A Run from before prompts were recorded" cannot be made by a daemon that
records them, so a scenario makes one the way an older database holds it: it
stops the daemon, empties the Run's recorded prompt in the daemon's SQLite
database, and starts the daemon again.

## Scenarios

### S1 - a Run records the system prompt its Agent is given, from the moment it starts
Given a running daemon, a registered Project whose base branch carries `.coding-owl.yaml` with `unattendedClauses` holding `run gofmt before committing`, a pending Job against it queued with `--no-plan`, and a stub agent that holds its Run open
When `owl start` runs and, while that Run's Agent is still working, `owl jobs show <job-id>` runs
Then it prints `run <run-id> was given this system prompt:` followed by exactly the prompt the Agent recorded
And once the Run has finished, `owl jobs show <job-id>` prints that heading followed by that same prompt

### S2 - Runs given the same prompt are printed once, naming every Run
Given a running daemon and a planned Job whose planning Run and execution Run both ran with the Project's configuration unchanged
When `owl jobs show <job-id>` runs
Then it prints `runs <planning-run-id>, <execution-run-id> were given this system prompt:` followed by exactly the prompt both Agents recorded
And the standing contract appears in the output exactly once

### S3 - changing the Project's clauses after a Run leaves what is printed for that Run unchanged
Given a running daemon and a Job against a Project whose base branch carries `unattendedClauses` holding `run gofmt before committing`, whose one Run has finished and left it in review
When the base branch's `.coding-owl.yaml` is changed to hold `run the linter before committing` instead, and `owl jobs show <job-id>` runs
Then the prompt it prints under `run <run-id> was given this system prompt:` is exactly the one it printed before the change, and exactly the prompt the Agent recorded
And `run the linter before committing` appears nowhere in the output

### S4 - a Job with no Runs prints the prompt its first Run will be given, labelled as the next Run's
Given a running daemon, a registered Project whose base branch carries `unattendedClauses` holding `run gofmt before committing`, and a pending Job against it queued with `--no-plan` that has not run
When `owl jobs show <job-id>` runs
Then it prints `the next run will be given this system prompt:` followed by the standing contract with `run gofmt before committing` after it
And nothing it prints says that a Run was given a system prompt
And when `owl start` then runs that Job, its Agent records exactly the prompt that was printed

### S5 - a pending Job whose next Run would be given a different prompt prints that one too
Given a running daemon and a planned Job against a Project whose base branch carries `unattendedClauses` holding `run gofmt before committing`, whose planning Run has finished and returned it to the queue
When the base branch's `.coding-owl.yaml` is changed to hold `run the linter before committing` instead, and `owl jobs show <job-id>` runs
Then it prints `run <planning-run-id> was given this system prompt:` followed by exactly the prompt the planning Run's Agent recorded, which holds `run gofmt before committing`
And after it, `the next run will be given this system prompt:` followed by a prompt holding `run the linter before committing` and not `run gofmt before committing`
And when `owl start` runs the Job's execution Run, its Agent records exactly that second prompt

### S6 - a pending Job whose next Run would be given the same prompt prints it once
Given a running daemon and a planned Job whose planning Run has finished and returned it to the queue, with the Project's configuration unchanged
When `owl jobs show <job-id>` runs
Then it prints `run <planning-run-id> was given this system prompt:` followed by exactly the prompt the planning Run's Agent recorded
And it prints no `the next run will be given this system prompt:`
And the standing contract appears in the output exactly once

### S7 - a Run from before prompts were recorded says so, and today's prompt does not stand in for it
Given a running daemon and a Job whose one Run has finished and left it in review, made into a Run from before prompts were recorded
When `owl jobs show <job-id>` runs
Then it exits 0 and prints `the system prompt run <run-id> was given was not recorded`
And it prints no `was given this system prompt:` and no `the next run will be given this system prompt:`
And the standing contract appears nowhere in the output
And the daemon's API, read the way the desktop app reads it, reports that Run's system prompt as empty and the Job's system prompt as empty

### S8 - a pending Job whose latest Run's prompt was not recorded still shows what its next Run will be given
Given a running daemon and a planned Job whose planning Run has finished and returned it to the queue, made into a Run from before prompts were recorded
When `owl jobs show <job-id>` runs
Then it prints `the system prompt run <planning-run-id> was given was not recorded`
And after it, `the next run will be given this system prompt:` followed by the standing contract
And the standing contract appears in the output exactly once, under that heading

### S9 - the daemon's API carries each Run's prompt, and the Job's system prompt is its latest Run's
Given a running daemon, a Project whose base branch carries `unattendedClauses` holding `run gofmt before committing`, a planned Job against it whose planning Run ran with that clause and whose execution Run ran after the clause was changed to `run the linter before committing`, and a second Job queued against it with `--no-plan` that has not run
When the daemon's API is asked for each Job the way the desktop app asks for it, and the answer is read as the desktop frontend receives it
Then every Run of the first Job carries exactly the prompt its Agent recorded, the two of them different
And the first Job's system prompt is exactly the execution Run's
And the second Job's system prompt is the one its next Run will be given: the standing contract with `run the linter before committing` after it

### S10 - the system prompt an Agent is given is what it was before
Given a running daemon, a registered Project whose base branch carries `unattendedClauses` holding `run gofmt before committing` and a blank clause, and a registered Project whose configuration carries no clauses, each with a pending Job queued with `--no-plan`
When `owl start` runs each Job and each Run finishes
Then the first Agent records, after `--append-system-prompt`, exactly the standing contract, a blank line, `This project also asks that you:`, a blank line and `- run gofmt before committing`
And the second Agent records exactly the standing contract
And this holds before the change as well as after it: it is what must not move
