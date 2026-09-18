# Handoff - issue #121

Docs: CLI manual (happy paths) and a focused Getting started page, split out of
the all-in-one guide. `vojtechmares/coding-owl#121`, labels `documentation`,
`ready-for-agent`.

Branch: `owl/job-4`, cut from `main` and, at the time of planning, identical to
`origin/main`. Nothing is implemented yet: this commit is the plan.

## Blocker check - clear

Run on 2026-09-18, all four commands from the Job prompt:

- state `OPEN`, labels `documentation`, `ready-for-agent` - in the queue.
- no `blocked_by` dependencies, no sub-issues, no cross-referenced open pull
  request.
- no comments on the issue at all, so no blocker written in prose.
- the body names #127, #124 and #122 under "Related", but as work to coordinate
  with, not work that must land first. #121's own text says "build this one
  first, or coordinate closely, since #127 references it as already existing".
  None of the three has an open pull request, so this branch is the first of the
  docs restructuring to land and gets to set the conventions the others follow.

## What the issue asks for

1. A **Getting started** page: README's `## Quick start` (README.md:121-165, the
   six steps - Account, Project, `.coding-owl.yaml`, queue a Job, let it run,
   review it) as a page of its own, purely that happy path. #127 links forward
   to it, so its id and URL are a contract.
2. **CLI manual page(s)**: the task-oriented happy-path narrative that README's
   `## Command overview` table (README.md:304-324) does not give - queueing and
   reordering Jobs, checking status and reviewing, following a Run, and
   account/project management day to day. "Use judgement on how many pages, the
   point is narrower scope per page, not exactly two files."

Both sourced from content that already exists, not written fresh.

## Decisions taken, with reasons

**D1 - three new pages, under `docs/guide/`.**

| File | id / URL | Title | order |
| --- | --- | --- | --- |
| `docs/guide/getting-started.md` | `getting-started` → `/docs/getting-started` | Getting started | 20 |
| `docs/guide/jobs.md` | `jobs` → `/docs/jobs` | Working with Jobs | 30 |
| `docs/guide/projects-and-accounts.md` | `projects-and-accounts` → `/docs/projects-and-accounts` | Projects and Accounts | 40 |

`docs/guide/` is the directory #127 and #124 both propose; the issue says to use
the same one. The two manual pages follow the issue's own grouping: it lists
queueing, reviewing and following a Run together (one Job's life, one page), and
"account/project management day-to-day" separately. Four pages would split the
Job narrative mid-story; one page would be the all-in-one guide again.

**D2 - move the quick start out of README, do not copy it.** The issue title is
"split out of the all-in-one guide", and its Problem section is that the guide
page is exhaustive. Copying would leave two six-step lists to drift apart, in a
repository whose website deliberately "never holds a copy of what the repo
already says" (`website/src/loaders/repo.ts`:1-6). So README's `## Quick start`
section becomes two or three sentences pointing at
`docs/guide/getting-started.md`, and the six steps live only on the new page.

`## Command overview` **stays in README**: the issue calls it "the full command
list", and says the manual is the narrative it does not provide, not a
replacement for it. Add one sentence under the table pointing at the manual
pages.

**D3 - ordering, with slots reserved for #127 and #124.** The docs collection's
`order` field drives both `/docs` and `/llms.txt`. Renumber in tens so the
sibling issues slot in without touching existing entries, and record the
reserved slots as a comment in `content.config.ts`:

```
10  Install CLI              (#127, reserved)
11  Install Desktop app      (#127, reserved)
20  Getting started          (this issue)
30  Working with Jobs        (this issue)
40  Projects and Accounts    (this issue)
50  Project configuration    (#124, reserved)
51  Daemon configuration     (#124, reserved)
90  Guide (README)           (existing, moved from 1)
```

The guide moves to the end because it stops being the entry point and becomes
the whole thing on one page. Until #127 lands there is no install page, so the
Getting started page opens by pointing at the guide's Installing section.

**D4 - links are written repo-relative, and the loader is taught to resolve
them.** `rewriteRepoLinks` in `website/src/loaders/repo.ts`:35-43 looks a link
target up in `PUBLISHED` verbatim, so `../../README.md` written in
`docs/guide/getting-started.md` - the form GitHub needs - misses the map and
falls through to a raw GitHub URL. Fix: resolve the target against
`path.posix.dirname(file.path)` (normalised) before the ADR and `PUBLISHED`
lookups. `README.md` from the repo root still resolves to itself, so nothing
existing changes. Then add to `PUBLISHED`:

```
'README.md': '/docs/guide'
'docs/guide/getting-started.md': '/docs/getting-started'
'docs/guide/jobs.md': '/docs/jobs'
'docs/guide/projects-and-accounts.md': '/docs/projects-and-accounts'
```

The `README.md` entry is what makes `../../README.md#configuration` land on
`/docs/guide#configuration` instead of GitHub. This is the `PUBLISHED` work the
issue's "Where to look" asks for.

**D5 - the manual is sourced from the cobra help text, not invented.** Every
command already carries a `Long` description written in the project's voice:
`internal/cli/queue.go`:33-45 (`owl add`), :167-170 (`reorder`),
`internal/cli/run.go`:36-41 (`start`), :74-78 (`logs`), :143-152 (`pause`),
:162-165 (`resume`), :216-223 (`accept`), :232-239 (`drop`), :250-255
(`extend`), :286-293 (`jobs show`), plus README's Account sections
(README.md:246-292). Read those and narrate them; do not describe a flag without
finding it in the source first. Flags that exist today: `owl add` has
`--project --plan --no-plan --model --effort --ttl`; `owl queue list` has
`--all`; `owl logs` has `--follow/-f`; `owl jobs accept|drop` have `--force`;
`owl jobs extend` has `--ttl`; `owl project add` has `--name --base-branch`;
`owl account add` has `--driver --failover --token-stdin`; **`owl status` has no
flags**.

**D6 - two accuracy fixes carried into the new page.** README's quick start step
5 says `owl logs -f`, but `owl logs` takes exactly one argument
(`internal/cli/run.go`:79) - the new page says `owl logs <run> -f`, which is
also what `owl start` prints (run.go:62). Step 3's `[Configuration](#configuration)`
is an in-page anchor that stops working once the steps leave README; it becomes
`../../README.md#configuration`. S4 below is the test that catches the first
kind of mistake.

**D7 - use the glossary's words.** Project, Job, Run, Handoff, Idle,
Verification, Account, Agent, Skill, Standing instructions - capitalised as
`CONTEXT.md` capitalises them, which is what README already does. Avoid "task",
"ticket", "cleanup", "quota". Nothing here contradicts an ADR; ADR-0025 (queue
order, no priority) and ADR-0013 (Verification gates completion) are the two the
manual narrates, so link or paraphrase rather than restate.

## Out of scope

README's `## Installing` (#127), `## Configuration` (#124) and `## Desktop app`
(#122) stay where they are. If something else turns up, open an issue; do not
widen this diff.

## Behavior spec sheet - `tests/behavior/issue-121.md`

Write the sheet first, then one failing `TestS<k>` per scenario in
`tests/behavior/issue121_test.go`, and commit the two together while they are
red. `repoDir` (package-level, `tests/behavior/issue2_test.go`:41,50) is the
repository root, and `owlBin` is the binary TestMain builds - S4 needs both.
Precedent for behaviour tests that read the repository rather than drive the
daemon: `TestS3AppUsesOnlySharedClient` and `TestS10ThemeIsOneTokensFile` in
`tests/behavior/issue9_test.go`.

- **S1 - Getting started is a page of its own, and it is the six-step happy
  path.** Given the repository, when `docs/guide/getting-started.md` is read,
  then it is titled `# Getting started` and walks, in this order, adding an
  Account, registering a Project, committing `.coding-owl.yaml`, queueing a Job,
  letting it run or starting it, and reviewing it - each step showing the
  command that does it.
- **S2 - the new pages are published pages with URLs of their own.** Given
  `website/src/content.config.ts`, when the `docs` collection is read, then it
  registers `getting-started`, `jobs` and `projects-and-accounts`, each with a
  title, a description, a source path under `docs/guide/` that exists on disk,
  and an order; the orders are distinct and put all three ahead of `guide`.
- **S3 - the manual covers the everyday commands, in the areas the issue
  names.** Given `docs/guide/jobs.md` and `docs/guide/projects-and-accounts.md`,
  when they are read, then between them they show `owl add`, `owl queue list`,
  `owl queue reorder`, `owl queue remove`, `owl status`, `owl jobs show`,
  `owl jobs accept`, `owl jobs drop`, `owl jobs extend`, `owl logs` with `-f`,
  `owl start`, `owl pause`, `owl resume`, the `owl project` verbs and the
  `owl account` verbs - the Job page carrying the Job ones and the other page
  carrying Projects and Accounts.
- **S4 - every command the new pages show is a command the binary has, with the
  flags it shows.** Given the built `owl` binary, when every `owl …` line in a
  fenced block and every `` `owl …` `` inline span in the three new pages is
  taken, then each resolves to a real command path and every flag it passes
  appears in that command's `--help`.
- **S5 - the pages link to published pages, not to raw GitHub.** Given the three
  new pages, when their relative Markdown links are resolved against the
  directory each file is in, then every target exists in the repository, and
  every target that has a published page - `README.md` and the three new files -
  is in `PUBLISHED` in `website/src/loaders/repo.ts`.
- **S6 - the guide no longer carries a second copy of the quick start.** Given
  `README.md`, when it is read, then it has no six-step quick-start list and no
  `## Quick start` heading followed by numbered steps; it points at
  `docs/guide/getting-started.md` instead, and that link is one the loader
  republishes rather than sending to GitHub.

### How to write S4 (the one with teeth)

1. Build the command tree once: run `owl --help`, parse the `Available
   Commands:` block, recurse into each child with `owl <path…> --help`. Cache
   it. `--help` is served by cobra locally and needs no daemon - confirm that
   early, and if some command does reach for the socket, fall back to parsing
   `Use:` strings out of `internal/cli/*.go`.
2. For each documented invocation: drop everything from a bare `--` onwards
   (`owl account exec work -- mcp add …` hands the rest to the tool), then cut
   at a `#` comment. Walk the tree while the next word is a child's name.
3. Fail when the node reached still has children and the next word looks like a
   command (`^[a-z][a-z-]*$`) but is not one of them - that is the `owl jobs
   list` class of mistake. A placeholder (`<job>`), a flag, a path, a quoted
   string or a number ends the walk legitimately.
4. Collect `-x` and `--flag` tokens from the words before the `--`, strip
   `=value`, and require each to appear in the `--help` output of the command
   path they were used on.
5. Write each documented command on its own line rather than as a `# or: …`
   comment, so the parser and the reader see the same thing.

## File-by-file plan

- `tests/behavior/issue-121.md` - the sheet above. New.
- `tests/behavior/issue121_test.go` - `TestS1…` to `TestS6…`. New.
- `docs/guide/getting-started.md` - new. `# Getting started`, one `##` per step,
  lifted from README.md:121-165 with D6's two fixes, opening with a line
  pointing at `../../README.md#installing` and closing by pointing at
  `jobs.md`.
- `docs/guide/jobs.md` - new. `# Working with Jobs`. Sections: queueing
  (`owl add`, what `--project`, `--no-plan`, `--ttl` are for), the queue
  (`owl queue list`, `reorder`, `remove`; first in first out, no priority,
  ADR-0025), running now and giving the machine back (`owl start`, `owl pause`,
  `owl resume`), following a Run (`owl logs <run> -f`), the morning after
  (`owl status`, `owl jobs show`), and keeping or refusing the work
  (`owl jobs accept`, `owl jobs drop`, `--force`, `owl jobs extend`).
- `docs/guide/projects-and-accounts.md` - new. `# Projects and Accounts`.
  Registering and moving Projects (`owl project add|list|show|rename|move|
  remove`, what `owl project show` answers), Accounts (`owl account add|list|
  remove`, the keychain, `owl account exec` and its `--` passthrough, the MCP
  user-scope note from README.md:262-267), and standing instructions
  (`owl account instructions show|set|edit`, README.md:269-292).
- `README.md` - replace `## Quick start` (121-165) with a short pointer; add one
  sentence under the command table (after 324) pointing at the manual pages.
- `website/src/content.config.ts` - three new `repoFiles` entries, `guide`
  renumbered to 90, and the reserved-slot comment from D3.
- `website/src/loaders/repo.ts` - resolve link targets against the source file's
  directory (D4); four new `PUBLISHED` entries.
- `website/src/pages/docs/index.astro`:41 - the subtitle says "Two places to
  start", which stops being true. Reword without a count.
- `website/README.md`:10-14 - add the three pages to the Page/Source table.
- `.github/workflows/website.yml` - one extra step after the build, in the shape
  of the two already there: assert `dist/docs/getting-started/index.html`
  contains `href="/docs/jobs"` and `href="/docs/guide` and that no
  `blob/main/docs/guide/` link survives anywhere in `dist`. This is the only
  end-to-end proof that D4's rewrite works, since the Go suite cannot run Astro.
  Drop this step if it fights you; the Go tests still cover the source side.

## Commits (small, Conventional, `--signoff`)

1. `docs(agents): plan issue #121` - this file. (done)
2. `test(docs): spec sheet and failing tests for the CLI manual pages` - sheet +
   red tests together.
3. `docs(guide): add the getting started page` - page, registration, README
   trim.
4. `docs(guide): add the CLI manual pages` - the two manual pages and their
   registration.
5. `fix(website): resolve repo links relative to the file they are in` -
   `repo.ts` plus the `PUBLISHED` entries.
6. `ci(website): check the docs pages link to published pages` - the workflow
   step, if it lands.
7. `docs(website): list the new guide pages` - website README table and the
   `/docs` subtitle.

## Verifying before the PR

1. `make lint && make test` from the repository root. That is what CI runs.
2. `cd website && pnpm install --frozen-lockfile && pnpm check && pnpm build`.
   node 26.8.2 and pnpm 12.4.2 are on this machine; `packageManager` pins pnpm
   11.5.2, so corepack may object, and the install needs the network, which the
   sandbox may refuse. If it will not run locally, say so and lean on CI:
   `.github/workflows/website.yml` builds on every pull request touching
   `docs/**` or `website/**`, which this one does.
3. Then the three verification agents from `.agents/agents/` in parallel -
   `security-reviewer`, `correctness-reviewer`, `behavior-verifier` - into a
   fresh `${TMPDIR:-/tmp}/owl-verify/issue-121/round-<r>`, waited on with
   `.agents/skills/bdd/scripts/wait-verdicts.sh`. Re-run after any change; a
   PASS counts only for the commit it saw.

## Finishing

`git push -u origin HEAD`, then `gh pr create --repo vojtechmares/coding-owl
--base main` with a body starting `Closes #121`, the decisions above under
"Decisions made on my own", and the verification verdicts. Wait for CI with
`gh pr checks --watch --fail-fast` - both `ci` and `website` will run. **Do not
merge**; a person accepts the work.

One last piece of coordination the issue explicitly asks for: comment on #127
and #124 naming the ids, URLs and reserved order slots this branch establishes
(`/docs/getting-started` for #127's forward link; slots 10/11 and 50/51 free).
A comment only - never relabel, never edit an issue body.

## What the second run did

The plan above held. Everything in it is built, in the commits it names, with
three corrections worth carrying forward.

**C1 - S4 did not catch the mistake it was written for.** As first committed it
checked that a documented command and its flags exist, which caught the
`owl jobs list` class, but `owl logs -f` is a real command with a real flag and
passed. D6's own example would have survived the test meant to stop it. S4 now
also reads the required `<placeholder>`s off each command's usage line and
requires a fenced-block invocation to supply that many arguments, with the
flags and their values taken out first (the help's Flags block says which flags
take a value). An inline span naming a command mid-sentence - `owl logs` in
prose - is exempt, because it is a name, not something to type. The sheet
gained the matching clause; nothing on it was weakened.

**C2 - the CI step's negative assertion was wrong.** The plan wanted no
`blob/main/docs/guide/` link anywhere in `dist`. Every docs page footer carries
exactly that, on purpose: `website/src/pages/docs/[slug].astro` links "Source:"
at the file the page was read from. The step asserts the three positive links
instead.

**C3 - `owl account exec`'s usage line ends in `-- <command>...`**, so counting
placeholders naively would demand two arguments from
`owl account exec work -- mcp list`, whose tail is another tool's. Both the
usage line and the documented invocation are cut at a bare `--`.

The website build ran locally after all: `pnpm install --frozen-lockfile`,
`pnpm check` and `pnpm build` all pass, pnpm 11.5.2, no corepack objection.
`dist/docs/getting-started/index.html` links `/docs/jobs`,
`/docs/projects-and-accounts`, `/docs/guide#installing`, `/docs/guide#risks`
and `/docs/guide#configuration`, and the two manual pages resolve their ADR
links to `/docs/decisions/*`. D4 works.

`make lint` passes. `make test` fails only
`TestS1CaskTheDesktopBuildProducesASignedAppInAZip`, `TestS2Cask...` and
`TestS3Cask...` from issue #22, all three because the `wails` CLI is not
installed on this machine; they build the desktop release and have nothing to
do with this branch. Everything else passes, the six new scenarios included.

## What verification found

Three rounds, because a PASS counts only for the commit it saw and the fixes
moved the branch each time. The agents could not be given a `$VERDICTS`
directory - this session may only write inside the worktree - so each returned
its verdict as its final message instead.

Rounds one and three each turned up one real error, both in the manual's prose
and neither catchable by S4, which walks commands and flags and not sentences
about them. Rounds two and four were clean.

**Round one, correctness: the manual stated a wrong default.** `--ttl` gets three Runs, not ten: `queue.DefaultTTL` is 3
(`internal/queue/queue.go`:48), applied by `Add` (:184) and by `Extend` (:344)
whenever the flag is unset. D5 said to narrate from the cobra help, and the
cobra help is itself stale - `internal/cli/queue.go`:44,79 and
`internal/cli/run.go`:252,278 all still say ten, left behind by the commit that
made a Job default to three. The pages are fixed; the help text is not, because
a docs issue is the wrong place to change CLI output. Filed as **#141**, with a
comment recording that `docs/adr/0025`:29,46 carries the same stale number.

The lesson for whoever narrates the next page: the help text is the project's
voice, not its truth. Check a number against the constant.

**Two notes acted on rather than deferred.** The homepage's "Follow the quick
start" linked `/docs/guide#quick-start`, which this branch turned into a
three-line pointer - a regression this work created, so it was repaired here
(`website/src/pages/index.astro`:61 now goes to `/docs/getting-started`; the
"Get started" button above it still points at the guide's Installing section,
which is #127's to move). And `owl account list` prints a failover column the
page's list of columns left out.

**Round three, correctness: closing that second note introduced a worse error
than the one it closed.** The new sentence said the flagged Account was one
work may fail over *to*. `--failover` records the opposite - the flagged
Account is the source (`internal/cli/account.go`:131, and the same gloss in
`proto/codingowl/v1/account.proto`:62, `internal/store/accounts.go`:32 and
`internal/account/account.go`:66, all citing ADR-0019). Backwards is worse than
absent: a reader would have set the flag on the spare Account. Fixed in
`6210574`.

Worth carrying forward: an omission is not always worth closing in a hurry, and
a sentence added to satisfy a reviewer's note deserves the same check against
the source as the rest of the page.

**Two left alone, on purpose.** The Account sections of
`projects-and-accounts.md` are near-verbatim copies of README.md:206-252, which
is the drift D2 argues against - but `## Configuration` is #124's to move, and
both reviewers agreed to leave it. Until #124 lands the two copies change
together. And S4 does not catch a bogus word after a leaf command
(`owl status json`): telling that from a legitimate argument needs arity that
`owl skills update [name...]` does not have, and the false positives would cost
more than the gap.

**S1 was tightened twice.** First it tested three weaker things than its own
sheet clause; it now splits the page into `##` sections and requires each
step's command in that step's own section. Confirmed by hand that it fails when
a command is moved out of its section.

## State

- [x] Blocker check
- [x] Plan written and committed
- [x] Spec sheet + red tests
- [x] Getting started page
- [x] Manual pages
- [x] Loader link resolution + PUBLISHED
- [x] Website README, /docs subtitle, CI step
- [x] `make lint && make test`, website build
- [x] Verification agents all PASS, on commit `6210574`, after four rounds
- [x] PR opened - **#142** - and CI green: `test`, `desktop` and the website
      `Build` all pass, `Deploy` skipped as it is for a pull request. The three
      `issue22_test.go` cask scenarios that fail locally pass in CI, which
      confirms the local failures were the missing `wails` CLI and nothing on
      this branch.
- [x] Comment on #127 and #124 with the ids, URLs and reserved order slots
- [ ] Nothing left. The PR waits for a person; do not merge it.

`bin/owl-verify`, built by a verification agent, could not be deleted: `rm` is
refused in this session. It is untracked and covered by `.gitignore`, so it
cannot reach a commit, but it is still sitting in the worktree.
