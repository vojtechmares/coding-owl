# Issue #16: Skills: declare per Project, native fetch, cache, symlink placement with per-worktree excludes, lockfile, versions per Run, owl skills

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on its `PATH` in place of Claude
Code. The XDG layout, the temporary repository, the stub and the harness
Account are as `tests/behavior/issue-5.md` and `tests/behavior/issue-14.md`
describe them.

A Skill is reusable instruction a Project gives every Agent that works in it
(ADR-0024). Owl fetches it itself, in Go, into a content-addressed cache and
symlinks it into the Driver's own skills directory inside the Job's worktree
(ADR-0033). The manifest carries intent - a source and a ref - and the lockfile
carries identity: the commit that ref resolved to and the digest of what was
fetched.

"Skill source" means a temporary git repository under the XDG layout carrying a
`SKILL.md` with `name` and `description` frontmatter, at least one commit on
`main` and the tag `v1.0.0` on an earlier one, so that a pinned Skill and a
tracking Skill differ. Skills are named by their source: `owner/repo` and a
local path both yield the last path element.

## Scenarios

### S1 - owl skills add records the source, the ref and what it resolved to
Given a running daemon and a registered Project, and a Skill source whose `main` carries two commits
When `owl skills add <source>` runs in the Project
Then it exits 0
And stdout names the Skill and the commit it resolved to
And the Project's configuration gains a `skills` entry with that source and the ref `main`
And `.coding-owl.lock.yaml` beside it records that Skill's resolved commit and a digest

### S2 - owl skills add pins to the ref it is given
Given a Skill source carrying the tag `v1.0.0` on an earlier commit than `main`
When `owl skills add <source> --ref v1.0.0` runs
Then the manifest records the ref `v1.0.0`
And the lockfile records the commit that tag points at, which is not the commit `main` points at

### S3 - owl skills list tells a pinned Skill from a tracking one
Given a Project with `go-review` added at `v1.0.0` and `house-style` added with `--auto-update`
When `owl skills list` runs
Then it reports both, with their sources, their refs and the commits they resolved to
And it says `go-review` is pinned and `house-style` tracks its ref

### S4 - owl skills list with none says so
Given a registered Project with no Skills
When `owl skills list` runs
Then it exits 0 and says the Project has no Skills

### S5 - owl skills add refuses a source it cannot read
Given a running daemon and a registered Project
When `owl skills add <a path that is not a repository>` runs
Then it exits with a non-zero code
And stderr names the source
And the Project's configuration is unchanged, and no lockfile was written

### S6 - owl skills add refuses a ref the source does not have
Given a Skill source with no branch or tag called `nope`
When `owl skills add <source> --ref nope` runs
Then it exits with a non-zero code
And stderr names `nope`
And nothing was added

### S7 - owl skills add refuses a source that carries no SKILL.md
Given a git repository with no `SKILL.md` in it
When `owl skills add <that repository>` runs
Then it exits with a non-zero code
And stderr says a Skill needs a SKILL.md
And nothing was added

### S8 - a Run materialises the Skills into the Driver's skills directory
Given a Project with one Skill added, and a pending Job against it
When `owl start` runs and the Run finishes
Then `<worktree>/.claude/skills/<skill>` exists and holds the Skill's `SKILL.md`
And its content is the content of the commit the lockfile records

### S9 - the Skills are invisible to git and the user's checkout is untouched
Given the Run of S8
Then `git status --porcelain` in the Job's worktree reports nothing
And `git status --porcelain` in the Project's own checkout reports nothing
And the Project's own checkout has no `.claude/skills` directory

### S10 - a global excludes file the user already had still applies
Given a Project whose repository is configured with a `core.excludesFile` of the user's own, ignoring `notes.txt`
When a Run has materialised the Skills and a `notes.txt` is written in the worktree
Then `git status --porcelain` in the worktree reports neither the Skills nor `notes.txt`
And the repository's own `core.excludesFile` is what it was

### S11 - editing the lockfile in the worktree changes nothing
Given a Project with one Skill pinned, whose Job has run once
When the lockfile in the Job's worktree is edited to name another commit and digest, and the Job runs again
Then the Skill materialised for the second Run is still the commit the base branch's lockfile records
And `owl jobs show <job>` records that commit for the second Run, not the one the worktree's lockfile names

### S12 - a Run records the Skill versions it ran with
Given the Run of S8
When `owl jobs show <job>` runs
Then it prints a skills section naming the Skill, its ref and the commit that Run used

### S13 - owl skills update re-resolves a tracking Skill and leaves a pinned one
Given a Project with `go-review` pinned at `v1.0.0` and `house-style` tracking `main`, and a new commit on each source's `main`
When `owl skills update` runs
Then the lockfile's commit for `house-style` is the new one
And the commit for `go-review` is unchanged
And stdout says which Skill it updated

### S14 - owl skills update takes a Skill by name, pinned or not
Given the Project of S13, and the tag `v1.0.0` moved to a later commit of its source
When `owl skills update go-review` runs
Then the lockfile's commit for `go-review` is what `v1.0.0` now resolves to, which is not what it was
And stdout says it updated `go-review`
And `house-style` is unchanged

### S15 - an auto_update Skill is re-resolved when a Run starts
Given a Project with an `auto_update` Skill, whose Job has run once, and a new commit on that source
When the Job runs again
Then the Skill materialised for the second Run is the new commit
And `owl jobs show <job>` records the new commit for that Run

### S16 - a pinned Skill is not re-resolved when a Run starts
Given a Project with a Skill following `main` and not declared to update on its own, whose Job has run once, and a new commit on that source's `main`
When the Job runs again
Then the Skill materialised for the second Run is still the commit the lockfile records, not the new one
And `owl jobs show <job>` records that commit for both Runs

### S17 - Owl refuses to overwrite a skills entry it did not create
Given a Project whose base branch carries a committed `.claude/skills/go-review` directory of its own, and a Skill named `go-review`
When `owl start` runs
Then it exits with a non-zero code
And stderr names the path and says Owl did not create it
And the committed directory is untouched, and the Job is still pending with no Run

### S18 - owl skills remove takes the Skill out of the manifest and the lockfile
Given a Project with two Skills
When `owl skills remove go-review` runs
Then it exits 0
And neither the manifest nor the lockfile mentions `go-review`
And the other Skill is untouched
And a Run afterwards materialises only the other Skill

### S19 - owl skills remove refuses a Skill that is not there
Given a Project with one Skill
When `owl skills remove nothing` runs
Then it exits with a non-zero code
And stderr names `nothing`

### S20 - the Skills commands report a stopped daemon
Given a temporary XDG layout with no daemon running
When `owl skills list` runs
Then it exits with a non-zero code
And stderr says the daemon is not running and names the socket path
