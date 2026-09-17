# Issue #111: An Account carries standing instructions every Run on it reads

Owl had two places to put standing instructions for an Agent and both were per
Project: `unattendedClauses` in the Project's configuration (ADR-0017), and
whatever the repository's own instruction files say. A user whose conventions
hold across every Project a given Account runs - how to write a commit message,
what never to touch - had to repeat them in each Project, and the night they
forgot to was the night the Agent did not know.

The change is that an Account carries **standing instructions**: text every Run
on that Account reads, whichever Project the Run is for (ADR-0037). They are
kept as a file inside the Account's own configuration directory, under whatever
name that Account's Driver's tool reads instructions from - `CLAUDE.md` for the
`claude-code` Driver. Owl injects nothing: it writes the file, and the tool
reads it as it would any other user-level instruction file, because an
Account's configuration directory is the tool's configuration directory
(ADR-0019). `owl account instructions show | set | edit` is where a user writes
them; the desktop app is issue #112.

The scenarios drive the built `owl` binary against a running daemon, with the
stub agent of issue #5 standing in for Claude Code (see
`tests/behavior/issue-5.md`) and the Account, its configuration directory and
its credential as `tests/behavior/issue-14.md` sets them up. An Agent runs in
the Job's worktree rather than in the Account's configuration directory, so
what proves a Run reads them is the `CLAUDE_CONFIG_DIR` the Agent was started
with and the file standing in it - which is exactly what the real tool would
open.

`owl account instructions set` reads the text from standard input, the way a
user pipes a file into it, and prints where it put it. `owl account
instructions show` is its inverse: the instructions go to standard output and
where they came from goes to standard error, so that piping one command into
the other carries the document across rather than the document with Owl's own
prose on top of it.

## Scenarios

### S1 - instructions written for an Account are the tool's own file in that Account's directory
Given a running daemon and the Account `work`, which nobody has written standing instructions for
When instructions are written with `owl account instructions set work`
Then it says where it put them, which is `CLAUDE.md` in that Account's configuration directory
And that file holds exactly what was written, and is readable by its owner alone, like everything else in that directory
And `owl account instructions show work` says where they came from, and prints exactly the text that was written and nothing else

### S2 - an Agent for that Account is started against a configuration directory holding them
Given a running daemon, the Account `work` with standing instructions written for it, and a Project on that Account with a pending Job
When the Job is run
Then the Agent was given `CLAUDE_CONFIG_DIR` holding that Account's configuration directory
And `CLAUDE.md` stands in that directory, holding exactly the instructions that were written
And the Agent ran in the Job's worktree rather than in that directory: the configuration directory is where the tool reads its instructions from, not where the work happens

### S3 - input that is blank takes them away
Given a running daemon and the Account `work` with standing instructions written for it
When `owl account instructions set work` is given blank input
Then it says the instructions were cleared and that the file is gone
And `CLAUDE.md` is gone from the Account's configuration directory, rather than left there empty
And `owl account instructions show work` reads them back as none, as it did before any were written, printing nothing as the document

### S4 - instructions past the bound are refused, and what was there is left alone
Given a running daemon and the Account `work` with standing instructions written for it
When `owl account instructions set work` is given more than 64 KiB
Then it exits non-zero and says how long they may be
And `CLAUDE.md` still holds exactly the instructions that were there before: a refused write leaves what it refused to replace

### S5 - a `CLAUDE.md` that is a symlink is refused rather than written through
Given a running daemon, the Account `work`, and a `CLAUDE.md` in its configuration directory that is a symlink to a file of the user's own elsewhere
When `owl account instructions set work` is given instructions to write
Then it exits non-zero and says that what is in that place is not a regular file
And the file the link points at is untouched: following it would have Owl write outside the Account's directory, into the user's own setup, which is the one thing an Account exists to keep separate (ADR-0019)
And the link is still a link, rather than replaced by a file of Owl's
