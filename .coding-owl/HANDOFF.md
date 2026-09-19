# Handoff - issue #134

`owl project show` should report a stray/misnamed config file in the
config-home fallback directory. `vojtechmares/coding-owl#134`, labels `bug`,
`ready-for-agent`.

Branch: `owl/job-25`, which is exactly `origin/main` (`b2bfd3e`) right now, so
nothing needs rebasing before the first commit.

## State right now

Planned only. No production code, no tests, no spec sheet yet. This file is the
whole of what the planning Run produced; everything under "Steps left" is still
to do.

Blocker check done first, as the Job prompt asks, and the issue is **not
blocked**: state `OPEN`, labels `bug` + `ready-for-agent`, no `blocked_by`
dependencies, no open sub-issues, no open cross-referenced pull request, and
nothing in the body or the two comments names another issue that must land
first. Both comments are the reporter's own: the first raised the priority, the
second confirmed the root cause (wrong filename) and **rescoped the issue to
detection and reporting only**. The rescoped body is the specification - the
earlier "the account is not picked up at all" framing is dead.

## What the issue asks for

When the per-Project config-home directory (`<config home>/coding-owl/<name>/`,
ADR-0014 form 4) holds a file that looks like it was meant to be the Project's
configuration but is not named `config.yaml`, `owl project show <name>` says so
on its existing `config:` line, e.g.

```
config: (none) - found .coding-owl.yaml in /Users/x/.config/coding-owl/api, but this location expects config.yaml
```

Report **only**. Nothing is loaded, renamed or moved, and the discovery order
and file names of ADR-0014 are untouched. Trigger only when discovery fell all
the way through to `config.Default()` and that directory holds at least one
other `*.yaml`/`*.yml` file. The in-repo forms (1-3) are explicitly out of
scope.

## What the code does today

- `internal/project/project.go`
  - `discover()` (line 243) walks `inRepoCandidates` off the base branch, then
    reads `s.configPath(name)` = `<configDir>/config.yaml`, then returns
    `("", config.Default(), nil)`. An empty source with no error happens on
    that last line and nowhere else, which is the exact "fell through" signal
    the issue asks to key off.
  - `configDir(name)` (line 291) and `configPath(name)` (line 294) are the
    directory and the expected file. `configFileName` is the const at line 23.
  - `Details` (line 88) carries `ConfigSource`, empty when nothing was found.
- `internal/daemon/project.go:53` copies `ConfigSource` into
  `codingowlv1.ProjectConfig{Source: ...}`.
- `internal/client/project.go:22` maps that proto message to
  `client.ProjectConfig`.
- `internal/cli/project.go:118` prints `(none)` (`noConfigFound`, line 17) when
  `Source` is empty.

The one non-obvious neighbour: `skill.LockName` is `.coding-owl.lock.yaml`
(`internal/skill/skill.go:35`) and **Owl itself writes it into that same
directory** (`Service.lockPath`, `FilesFor`). It is a `*.yaml` file that is not
a near miss, so it must be excluded or every Project with a fallback lockfile
and no config would get a false report.

## Design decided

### Detection, in `internal/project`

A new unexported method rather than a fourth return value from `discover()`,
because `discover()` is called from exactly one place (`Show`, line 169) and
its signature is already at three:

```go
// strayConfig is a file in the Project's configuration directory that looks
// like it was meant to be its configuration but is not the name this location
// reads (ADR-0014 form 4). It is reported, never loaded, and the file is
// never opened - only the directory is listed.
func (s *Service) strayConfig(name string) string
```

- Lists `s.configDir(name)` with `os.ReadDir`. **Any error means no report**,
  including a missing directory and a permission error: a diagnostic that can
  turn a working `owl project show` into a failure is worse than the problem it
  reports.
- Skips directories, skips `configFileName` and `skill.LockName`, keeps names
  whose lowercased extension is `.yaml` or `.yml`.
- With several candidates, returns the first of `.coding-owl.yaml`,
  `coding-owl.yaml`, `config.yml` that is present, else the lexically first.
  Deterministic output matters more than the exact order, and those three are
  what a person who got the name wrong most likely typed.
- Returns an absolute path (`filepath.Join(s.configDir(name), n)`) or `""`.

`Details` gets `StrayConfig string`, and `Show` sets it **only when
`ConfigSource == ""`**, which is the issue's trigger condition read literally.

### Across the daemon boundary

`ProjectConfig` in `proto/codingowl/v1/project.proto` gets

```proto
  // StrayConfig is the absolute path of a file in the Project's configuration
  // directory that looks like it was meant to be its configuration but is not
  // named config.yaml, so nothing read it (ADR-0014 form 4). Empty when there
  // is none; reported only, never loaded.
  string stray_config = 4;
```

Adding a field is not a breaking change, so `buf breaking` in CI stays green.
One absolute path is carried rather than a formatted sentence, so that the
wording stays in the CLI where the rest of the wording is, and so that the
daemon does not have to guess how the caller's terminal wants it. The CLI
cannot do the directory listing itself: it is not guaranteed to have the
daemon's `XDG_CONFIG_HOME`.

Then `internal/daemon/project.go` sets it from `d.StrayConfig`, and
`internal/client/project.go` adds `StrayConfig` to `ProjectConfig` and reads
`GetStrayConfig()`.

### The printed line

A small pure helper in `internal/cli/project.go`, so the wording is testable
without a daemon:

```go
func configLine(source, stray string) string
```

- `source != ""` - the source, exactly as today.
- `source == ""`, no stray - `noConfigFound`, exactly as today.
- `source == ""`, stray - `(none) - found <file> in <dir>, but this location
  expects config.yaml`.

`<file>` and `<dir>` go through `terminalSafe` (`internal/cli/safe.go`): the
name comes from whatever happens to be in that directory, and `internal/cli/
run.go` already puts every filesystem-derived string through it. The expected
name is a local const in `internal/cli` with a comment pointing at ADR-0014;
`internal/cli` does not import `internal/project` today and should not start
for one string.

## Decisions made on my own (repeat these in the PR body)

1. **An in-repo configuration silences the report.** The issue says "trigger
   only when the expected `config.yaml` is absent (`discover()` has fallen
   through to the default)", and discovery never reaches form 4 when a form 1-3
   file exists. Taken literally: a Project configured in its repository says
   nothing about a leftover file in its config-home directory. The alternative
   - always scanning - reports a file that could not have been used anyway and
   was not asked for.
2. **One file is named even when several are there**, by the preference order
   above. Listing all of them makes the line unreadable and the failure it
   describes is almost always one file.
3. **`.coding-owl.lock.yaml` is never a near miss.** Owl writes it there
   itself; reporting it would fire on Projects that are working as designed.
4. **Errors listing the directory report nothing rather than failing.** See
   above.
5. **`terminalSafe` is applied to the new parts of the line only.** Wrapping
   the existing `source`, name and path output too would be a behaviour change
   outside this issue; worth its own issue, not this diff.
6. **One doc sentence.** The existing paragraph about `owl project show` in
   `docs/guide/projects-and-accounts.md` (line 44) gets the near-miss case
   added to it. ADR-0014's own consequence is that "a config that is not being
   picked up is a support question", so the page that answers it should say
   this exists. Nothing new is created; the sentence is extended.

## Ruled out

- **Loading the file anyway**, or renaming it to `config.yaml`. The issue
  forbids both in as many words.
- **Adding the near-miss name to the discovery order.** That is ADR-0014's
  decision to change, not this issue's.
- **Doing the scan CLI-side to avoid a proto field.** The CLI's environment is
  not the daemon's, so it would report about the wrong directory whenever they
  differ.
- **Overloading the existing `source` field with the message.** ADR-0014
  requires `owl project show` to print which file was loaded; a `source` that
  sometimes holds prose stops answering that.
- **A fourth return value on `discover()`.** Noise on a function that four
  other things already read.

## Behavior spec sheet to write

`tests/behavior/issue-134.md`, with `TestS<k>...` in
`tests/behavior/issue134_test.go`. The harness is in place already:
`newLayout`, `daemonUp`, `newRepo`, `addProject`, `runOwl`, and
`line(t, out, "config")` for reading one line of the output. There is no helper
that writes an arbitrary file into the config-home directory - `projectConfig`
(`tests/behavior/issue14_test.go:557`) writes `config.yaml` only - so add a
small one in `issue134_test.go` itself and keep it there (a helper with no
caller was deleted from this repo two commits ago, `fd16e55`).

- **S1 - the near-miss file is named.** Given a registered Project with no
  configuration anywhere and `.coding-owl.yaml` in its config-home directory,
  when `owl project show` runs, then it exits 0 and the `config` line names
  `.coding-owl.yaml`, the directory it is in, and `config.yaml` as what that
  location expects.
- **S2 - reported, not loaded.** Given that file sets `branchPrefix: stray/`
  and an account, then `branch prefix` is still `owl/`, `account` is still
  `(none)`, the file is still on disk with its content unchanged, and no
  `config.yaml` appeared beside it.
- **S3 - `.yml` counts.** Given `coding-owl.yml` instead, the `config` line
  names it.
- **S4 - a configuration that was found says nothing.** Given both
  `config.yaml` and `.coding-owl.yaml` in that directory, the `config` line is
  the path of `config.yaml` and carries no near-miss wording.
- **S5 - an in-repo configuration says nothing.** Given `.coding-owl.yaml`
  committed on the base branch and a stray file in the config-home directory,
  the `config` line is `main:.coding-owl.yaml` and carries no near-miss
  wording. (Decision 1.)
- **S6 - files that are not near misses.** Given only `.coding-owl.lock.yaml`
  and `notes.txt` in that directory, the `config` line is exactly `(none)`.
- **S7 - one file is named when several are there.** Given `.coding-owl.yaml`,
  `owl.yaml` and `settings.yml`, the `config` line names `.coding-owl.yaml`.
  (Decision 2.)
- **S8 - a hostile filename is printed harmlessly.** Given a near-miss file
  whose name contains an escape sequence, the output contains no raw `0x1b`
  byte and shows `\x1b` instead.

Unit tests beside them, in `internal/project/project_test.go` (fixture
`newFixture`, `f.configHome`, `writeFile` all exist): stray reported with
defaults still in force; lockfile and non-YAML ignored; nothing reported when
`config.yaml` is present; nothing reported when an in-repo form won; the
preference order; a missing directory reports nothing. And
`internal/cli/project_internal_test.go` for `configLine`'s three branches plus
the `terminalSafe` one - the `*_internal_test.go` suffix is this package's
convention for tests that reach inside it.

## Steps left

1. Read the issue again (`gh issue view 134 --repo vojtechmares/coding-owl
   --comments`) and this file. Re-run the blocker checks from the Job prompt -
   labels can change between Runs.
2. Write `tests/behavior/issue-134.md` and the eight failing tests. Run them,
   see them red, commit both together:
   `test(project): spec the near-miss config report for #134`.
3. Implement, smallest slice first (`tdd` skill):
   a. `proto/codingowl/v1/project.proto` + `make generate` (`buf` is at
      `/opt/homebrew/bin/buf`; `buf.gen.yaml` uses **remote** plugins from the
      BSR, so this step needs network). Commit `gen/` with it - CI runs
      `buf generate && git diff --exit-code -- gen`.
   b. `internal/project`: `strayConfig`, `Details.StrayConfig`, `Show` setting
      it, plus the unit tests.
   c. `internal/daemon/project.go` and `internal/client/project.go` plumbing.
   d. `internal/cli/project.go`: `configLine` + its internal test.
4. `make lint && make test`. Both are what CI runs (`.github/workflows/ci.yml`
   also checks `go mod tidy -diff`, `buf lint`, `buf breaking` and actionlint).
5. Extend the `owl project show` paragraph in
   `docs/guide/projects-and-accounts.md` (decision 6).
6. Self-review against the issue and `git diff main...HEAD`.
7. The three verification agents in parallel, fresh verdict directory outside
   the repo, `.agents/skills/bdd/scripts/wait-verdicts.sh` to wait in one turn.
   Fix findings and re-run until all three are `PASS` for the commit they saw.
8. `git push -u origin HEAD` and `gh pr create --repo vojtechmares/coding-owl
   --base main`, body starting `Closes #134`, with the decisions above. Do not
   merge.

## Gotchas found while planning

- **This file is gitignored, and it is committed anyway, deliberately.**
  `b2bfd3e` (yesterday) added `.coding-owl/HANDOFF.md` to `.gitignore` and
  removed it from the index, because tracking it made every open pull request
  conflict. But Owl's own `recordPlan` (`internal/run/run.go:699-708`) reads
  this exact path out of the worktree and commits it with
  `git.CommitPath` -> `git add -- <path>`, which **fails on an ignored,
  untracked path**. A planning Run for this repository therefore cannot succeed
  while the file is both ignored and untracked, so this Run force-added it
  (`git add -f`). Once the path is tracked, gitignore no longer applies to it
  and `recordPlan`'s plain `git add` succeeds, stages nothing, and returns
  cleanly. Do not "tidy" this by deleting the file or by reverting the
  `.gitignore` entry - the first breaks the next Run, the second is the repo
  owner's call. The underlying product bug - Owl cannot plan a Job in a
  repository that ignores its handoff path, and `CommitPath` could force-add
  the path Owl owns - is filed as **#169** and is not part of this diff.
- **Bash permissions in the planning session were narrow.** `which buf`, `gh`,
  `git`, `grep`, `ls`, `sed` were allowed; `buf --version`, `command -v buf`,
  two `cd`s in one command and any write under `/tmp` were auto-denied with no
  way to approve. If `make generate` is denied or the BSR is unreachable, say
  so plainly and stop - `gen/*.pb.go` embeds a raw file descriptor and must not
  be hand-edited.
- The Job's branch is `owl/job-25`, not the `fix/issue-134-...` that
  `AGENTS.md` describes for a person working by hand. Owl made this branch and
  the Job prompt says to push it, so stay on it.
- `make test` runs the whole suite including `tests/behavior`, which builds the
  `owl` binary and starts real daemons. It is slow; run it once per slice, not
  per edit.
