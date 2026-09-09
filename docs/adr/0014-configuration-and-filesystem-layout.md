# ADR-0014: Configuration discovery and filesystem layout

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Per-Project settings - base branch, branch prefix, Verification checks, tool
allowlist - need a home, and so do the daemon socket, the SQLite database, Job
worktrees and Run logs.

One constraint shapes the config decision. Under ADR-0006 there is no sandbox,
and an Agent's working directory *is* its Job's worktree. If Owl read
Verification config out of that worktree, an Agent that could not make the tests
pass could edit the file that judges it.

## Decision

### Project configuration

Discovered in this order, first match wins:

1. `<project>/.coding-owl.yaml`
2. `<project>/.config/coding-owl.yaml` (`.config/.coding-owl.yaml` also accepted)
3. `<project>/.meta/coding-owl.yaml` (`.meta/.coding-owl.yaml` also accepted)
4. `~/.config/coding-owl/<project-slug>/config.yaml`

The in-repo forms (1-3) are read **from the Project's base branch** at Run
start - `git show <base>:<path>` - never from the Job's worktree. An Agent may
edit the file in its worktree; it has no effect on the Run being judged, and the
change shows up in the diff for review like any other.

Form 4 is the fallback for Projects that should carry no Owl file, and for
overrides that should not be committed.

### Filesystem layout

Honouring `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and
`XDG_RUNTIME_DIR` when set, on both macOS and Linux:

```
~/.config/coding-owl/
    config.yaml                  daemon and global settings
    <project-slug>/config.yaml   per-Project fallback config

~/.local/share/coding-owl/
    owl.db                       Jobs, Runs, Projects, Accounts
    worktrees/<job-id>/          one per Job (ADR-0007)
    accounts/<name>/             per-Account tool config (ADR-0019)
    skills/<content-hash>/       materialised skills (ADR-0024)

~/.local/state/coding-owl/
    owld.sock                    or $XDG_RUNTIME_DIR/coding-owl/owld.sock
    logs/<run-id>.jsonl          captured stream-json per Run
```

### Versioning

Every config file carries an `apiVersion`, e.g. `apiVersion: codingowl.dev/v1`.
Owl refuses to load a file whose `apiVersion` it does not recognise rather than
guessing.

## Consequences

- **This supersedes the `~/.coding-owl/owld.sock` path in ADR-0004**, which has
  been amended. No `~/.coding-owl/` directory exists.
- Verification config cannot be weakened by the Agent being verified, which is
  what makes ADR-0013's gate meaningful under host execution.
- Changing a Project's checks is a commit to its base branch. That friction is
  deliberate - it is the same review path as any other change to how the project
  is built.
- The database and worktrees stay out of `~/.config`, which people sync to
  dotfile repos. Worktrees in particular can be large.
- Socket paths have a hard limit near 104 bytes on macOS. Both the runtime and
  state paths are comfortably short; anything nested deeper would not be.
- Four discovery locations is more than one, and a config that is not being
  picked up is a support question. `owl project show <slug>` must print which
  file was loaded.
- `<project-slug>` needs deriving from the Project path with collision handling,
  and must be stable, because it names a directory.

## Alternatives considered

**Config in Owl's store only**, set through `owl project set`. Out of reach of
any Agent and adds no file to the repo, but it is not version-controlled, does
not travel to a second machine, and is invisible to anyone reading the project.
Kept as fallback form 4 rather than as the primary.

**Reading in-repo config from the worktree** rather than the base branch. The
obvious implementation, and the one that quietly breaks Verification.

**Auto-detecting checks from the toolchain** - `go.mod` implying `go test ./...`
and so on - with a committed file as override. Attractive for making
`owl project add` a zero-config command, and a natural follow-up. Left out of
the MVP because it needs a heuristic per ecosystem, each of which will be wrong
for something.

**Platform-native paths** - `~/Library/Application Support` on macOS. More
native for the desktop app, but it costs platform branching in path resolution
and spends the socket path budget.
