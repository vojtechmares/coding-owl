# ADR-0037: An Account carries standing instructions, kept as the tool's own file

- **Status:** Accepted
- **Date:** 2026-09-17

## Context

Owl has two places to put standing instructions for an Agent, and both are per
Project: `unattendedClauses` in the Project's configuration, appended to the
system prompt (ADR-0017), and whatever the repository's own instruction files
say.

There is nothing per Account. A user whose conventions hold across every
Project a given Account runs - how to write a commit message, what never to
touch, which house style to follow - has to repeat them in each Project's
configuration, and the night they forget to is the night the Agent does not
know.

An Account already has a configuration directory of its own, which is the
coding tool's configuration directory (ADR-0019). Claude Code reads a
user-level instruction file out of that directory. So the mechanism to carry
per-Account instructions already exists; Owl simply never wrote to it.

Nothing else Owl keeps in that directory was reachable either, which is the
same gap `owl account exec` closes from the other side.

## Decision

An Account carries **standing instructions**: text every Run on that Account
reads, whichever Project the Run is for.

They are kept as a file inside the Account's configuration directory, under
whatever name that Account's Driver's tool reads. For `claude-code` that is
`CLAUDE.md`. The filename is the tool's, so it lives behind the Driver as
`InstructionsFile()`, beside `SkillsDir()` (ADR-0018, ADR-0033). A Driver whose
tool reads no such file returns empty, and every surface that would show an
editor says so instead of offering one that writes nowhere.

**Owl injects nothing.** It writes the file; the tool reads it as it would any
other user-level instruction file. That is the whole of the mechanism, and it
is why this is a file rather than more text appended to a system prompt.

Reading and writing go through the daemon, so the CLI (`owl account
instructions show | set | edit`) and the desktop app take the same path and
cannot diverge (ADR-0004, ADR-0009).

The file is written in one step - in full beside the real one, then moved over
it - so a Run starting mid-save reads either what was there before or what was
just written, never half of either. It is the owner's alone, like everything
else in that directory. Text that is blank takes the file away rather than
leaving an empty one, so an Account with nothing to say reads as one that was
never given any. A name in that place that is not a regular file is refused
rather than written through: following a symlink would have Owl write outside
the Account's directory, into the user's own setup, which is the one thing an
Account exists to keep separate.

## Consequences

- Conventions that hold across an Account are stated once, and a new Project on
  that Account inherits them without anybody remembering to copy them.
- This is a third place an Agent's behaviour comes from, after the unattended
  contract and the Project's own clauses. ADR-0017 already warned that text the
  user did not write is a hidden variable; this is text they *did* write, but
  in a place they may not be looking at when they read a Job's diff. Today it
  is visible on demand - `owl account instructions show` and the desktop app -
  but not among a Run's recorded inputs, where `owl jobs show` prints the
  effective system prompt. Folding it in there is follow-up work, and it waits
  on that command learning to print what a Run was actually given rather than
  what today's configuration would give it.
- Its reach is wider than anything else Owl edits: one file changes every Job
  on that Account, across every Project. Both surfaces say so where it is
  edited, because nothing about a text box implies it.
- **An Agent can write to it.** An unattended Agent is granted `Write` and
  `Edit` with no path restriction (ADR-0035), its process has
  `CLAUDE_CONFIG_DIR` in its environment, and there is no sandbox (ADR-0006) -
  the worktree is a convention, not a boundary. So an Agent can rewrite the
  standing instructions of the Account it is running on, and thereby affect
  every later Run on that Account in every Project. This is not new with this
  ADR: the same reach already existed for `settings.json` in the same
  directory, where an Agent could widen its own allowlist. It is written down
  here because this ADR makes the directory a place users deliberately keep
  things, which makes the existing hole worth naming. Closing it needs the
  Executor to bound what an Agent can reach (ADR-0006's deferred container),
  not a rule in a file the Agent can also edit.
- Because it is the tool's own file, Owl inherits whatever that tool does with
  it. For `claude-code` the relocation is documented: setting
  `CLAUDE_CONFIG_DIR` puts every `~/.claude` path under that directory instead,
  and `CLAUDE.md` is one of them. Owl's own half - that an Agent for an Account
  is started against a directory holding the file - is what the behaviour tests
  pin, and it is all Owl can pin. A Driver for a tool that decided differently
  would need its own answer here, which is why the filename is the Driver's.

## Alternatives considered

**Append them to the system prompt per Account**, as ADR-0017 does for the
Project's clauses. Would work for every Driver uniformly, would appear in `owl
jobs show`'s effective system prompt for free, and could not be edited by an
Agent that lacks the file. Rejected because it makes Owl the carrier of text it
has no business reading, duplicates a mechanism the tool already has, and puts
per-Account text through a per-Run flag - which means it is Owl's prompt
assembly, not the user's file, that decides what an Account says.

**A file Owl owns, in its own format, rendered into whatever each Driver
wants.** More portable across tools. Rejected as a layer with nothing in it:
the file is prose either way, and every Driver Owl has or plans reads prose
from a file in its configuration directory.

**Let a Project point at an Account-level file itself.** Keeps everything per
Project and needs no new concept. Rejected because it inverts the thing being
asked for - the point is that a new Project inherits the conventions without
being told to.
