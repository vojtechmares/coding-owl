# ADR-0033: Skills are fetched natively in Go and placed per Driver

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

ADR-0024 decided that skills are declared per Project, pinned, and managed by
Owl. It did not say how they are fetched or where they land - and it was wrong
about the latter, claiming Owl hands them to `claude-code` through
`--plugin-dir`. Claude Code loads skills from `.claude/skills/`, which is a
different thing from `~/.claude/plugins/`.

The ecosystem has meanwhile settled its conventions around the `skills` CLI
(`vercel-labs/skills`): `SKILL.md` with `name` and `description` frontmatter,
`owner/repo` and git-URL sources, and a published table mapping each agent to
its skills directory. What it does **not** have is version pinning or a
lockfile - `skills update` means "go to latest", which ADR-0024 forbids.

## Decision

### Native, not delegated

Owl resolves, fetches and places skills itself, in Go. It does not shell out to
`npx skills`. Owl ships as a single binary through a Homebrew formula, its
daemon runs under launchd where the user's interactive `PATH` does not apply,
and the delegated CLI's update semantics contradict ADR-0024.

### Ecosystem conventions are adopted

`SKILL.md` and its frontmatter, the source shorthands (`owner/repo`, git URL,
local path), and the per-agent directory table - which is exactly
`Driver.SkillsDir()` from ADR-0018:

| Driver | Skills directory |
| --- | --- |
| `claude-code` | `.claude/skills/` |
| `pi` | `.pi/skills/` |
| `opencode` | `.agents/skills/` |

A skill is therefore portable across Drivers at no cost.

### Placement

Skills are materialised into a content-addressed cache at
`~/.local/share/coding-owl/skills/<digest>/` and **symlinked** into
`<worktree>/<Driver.SkillsDir()>/`.

To keep them out of the review diff, Owl enables `extensions.worktreeConfig` on
the repository and sets `core.excludesFile` **per worktree** to an Owl-owned
exclude file. This hides the path inside Owl's worktree while leaving the user's
own checkout completely unaffected.

### Lockfile

`.coding-owl.lock.yaml`, discovered in the same three locations as the config -
repository root, `.config/`, or `.meta/` - and read from the **base branch**,
exactly as ADR-0014 reads config, so an Agent cannot pull a different skill
version by editing the lock in its worktree.

The manifest carries intent (`ref: main`, `ref: v1.4.0`); the lock carries
identity (resolved commit sha and content digest). `skills:` is a top-level
section, so Driver versions or container images can be locked later without a
second lockfile.

### Commands

`owl skills add | list | remove | update`, modelled on the ecosystem CLI.
Skills are per Project only (ADR-0024); there is no global scope.

## Consequences

- Skills never appear in a review diff and never trip garbage collection's
  unfinished-work check (ADR-0015).
- Enabling `extensions.worktreeConfig` writes to the configuration of a
  repository Owl does not own. It is benign - it only enables per-worktree
  config - but it is a write, and it should be done once and idempotently.
- Setting `core.excludesFile` per worktree **shadows the user's global ignore
  file** inside that worktree. Owl's exclude file must carry forward whatever
  was already configured, or global ignores silently stop applying.
- Owl reimplements source resolution and private-repository authentication that
  the ecosystem CLI already solved. Git over SSH and the credential helper cover
  the realistic cases; there is no `gh`-style fallback chain.
- There is no registry search: you name a source. `owl skills find` arrives with
  the skills.sh API, which is the intended next step rather than a rejected one.
- A repository that already keeps committed skills in `.claude/skills/` still
  works. Owl manages only the entries it created and refuses to overwrite a path
  it does not own.

## Alternatives considered

**Delegate to `npx skills`.** Inherits the registry, interactive search, 79
agent targets and a well-tested private-repo authentication chain - a large
amount of working code not to write. Rejected for the Node dependency, the
launchd `PATH` problem, and update semantics that contradict ADR-0024.

**Native, plus the skills.sh API for discovery.** Deferred rather than rejected;
it is the planned follow-up once the fetching path exists.

**Materialise into the Account's configuration directory** instead of the
worktree, using the Driver's global skills path. Keeps the repository untouched
with no git configuration tricks at all. Rejected because an Account is shared by
several Projects and by concurrent Runs (ADR-0021), so a single global skills
directory would have to be rebuilt per Run and would race between them.
