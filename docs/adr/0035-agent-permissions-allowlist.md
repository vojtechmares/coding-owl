# ADR-0035: An unattended Agent is granted a default allowlist, once, per Account

- **Status:** Accepted
- **Date:** 2026-09-14

## Context

An Agent runs unattended (ADR-0017). Nobody is there to answer a permission
prompt, so Owl starts Claude Code with prompts disabled: anything that would
have asked is denied. ADR-0006 says to prefer the tool's own allowlist over
skipping permissions outright, and said nothing about who writes that
allowlist.

Nobody did. Nothing granted an Agent anything, so on a fresh install every
file edit and most commands were denied, and a Job ended in `blocked` or
`review` with an empty diff. The behavioural suite could not see it, because
the stub agent accepted whatever it was started with.

Claude Code reads what it may do from `settings.json` in its configuration
directory, and each Account has a configuration directory of its own
(ADR-0019). It also takes additional rules on the command line.

## Decision

**The Claude Code Driver seeds a fresh Account's `settings.json` with a default
allowlist, and never writes to one that exists.** The file is created before
the first Agent runs on the Account, and created rather than written over, so
a file the user made in the meantime, or one another Run seeded a moment
earlier, is left exactly as it is. From the moment it exists, the file is
the user's: Owl reads it to know what an Agent was allowed, and does not
re-impose the default, extend it, or repair it.

The default allowlist is:

- the edits an Agent exists to make: `Edit`, `Write`, `MultiEdit`,
  `NotebookEdit`;
- reading what is there: `Read`, `Glob`, `Grep`, `LS`;
- the commands a Job's work turns on - version control and the build and test
  runners a Project is likely to have: `Bash(git:*)`, `Bash(make:*)`,
  `Bash(go:*)`, `Bash(npm:*)`, `Bash(pnpm:*)`, `Bash(yarn:*)`, `Bash(npx:*)`,
  `Bash(cargo:*)`, `Bash(pytest:*)`.

Nothing that reaches the network, and no shell beyond those prefixes. Anything
wider is a decision for whoever owns the work, made in one of two places:

- **the Account's file**, for everything that Account's Agents may do, edited
  by the user by hand;
- **the Project's own file**, `allowedTools` in `.coding-owl.yaml`, for what
  that Project's Agents may do on top, passed to the tool as its allowed
  tools for the Run. It extends; it cannot take anything away.

**Every Run records the permissions its Agent had** - the Account's list as
it stood, then the Project's - and `owl jobs show` prints them per Run, so
what an Agent did is attributable to what it was allowed, as ADR-0024 does
for what it read.

The stub agent of the behavioural suite refuses a print-mode invocation that
was given no settings file, one that allows nothing, or prompts left enabled,
so the suite can no longer pass while a real Agent would deny everything.

## Consequences

- A fresh install works: the first Run on a new Account can edit files and run
  the Project's build and tests without anybody granting anything by hand.
- The default is a starting point, not a policy Owl enforces. A user who
  narrows or widens their Account's file is not second-guessed, and a user
  who deletes it gets the default seeded again on the next Run.
- `--dangerously-skip-permissions` stays out: what an Agent may do is always a
  list somebody can read. Per-Job overrides are not offered; a Job that needs
  more than its Project grants is a Project decision.
- The default names commands by prefix. A Project whose runner is not on the
  list adds it in its own file, which is committed and reviewed like any other
  change to what the Project's Agents may do.
- A Run's recorded permissions are what the tool was told, not a log of what
  it used. The Run's log holds that.

## Alternatives considered

**Skip permissions outright.** The simplest thing that works, and the thing
ADR-0006 says not to do: an unattended Agent with everything granted is bounded
by nothing but the worktree.

**Pass the whole allowlist on the command line every Run.** Nothing to seed
and nothing the user could edit by mistake - and nothing the user could edit on
purpose either. The tool's own file is where its users expect to find and
change what it may do.

**Re-seed or merge the default into an existing file.** Keeps every Account
current as the default changes, at the cost of overwriting a decision the
user made. A file the user has is theirs.
