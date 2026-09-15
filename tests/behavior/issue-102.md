# Issue #102: owl jobs show prints Agent-written text without escaping control characters

`owl jobs show` is the command a person runs in the morning to read what an
unattended Run did. Reported against `main`: `printJob` passed the header
fields and the reason through `terminalSafe`, which exists so that text from
outside Owl reaches the terminal as text and never as an instruction to it, and
printed the rest raw - the plan and the handoff an Agent wrote (ADR-0026), the
system prompt carrying a Project's `unattendedClauses` (ADR-0017) and the
verifier's system prompt, each check's name, reason and output (ADR-0030), each
Run's Skills (ADR-0024) and permissions (ADR-0035), each phase's model and
effort and where they came from (ADR-0028), and the Run table's phase, state
and log path. Any of them could hold an escape that retitles the terminal or
rewrites lines already on screen.

The fix is that every value `owl jobs show` prints that did not come from Owl
itself reaches the terminal with its control characters made visible, as
`terminalSafe` renders them, in every one of those sections; that newlines and
tabs in the plan, the handoff, the system prompts and check output still print
as newlines and tabs; and that a report holding no control characters is
byte-for-byte what it was. Other commands are out of scope.

All scenarios drive the built `owl` binary against a running daemon, with the
stub agent of issue #5 first on the daemon's `PATH` in place of Claude Code.
The XDG layout, the temporary repository, the registered Project and the stub
are as `tests/behavior/issue-5.md` and `tests/behavior/issue-6.md` describe
them. "The handoff" is `.coding-owl/HANDOFF.md` on the Job's branch.

"The escape" is the sequence ESC `]0;pwned` BEL ESC `[2K`, which retitles a
terminal window and then erases the line the cursor is on. "Shown as text"
means printed as the characters `\x1b]0;pwned\x07\x1b[2K`. "Clean" means the
command's stdout holds no control character other than newline and tab, so
nothing in it is an instruction to a terminal.

Some values have no way in through the daemon today. A Skill's source and ref
and an Account's allowed tools are refused where they are written when they
would not print as themselves, and a check's reason, a Run's phase and its log
path are worded or chosen by the daemon. Those scenarios write the value into
the daemon's database directly, as issue #11's scenarios do for what an earlier
daemon left behind: `owl jobs show` prints what the daemon reports, and does
not rely on every writer having refused it. Three values have no way in from
outside at all: the verifier's system prompt and where a phase's setting came
from are Owl's own words, and a Run's state is either an outcome, which travels
from the daemon as one of a closed set, or where a Run in progress is, which
the daemon keeps in memory. S4 pins that the verifier's system prompt still
prints as it did, and an escape in any of the three is left to the unit tests
of `internal/cli`.

## Scenarios

### S1 - the plan shows the escape as text and keeps its lines and tabs
Given a running daemon and a planned Job whose planning Agent writes and commits a handoff holding the escape, a line indented with a tab and a third line
When the planning Run finishes and `owl jobs show <job>` runs
Then the plan section is that handoff with the escape shown as text, line for line and with its tab
And stdout is clean

### S2 - the handoff shows the escape as text and keeps its lines and tabs
Given a running daemon and a Job added with `--no-plan` whose Agent writes and commits a handoff holding the escape, a line indented with a tab and a third line
When the Run finishes and `owl jobs show <job>` runs
Then the handoff section is that handoff with the escape shown as text, line for line and with its tab
And stdout is clean

### S3 - a Project's clause shows the escape as text in the system prompt
Given a running daemon, a Project whose base branch carries `.coding-owl.yaml` with an `unattendedClauses` entry holding the escape, and a Job added with `--no-plan` against it
When the Run finishes and `owl jobs show <job>` runs
Then the system prompt section is the system prompt the Agent was given with the escape shown as text, and every other line of it, the standing contract's included, exactly as the Agent was given it
And stdout is clean

### S4 - the verifier's system prompt prints exactly as the verifying Agent was given it
Given a running daemon, a Project that asks for the agent Verifier, and a Job whose Run was verified and passed
When `owl jobs show <job>` runs
Then the verifier system prompt section is exactly the system prompt the verifying Agent was given, line for line
And stdout is clean

### S5 - a check's name and output show the escape as text, and the output keeps its lines and tabs
Given a running daemon, a Project whose `.coding-owl.yaml` declares a check whose name holds the escape and whose command prints a line holding the escape and a second line indented with a tab and then exits 1, and a Job added with `--no-plan` against it
When the Run finishes and `owl jobs show <job>` runs
Then the checks section names the check with the escape shown as text, as failed
And the check's output is the two lines the command printed, with the escape shown as text and the tab still a tab
And stdout is clean

### S6 - a check's reason shows the escape as text
Given the finished Run of a Job, and a failed check recorded against that Run whose reason holds the escape, written into the database
When `owl jobs show <job>` runs
Then the checks section gives that check's reason with the escape shown as text
And stdout is clean

### S7 - a Run's Skills show the escape as text
Given the finished Run of a Job, and a Skill recorded against that Run whose name, source and ref each hold the escape, written into the database
When `owl jobs show <job>` runs
Then the skills section gives that Skill's name, source and ref, each with the escape shown as text
And stdout is clean

### S8 - a Run's permissions show the escape as text
Given the finished Run of a Job, and permissions recorded against that Run of which one holds the escape and one does not, written into the database
When `owl jobs show <job>` runs
Then the permissions section lists, for that Run, the rule that holds none as it is and the other with the escape shown as text
And stdout is clean

### S9 - a phase's model and effort show the escape as text
Given a running daemon, a Project whose `.coding-owl.yaml` sets the execute phase's model and effort to values holding the escape, and a Job added with `--no-plan` against it
When `owl jobs show <job>` runs
Then the phases section gives the execute phase's model and effort, each with the escape shown as text and each from `project`
And stdout is clean

### S10 - the Run table shows the escape as text in a Run's phase and log path
Given the finished Run of a Job, and a second Run of that Job that succeeded and whose phase and log path each hold the escape, written into the database
When `owl jobs show <job>` runs
Then the Runs section gives that second Run's phase and log path, each with the escape shown as text, and its state as `succeeded`
And stdout is clean

### S11 - a report with no control characters prints every document as it was written
Given a running daemon, a Project whose clause holds a tab and non-ASCII text and whose check prints three lines, one indented with a tab and one in non-ASCII text, and then exits 1, and a planned Job against it whose Agent writes and commits a handoff of several lines, one indented with a tab, one blank and one in non-ASCII text
When the planning Run and the execution Run finish and `owl jobs show <job>` runs
Then the plan section and the handoff section are each exactly that handoff, without its final newline
And the system prompt section is exactly the system prompt the Agent was given
And the check's output is exactly the lines its command printed, each indented as a check's output is
And stdout is clean
