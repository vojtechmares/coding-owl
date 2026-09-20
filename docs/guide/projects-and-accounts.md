# Projects and Accounts

A Project is a git repository Jobs are queued against. An Account is a
subscription Owl runs those Jobs on. Both are set up once and then mostly left
alone; this page is what you reach for when that changes - a repository that
moved, a second subscription, an MCP server an Agent needs, a convention every
Run should follow.

[Getting started](getting-started.md) registers one of each in two commands.
What follows is the rest of it.

## Registering a Project

```
owl project add ~/code/my-app
```

The Project is named after the directory and its base branch is the
repository's current branch. Both can be said outright, and the name has to be
unique: it is the Project's identity and it names the Project's configuration
directory.

```
owl project add ~/code/my-app --name api --base-branch main
```

The base branch matters more than it looks. Job branches are cut from it, they
are rebased onto it at the start of every Run, and the Project's configuration
and its Verification checks are read from it - never from a Job's worktree, so
an Agent cannot weaken the checks that judge it.

## Giving a Project an Account

A Project that names no Account cannot run a single Job, and a freshly
registered one names none. `owl project add` says so and points here:

```
owl project setup api
```

It asks two things. Which Account the Project's Jobs run on - it lists the ones
you have registered - and where the configuration file should go:

- **in the repository**, as `.coding-owl.yaml`, which is committed and travels
  with the repository to every machine that clones it;
- **in Owl's configuration home**, at `~/.config/coding-owl/<name>/config.yaml`,
  which is local to this machine and is never committed.

Then it writes those two settings and nothing else. The choice matters more
than it looks: the configuration home file is read from disk, so it is in force
the moment it is written, while an in-repo file is read from the Project's base
branch. Writing `.coding-owl.yaml` into your working tree changes nothing until
you commit it, and setup says so rather than committing on your behalf.

The Project may be named by name, or by any path inside it, or left out
entirely when you are already in its directory:

```
owl project setup                # the project you are standing in
owl project setup ~/code/my-app  # or one named by path
```

A Project that already has a configuration file anywhere in the discovery
order is left alone: what else that file carries is yours, and setup is not the
thing to merge into it.

Everything else a configuration file can carry - the checks that judge a Run,
what an Agent may do, the Skills it reads - is in the
[guide](../../README.md#configuration). Setup points you at it and suggests
handing that part to a coding agent.

## Seeing what is registered

```
owl project list
```

Every Project with its path and base branch.

```
owl project show api
```

The same, plus the configuration in force: which file it was read from, the
branch prefix Job branches are named under, and the Account its Jobs run on. A
Project with no configuration file anywhere in the discovery order says so and
runs on the defaults.

## Moving, renaming and deregistering

A repository that moved on disk needs Owl told, because the daemon resolves
nothing on your behalf:

```
owl project move api ~/work/api
```

Only the path changes. The name is the Project's identity, so the queued work,
the history and the configuration directory stay where they are. Changing the
name instead moves the configuration directory with it:

```
owl project rename api api-server
```

```
owl project remove api-server
```

Deregistering takes the Project's Jobs with it, queued or not, since a Job
whose Project is gone has nowhere to run. The configuration directory under the
config home is left alone: it is hand-written, and nothing else can put it back.

## Accounts

An Account is a subscription with a tool configuration directory of its own, so
that a night of Owl's work never touches your own setup. Its token is kept in
the OS keychain and the database holds only a reference to it
([ADR-0019](../adr/0019-accounts.md)).

```
owl account add work
```

That runs the coding tool's own token setup against the Account's directory and
takes the long-lived token it prints. A machine that has a token already skips
the browser round trip, and `--driver` names the coding tool for an Account
that is not on the default one:

```
owl account add work --token-stdin
owl account add work --driver claude-code
```

A Project names the Account its Jobs run on in its configuration file, as
`account: work`.

```
owl account list
```

Every Account with its Driver, whether the credential store still holds its
secret, whether work on it was recorded as allowed to fail over to another
Account, and where its configuration directory is. An Account that has lost its
credential cannot run anything, and the listing says so rather than showing a
blank. Failover itself is recorded and unused today
([ADR-0019](../adr/0019-accounts.md)); `--failover` on `owl account add` sets
the flag and nothing acts on it yet.

```
owl account remove work
```

The token comes out of the keychain with it. The configuration directory is
left where it is - removing an Account is not a reason to destroy a tool's
state, and adding the Account again picks it up. An Account any Job ran on is
refused outright: what a Job drew on stays knowable for its whole life
([ADR-0023](../adr/0023-job-account-binding.md)).

## Configuring an Account with its own tool

An Account's configuration directory is its coding tool's configuration
directory, so the tool's own commands are how that Account is configured - its
MCP servers, its plugins, its settings. Owl models none of those:

```
owl account exec work -- mcp add --scope user sentry --transport http https://mcp.sentry.dev/mcp
owl account exec work -- plugin install some-plugin
owl account exec work -- mcp list
```

Everything after `--` is the tool's, passed through untouched, and the tool's
exit status is the command's.

Two things to know about an MCP server an Agent is meant to use. An unattended
Agent denies anything not on its allowlist, so the server's tools have to be
granted as `mcp__<server>__*` in the Account's `settings.json` or in a
Project's `allowedTools`
([ADR-0035](../adr/0035-agent-permissions-allowlist.md)). And a repository's own
`.mcp.json` servers wait for an approval nobody is awake to give, so user scope
- what `--scope user` writes, in the Account's own directory - is the one that
works for a Run.

## Standing instructions

An Account carries standing instructions that every Run on it reads, whichever
Project the Run is for. They are the place for conventions that hold across all
of an Account's work - how to write a commit message, what never to touch - as
against a Project's `unattendedClauses`, which hold only for that Project
([ADR-0037](../adr/0037-account-standing-instructions.md)).

```
owl account instructions edit work
owl account instructions set work < CLAUDE.md
owl account instructions show work
```

`edit` opens them in `$VISUAL`, `$EDITOR` or vi, and what the editor is left
holding is what is saved. `set` reads them from standard input. `show` prints
them, and prints where they are to standard error, so that piping one command
into the other writes the instructions and nothing else.

Owl keeps them in the Account's configuration directory under the name that
Account's tool reads instructions from - `CLAUDE.md` for Claude Code - so the
tool reads them itself and Owl injects nothing. Saving blank text takes them
away. One file changes every Job on that Account, in every Project, so it is
worth keeping short.

To read them back as an Agent on that Account would, ask it:

```
owl account exec work -- --print "what standing instructions are you under?"
```

## Where to go next

- [Working with Jobs](jobs.md) - queueing, ordering, following a Run, and
  deciding what to do with the work.
- [Guide](../../README.md) - the risks, the install, every configuration field
  and the full command list.
