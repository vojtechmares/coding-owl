# ADR-0031: A Project is identified by a name, not by its path

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

ADR-0014 left this open, noting only that the slug "needs deriving from the
Project path with collision handling, and must be stable, because it names a
directory". It is also a primary key, and `owl project add` is in the first
milestone (ADR-0029), so it stops being deferrable.

## Decision

A Project is identified by a **name**. `owl project add <path>` proposes the
directory's basename and accepts `--name` to override; uniqueness is enforced at
registration.

The filesystem path is an ordinary mutable attribute. `owl project move
<name> <path>` updates it, and identity is unaffected.

## Consequences

- A collision - two client repositories both called `api` - surfaces at
  registration, when the fix is one flag, rather than as a silent overwrite of
  someone's configuration directory.
- Moving or renaming a repository does not orphan its configuration, its Job
  history or its queued work.
- `~/.config/coding-owl/<name>/` stays readable, which matters because it is a
  directory people open by hand.
- Names are a namespace to be governed. Renaming a Project means moving a
  configuration directory, so `owl project rename` has to exist and has to be
  transactional rather than left as an exercise.
- Nothing connects a Project on one machine to the same repository on another.
  That is fine while daemons are local (ADR-0004), and is the first thing a
  remote Runner would need to solve.

## Alternatives considered

**Derive the slug from the absolute path**, as Claude Code does for its own
session directories. Collisions become impossible by construction and nothing
needs naming. Rejected because the slug is then unreadable, and moving a
repository silently orphans everything attached to it.

**Use the git remote URL.** The only option under which the same repository is
the same Project across machines, which is what a remote Runner would want.
Rejected because local-only repositories have no remote, repositories with
several remotes are ambiguous, and renaming on the forge changes the identity.
