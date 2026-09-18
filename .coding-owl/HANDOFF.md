# Handoff - issue #123

Docs: the Desktop app chat's model provider integration - Anthropic and
OpenRouter. `vojtechmares/coding-owl#123`, labels `documentation`,
`ready-for-agent`.

Branch: `owl/job-6`, cut from `main` (`fdbb234`, still `origin/main`).

## State right now

Implemented. The page, its website registration, the README link and ten
behavior tests are committed and green. What is left is in "Steps left" below:
the website build check, the three verification agents, the push and the PR.

Commits on the branch, oldest first:

1. `3154092` `docs(handoff): plan the chat model provider docs page for #123`
2. `test(docs): spec the chat model provider page for #123` - the sheet and the
   ten tests, committed red.
3. `docs(guide): the chat's model providers, Anthropic and OpenRouter` - the
   page, the website registration, the README link, and the sharpening of the
   test's sentence splitter that writing the page showed was needed.

## Blocker check - clear

Re-run on 2026-09-18, all four commands from the Job prompt:

- state `OPEN`, labels `documentation` and `ready-for-agent`; none of
  `needs-info`, `needs-triage`, `ready-for-human`, `wontfix`.
- no `blocked_by` dependencies, no sub-issues, no comments at all.
- one cross-referenced open pull request, **#139** (`owl/job-3`,
  `feat(cli): add owl providers supported`). It closes **#120**, not this issue.
- the body names #122, #127 and #120; none is phrased as work that must land
  first, and #120 and #122 both say #123 goes first.

**#139 and #142 are both still open** as of this run, which is what D7 and D8
below turn on. `docs/guide/` did not exist on `main`, so #142 had not merged.

## What was built

### `docs/guide/chat-providers.md`

Covers the five things the issue lists, in this order: what `owl providers` is
for (the help's own words), a provider against an Account and a Driver, how a
key is configured, Anthropic, OpenRouter, listing and removing, and a See also.

Facts and where each came from:

| Fact | Source |
| --- | --- |
| The closed set of providers | `internal/chat/chat.go:33-44` |
| Anthropic's models | `internal/chat/chat.go:49-51` |
| Why OpenRouter needs `--model` | `internal/chat/chat.go:336-362` (`checkModels`) |
| What `--base-url` accepts | `internal/chat/chat.go:306-334` (`checkBaseURL`) |
| The default base URLs | `internal/chat/anthropic.go:12`, `internal/chat/openai.go:12` |
| The wording lifted | `internal/cli/providers.go:24-33`, `46-55` |
| Why the key is stdin-only | `internal/cli/providers.go:83-99`, ADR-0019 |
| Keychain vs file store | `docs/adr/0019-accounts.md:44-62` |
| The chat's framing | `internal/chat/chat.go:1-4`, ADR-0022 |

### `website/src/content.config.ts`

One docs entry: id `chat-providers`, title "Chat model providers", `order: 61`.
The doc comment now says the numbering leaves 60 for #122.

### `website/src/loaders/repo.ts`

`PUBLISHED` gains `'docs/guide/chat-providers.md': '/docs/chat-providers'`, and
`rewriteRepoLinks` now takes the file the links are written in and resolves
each target against it (`repoPath`). Without that, a page under `docs/guide/`
writing `../../CONTEXT.md` had it looked up as `../../CONTEXT.md` against the
repository root and rewritten to a GitHub URL that is not there.

### `README.md`

One sentence in the `## Desktop app` section's chat paragraph, linking to the
page. The `## Command overview` table is untouched on purpose (D5).

## Decisions taken, with reasons

**D1 - one page, `docs/guide/chat-providers.md`, id `chat-providers`.**
`docs/guide/` is what #127 proposes and what #121/#142 already use;
`docs/desktop/` is screenshots and `docs/agents/` is agent-facing. The id is not
the shorter `providers` because the page's whole point is that these are not
Accounts or Drivers, and `/docs/providers` invites that confusion.

**D2 - `order: 61`, leaving 60 for #122.** `guide` keeps `order: 1` from `main`;
renumbering the collection is #142's change, and doing it here only makes the
merge worse.

**D3 - the page states no fact a test cannot check against the source.** Every
list on it - the providers, Anthropic's models, the subcommands - is asserted
against `internal/chat` and the built binary. The issue warns of drift by name.

**D4 - the explanation is the CLI help's own sentences, verbatim.** S9 asserts
it with whitespace normalized, so the two cannot come to disagree. This is why
the ADR citation in the lifted paragraph is the plain `(ADR-0019, ADR-0022)`
rather than links: links would break the verbatim match. The ADRs are linked
elsewhere on the page instead, and S8 checks those resolve.

**D5 - README gets one link and nothing else.** The command overview table is
already being changed by #142; a second hand in it buys a conflict for nothing.

**D6 - no CHANGELOG entry.** A docs page changes no binary, and #142 (docs)
adds none either. #139 did, but it shipped a command.

**D7 - ported #142's `repoPath` fix rather than branching off it.** AGENTS.md
forbids stacking branches. The port is written as close to #142's version as it
can be - same helper name, same shape, same comment - so whichever merges
second resolves by keeping one copy.

**D8 - `owl providers supported` (#139) is not documented.** It is not on
`main`. S6 is deliberately one-directional (every command the page names must
exist, not the reverse) so that #139 landing does not turn this branch red.

**D9 - no screenshot.** Provider configuration happens in the CLI;
`docs/desktop/*.jpg` has no picture of it, and screenshots are #122's job.

**D10 - the test's sentence splitter was sharpened after the red commit.**
The first version split flattened text on `. ` only, so a claim could be read
across a code block. It now breaks at blank lines, headings, list items, fences
and table rows too. This makes the tests stricter, never weaker; the sheet is
unchanged.

## The behavior spec sheet

`tests/behavior/issue-123.md`, ten scenarios, `TestS123*` in
`tests/behavior/issue123_test.go`. S1 page exists and is published; S2 every
provider and no other; S3 Anthropic's models and no `--model`; S4 OpenRouter
needs `--model`; S5 the key is stdin-only; S6 every subcommand named is real;
S7 what a provider is not; S8 nothing dangles; S9 the page and the help agree;
S10 the README points at it.

All ten were red before the page existed. Three were then mutation-checked by
hand and each caught its drift: S9 against a reworded lifted sentence, S2
against "An OpenAI key is configured the same way", S3 against an invented
`claude-sonnet-3-7`. The mutations were reverted.

## Verification so far

- `make lint` - green.
- `make test` - everything green except three scenarios of issue #22
  (`TestS1CaskTheDesktopBuildProducesASignedAppInAZip`, `TestS2Cask...`,
  `TestS3Cask...`), which fail with "the wails CLI is not installed". That is
  this machine, not the branch: CI installs wails
  (`.github/workflows/ci.yml:90`), and nothing in the diff touches
  `tests/behavior/issue22_test.go` or the desktop build. The ten `TestS123*`
  scenarios are green. Run twice, before and after the review fixes, with the
  same three failures and no others.
- `website/`: `pnpm install --frozen-lockfile`, `pnpm check` and `pnpm build`
  all green on Node v26.8.2 / pnpm 11.5.2. The built page is
  `dist/docs/chat-providers/index.html`; its ADR links resolved to
  `/docs/decisions/0019-accounts` and `/docs/decisions/0022-desktop-chat`, and
  the guide page's link to it resolved to `/docs/chat-providers`, which is the
  `repoPath` fix working end to end. Both were run again after the last page
  edit, still green.

### The three agents

Each was run twice: once on the first implementation, once on HEAD after its
findings were acted on, because a `PASS` counts only for the commit it saw.

- `security-reviewer` - **PASS** both rounds. Three optional notes; two taken
  (the `key.txt` example now carries a warning and a `pass show ... |` 
  alternative; `--base-url` now says when http is not enough). The third -
  clamping a `..` that climbs above the repository root in `repoPath` - was
  **not** taken, and on re-review the agent agreed: the result is only a
  `PUBLISHED` key, an ADR regex match or a suffix on the GitHub blob URL, never
  a filesystem path, so there is no traversal; and diverging from #142's copy
  of the helper would make that merge worse.
- `correctness-reviewer` - **FAIL**, then **PASS**. Its finding was real and
  worth recording: `claims(body, "account")` could not fail, because S9 already
  forces the page to carry the help's sentence about "whatever your account
  has". So the one requirement with no source of truth in `internal/chat` -
  that a provider is not an Account and not a Driver - was the one requirement
  nothing checked, which is exactly where the branch had shipped a false
  statement about where a Driver is chosen. S7 now asserts the distinction and
  checks `owl account add` and `owl account add --driver` against the binary.
- `behavior-verifier` - **PASS** on the first round, with notes that led to
  `claims` matching whole words rather than substrings and the stale-model scan
  reading the whole page again.

No `$VERDICTS` directory: creating a directory outside the worktree is blocked
in this session, so the agents returned their verdicts as their final message
instead of writing files. For the same reason the behavior verifier could not
build into a scratch directory, so the daemon-driven clauses of S3, S4 and S5
were confirmed by their tests rather than by hand; it drove the CLI surface and
the built site by hand.

## Steps left

1. The `behavior-verifier` re-run on HEAD.
2. Push, open the PR against `main` with `Closes #123`, and say in the body that
   `content.config.ts` and `repo.ts` will conflict with #142 and how to resolve
   it (keep one copy of `repoPath`; keep both `PUBLISHED` entries; keep both
   docs entries and let #142's renumbering win).
3. Wait for CI with `gh pr checks --watch --fail-fast`. Do not merge.

## Ruled out

- Waiting for #122, #127 or #120: none blocks this, and #120 and #122 say so.
- Branching from `owl/job-4`: AGENTS.md forbids stacking branches.
- Documenting `owl providers supported`: not on `main` (D8).
- A page under `docs/desktop/`: that directory is screenshots.
- Restructuring the docs collection's ordering: #142's job, in flight.
