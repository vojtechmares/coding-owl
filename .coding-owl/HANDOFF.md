# Handoff - issue #123

Docs: the Desktop app chat's model provider integration - Anthropic and
OpenRouter. `vojtechmares/coding-owl#123`, labels `documentation`,
`ready-for-agent`.

Branch: `owl/job-6`, cut from `main` and, at the time of planning, identical to
`origin/main` (`fdbb234`). Nothing is implemented yet: this commit is the plan.

## Blocker check - clear

Run on 2026-09-18, all four commands from the Job prompt:

- state `OPEN`, labels `documentation` and `ready-for-agent` - still in the
  queue, and none of `needs-info`, `needs-triage`, `ready-for-human`, `wontfix`.
- no `blocked_by` dependencies and no sub-issues.
- one cross-referenced open pull request, **#139** (`owl/job-3`,
  `feat(cli): add owl providers supported`). It closes **#120**, not this issue,
  and its own body says "The docs page stays with #123". Not a blocker.
- the issue has **no comments at all**, so there is no blocker written in prose.
- the body names #122, #127 and #120. All three are open, none is phrased as
  work that must land first, and both of the others point the other way: #120
  says "#123 ... specs exactly this content ... Don't duplicate that work here;
  #123 can reference this new `providers supported` command as its live source
  **once it exists**", and #122 lists #123 as one of its own sub-pages,
  "already ready-for-agent". So #123 is meant to go first and be linked to.

## What the issue asks for

One docs page covering five things, all of which already exist as prose in the
CLI's help text and are to be lifted from there rather than rewritten:

1. **What "providers" means here** - the model providers the Desktop app's
   *chat* speaks to, explicitly *not* an Agent's Account or Driver.
2. **How keys are configured** - `owl providers add <provider> --key-stdin`,
   read from standard input so the app never holds a key; the key goes to the
   OS keychain through the credential store (ADR-0019, ADR-0022).
3. **Anthropic** - Owl knows the model list itself, so no `--model` is needed.
4. **OpenRouter** - the models are the user's own account's business, so
   `--model` is required, and `--base-url` can override where it is reached.
5. **Listing and removing** - `owl providers list`, `owl providers remove`.

The issue is emphatic on one point: describe **what is actually implemented**,
not a wishlist. Two providers, `anthropic` and `openrouter`. OpenAI is not a
provider - OpenRouter merely speaks the OpenAI shape.

## Source of truth, with the lines to read before writing a word

| Fact | Where it is |
| --- | --- |
| The closed set of providers | `internal/chat/chat.go:33-44` - `Anthropic`, `OpenRouter`, `Providers` |
| Anthropic's models | `internal/chat/chat.go:46-51` - `claude-opus-5`, `claude-sonnet-5`, `claude-haiku-4-5-20251001` |
| Why OpenRouter needs `--model` | `internal/chat/chat.go:336-362` - `checkModels`, and the error it returns |
| What `--base-url` accepts | `internal/chat/chat.go:306-334` - `checkBaseURL` (http/https, a host, no credentials, no query or fragment) |
| The default base URLs | `internal/chat/anthropic.go:12`, `internal/chat/openai.go:12` |
| The wording to lift | `internal/cli/providers.go:20-37` (`providers` Long), `39-56` (`add` Long), `73-76` (the flags) |
| Why the key is stdin-only | `internal/cli/providers.go:83-99` - `providerKey`, and ADR-0019 |
| Where the key goes | `internal/chat/chat.go:148-212` - `AddProvider`; `credentialRef` is `chat/<provider>` |
| The chat's framing | `internal/chat/chat.go:1-4` - "never acts, only reads what Owl knows"; ADR-0022 |
| Keychain vs file store | `docs/adr/0019-accounts.md:44-62` - `credentialStore: keychain` or `file` |

## Decisions taken, with reasons

**D1 - one page, `docs/guide/chat-providers.md`, id `chat-providers`, published
at `/docs/chat-providers`, titled "Chat model providers".**

`docs/guide/` is the directory #127 proposes for website-facing guide pages and
the one #121 is already using on `owl/job-4` (see D7). `docs/desktop/` is
screenshots, `docs/agents/` is agent-facing; neither fits.

The id is `chat-providers` rather than the shorter `providers` on purpose. The
whole point of the page is that these are not the Accounts an Agent runs on nor
the Drivers that operate a coding tool, and a URL of `/docs/providers` invites
exactly that confusion.

**D2 - `order: 61`, leaving 60 for #122's capabilities overview.** #121 numbers
the docs collection in tens and records the reserved slots as a comment
(10/11 for #127's install pages, 50/51 for #124's config pages, 90 for the
README guide). The Desktop pages have no slot yet; take the range after the
config pages, and leave 60 to #122 so its overview sorts above the sub-page it
links to. Extend the reserved comment rather than leaving the next issue to
guess.

`guide`'s own `order` is left at whatever `main` has it at. Moving it is #121's
change, not this one, and duplicating it only makes the merge worse.

**D3 - the page states no fact that a test cannot check against the Go source.**
The issue's own warning is drift ("update the docs alongside this file if a
provider is ever added"). So every list on the page - the providers, Anthropic's
models, the subcommands - is asserted against `internal/chat` and
`internal/cli` by the behavior tests, in both directions where it is safe to
(see S2 and S6). A page whose claims are checked by `make test` is the only kind
that stays true.

**D4 - the explanation is the CLI help's own sentences, verbatim.** The issue
says to lift the wording, and S9 turns that into a drift guard: the test
whitespace-normalizes both and asserts the page carries the help's sentences, so
the two cannot come to disagree. Line wrapping differs between the two (the help
wraps to the terminal, the page to 80 columns), hence the normalization.

**D5 - README gets one link, and nothing else.** README's `## Desktop app`
section (~line 326-338) is the only Desktop prose there is, and the page is
unreachable from the repository without it. One sentence pointing at the new
page, in the paragraph that already describes the chat. The `## Command
overview` table's `owl providers add|list|remove` row stays as it is - #121 is
already changing that table, and a second hand in it buys a conflict for
nothing.

**D6 - no CHANGELOG entry.** The changelog records what changed in the product;
a docs page changes no binary. #121's docs branch (`owl/job-4`) touches no
CHANGELOG either, and that is the precedent to follow. #139 *did* add one, but
it shipped a new command.

**D7 - coordinate with PR #142, do not build on it.** #142 (`owl/job-4`, issue
#121) is open and already adds `docs/guide/` with three pages, renumbers the
docs collection, and - importantly - teaches `website/src/loaders/repo.ts` to
resolve a relative link **relative to the file it is written in** rather than to
the repository root (its `repoPath` helper). A page living in `docs/guide/`
needs that fix, or every relative link it carries is rewritten to a path that is
not there.

AGENTS.md forbids branching off another feature branch, so: branch from `main`,
and **check `main` for `repoPath` before touching `repo.ts`**. If #142 has
merged by then, the fix is already there and only the `PUBLISHED` entry is
needed. If it has not, port the same fix, written as close to #142's version as
it can be, so that whichever merges second resolves its conflict by keeping one
copy. Say in the PR body that `content.config.ts` and `repo.ts` will conflict
with #142 and how to resolve it.

**D8 - `owl providers supported` (#139) is not documented.** It does not exist
on `main`. If it has merged by the time this is implemented, add one line for
it; the test in S6 is deliberately one-directional so that it landing does not
turn this branch red (see the sheet's note on S6).

**D9 - no screenshot.** `docs/desktop/*.jpg` has no picture of provider
configuration, because configuration happens in the CLI. #122 is where the
screenshots belong.

## The behavior spec sheet

`tests/behavior/issue-123.md`, with `TestS<k>` in
`tests/behavior/issue123_test.go`. Written and committed **red** before the
page exists. Each scenario is observable from outside: a file on disk, its text
measured against the Go source of truth, or what the `owl` binary really does.

- **S1 - the page is in the repository and published.** `docs/guide/chat-providers.md`
  exists, is registered in `website/src/content.config.ts`'s `repoFiles([...])`
  with an id, title, description and order, and `website/src/loaders/repo.ts`'s
  `PUBLISHED` maps its path to `/docs/chat-providers`, so a link to it from
  another repository file resolves to the page rather than to GitHub.
- **S2 - it documents every provider Owl drives, and no other.** Every name in
  `chat.Providers` appears on the page; no name outside it is presented as a
  provider. This is the scenario that catches "OpenAI" being written up as one,
  and it is checked in both directions because the set is closed
  (`chat.go:33-34`).
- **S3 - Anthropic's models are the ones Owl knows.** The page lists exactly
  `chat.DefaultModels[chat.Anthropic]`, and says no `--model` is needed. Then
  the claim is checked against the binary: `owl providers add anthropic
  --key-stdin`, with no `--model`, succeeds and `owl providers list` reports
  those models.
- **S4 - OpenRouter needs `--model`.** The page says so, and `owl providers add
  openrouter --key-stdin` without `--model` is refused, for the reason the page
  gives.
- **S5 - a key is read from standard input and nowhere else.** The page shows
  `owl providers add <provider> --key-stdin < key.txt` and says the key goes to
  the credential store with only a reference in the database (ADR-0019,
  ADR-0022). `owl providers add anthropic` without `--key-stdin` is refused and
  says where a key is read from, and the command has no flag that takes a key as
  an argument - so the page is not describing a second way in that exists.
- **S6 - every `owl providers` subcommand the page names is real, and `add`,
  `list` and `remove` are all named.** One direction only, deliberately: #139
  adds `owl providers supported`, and a both-directions check would turn this
  branch red the moment that merges, for a page that was correct when it was
  written. The dangerous direction - documenting a command that is not there -
  is the one that is guarded.
- **S7 - it says what a provider is not.** The page names the distinction from
  an Agent's Account and from a Driver, and links to `CONTEXT.md`, where both
  terms are defined. (Whether the sentence *reads* well is the correctness
  reviewer's call; the test checks the link and the terms are there at all.)
- **S8 - nothing on the page dangles.** Every relative Markdown link target,
  resolved against `docs/guide/`, is a file that exists in the repository - so
  the page is right on GitHub as well as on the website.
- **S9 - the page and the CLI help say the same thing.** With whitespace
  normalized, the page carries the sentences from `newProvidersCmd`'s and
  `newProvidersAddCmd`'s `Long` text, so the explanation cannot fork into two
  that disagree.

The harness is the one `tests/behavior/issue18_test.go` already uses for
providers: `newLayout`, `globalConfig(t, l, fileStore)`, `daemonUp`,
`addProvider`, `mustOwl`/`runOwl`. `fileStore` matters - no test touches a real
keychain.

## Steps, in order

1. Re-read the issue (`gh issue view 123 --comments`) in case it moved, and
   check whether #139 and #142 have merged - D7 and D8 both turn on that.
2. Write `tests/behavior/issue-123.md` and the red
   `tests/behavior/issue123_test.go`. Commit them together: `test(docs): spec
   the chat provider page` (red, as the sheet is the contract).
3. Write `docs/guide/chat-providers.md`, lifting from `internal/cli/providers.go`
   and checking every fact against the table above.
4. Register it: `website/src/content.config.ts` entry, `PUBLISHED` entry in
   `website/src/loaders/repo.ts`, and the relative-link fix if `main` still
   lacks it.
5. One sentence in README's `## Desktop app` section linking to the page.
6. Green: `make lint && make test`.
7. The website is not in `make`: run `pnpm install --frozen-lockfile && pnpm
   check && pnpm build` in `website/`, because the website workflow builds on
   any PR touching `docs/**` or `website/**` and a bad `content.config.ts` entry
   fails it. If pnpm or Node 24 is not available on this machine, say so plainly
   in the PR rather than claiming it was checked.
8. Self-review against `git diff main...HEAD`, then the three agents in
   `.agents/agents/` in parallel, fixing until all three `PASS`
   (`.agents/skills/bdd/scripts/wait-verdicts.sh` waits for them). A `PASS`
   counts only for the commit it saw.
9. Push, open the PR against `main` with `Closes #123`, and wait for CI with
   `gh pr checks --watch --fail-fast`. Do not merge.

## Ruled out

- **Waiting for #122, #127 or #120.** None blocks this; #120 and #122 both say
  so outright. See the blocker check.
- **Branching from `owl/job-4` to get its `docs/guide/` scaffolding.** AGENTS.md
  forbids stacking branches. The conflict is small and the resolution is
  obvious.
- **Documenting `owl providers supported`.** Not on `main` - D8.
- **A page under `docs/desktop/`.** That directory is screenshots, and #127 asks
  for website-facing guide content under `docs/guide/`.
- **Restructuring the docs collection's ordering.** #121's job, in flight.

## State right now

Planned only. Working tree clean apart from this file; no page, no tests, no
website change, no PR.
