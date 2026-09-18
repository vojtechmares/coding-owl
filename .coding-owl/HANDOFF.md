# Handoff - issue #120: `owl providers supported`

Plan written 2026-09-18 by the planning Run. Nothing has been implemented yet:
the only change on this branch so far is this file.

## What the issue asks for

<https://github.com/vojtechmares/coding-owl/issues/120> - `enhancement`,
`ready-for-agent`, open, no comments.

A first-time user has no way to learn that `anthropic` and `openrouter` are the
names `owl providers add <provider>` accepts. `owl providers list`
(`internal/cli/providers.go:101`) only reports what is already **configured**,
so it is empty for exactly the person who needs the answer. The names are in
the `providers` command's `Long` prose (`internal/cli/providers.go:24-33`),
which is discoverable by accident, not a discovery command.

Add `owl providers supported`: it prints the `chat.Providers` list
(`internal/chat/chat.go:44`) with a short note on each - Anthropic's models are
Owl's own so `--model` is not needed, OpenRouter's are whatever the user's
account has so `--model` is required.

The issue says a **docs page** for this is already specced by #123 (open,
`ready-for-agent`) and must not be duplicated here. #123 will reference this
command once it exists, so #123 depends on this, not the other way round.

## Blocker check - clear

Run on 2026-09-18, all four commands from the Job prompt:

- state `OPEN`, labels `enhancement`, `ready-for-agent` - in the queue.
- `dependencies/blocked_by`: empty.
- `sub_issues`: empty.
- cross-referenced open pull requests: none.
- prose in the body: names #123 only as work that will *consume* this command.
  #123 is open, and is not a blocker.

## Decisions made without the issue saying

1. **The command does not talk to the daemon.** `providers list`, `add` and
   `remove` all go through `withDaemon`. This one prints a compiled-in list, so
   it must work with no daemon running and nothing configured - that is the
   whole point of "without configuring one first". A first-time user may not
   have `owl daemon run` up yet.
2. **The prose note lives in `internal/cli/providers.go`, not in
   `internal/chat`.** The issue points at the `providers add` help for "the
   existing wording to reuse", so the wording is the CLI's. `internal/chat`
   stays the source of truth for *which* providers exist.
3. **The `--model` column is derived, not hardcoded.** Whether a provider needs
   `--model` is exactly `len(chat.DefaultModels[name]) == 0` - that is what
   `chat.checkModels` (`internal/chat/chat.go:339`) already decides. Deriving it
   means a provider added later cannot disagree with what `add` will do.
4. **A guard test covers the prose that is not derived.** Because the note is a
   CLI-local map, a provider added to `chat.Providers` later could print a blank
   note. A test in `internal/cli` asserts every entry of `chat.Providers` has
   one, so that failure is loud rather than silent.
5. **Stay on branch `owl/job-3`.** AGENTS.md's `feat/issue-<n>-<slug>`
   convention is for cutting your own branch; Owl put this Run on its Job
   branch and the Job prompt pushes `HEAD`. Renaming it would fight the
   harness. The branch is currently at `main` (`fdbb234`), so nothing is
   stacked.
6. **README command table and CHANGELOG `[Unreleased]` get one line each.** Both
   are the repo's habit for a new user-facing command (see the `owl models` and
   `owl account exec` entries). Neither is a docs page, so neither treads on
   #123.

## What is out of scope

- A docs page listing supported providers - #123.
- Anything in the desktop app: the issue asks for a CLI command.
- Touching `owl providers list`, `add` or `remove` behaviour.
- Adding a provider, or changing `chat.Providers`.

Anything else noticed along the way goes in a new issue, not in this diff.

## Design

`newProvidersSupportedCmd(env Env) *cobra.Command` in
`internal/cli/providers.go`, registered in `newProvidersCmd`'s `AddCommand`
alongside `add`, `list` and `remove`. `Use: "supported"`, `Args:
cobra.NoArgs`, no daemon call, exits 0.

Output, a `text/tabwriter` table in the same shape as `providers list`
(`PROVIDER` first) and `owl models` (an `ABOUT` column):

```
providers Owl drives:

PROVIDER    MODELS                          ABOUT
anthropic   Owl's own; no --model needed    The Anthropic API, spoken directly
openrouter  yours; name them with --model   Whatever your account has, in the OpenAI shape

configure one with: owl providers add <provider> --key-stdin < key.txt
```

Wording is lifted from the `providers` and `providers add` help
(`internal/cli/providers.go:31-33`, `:46-55`) and from the doc comments on the
`Anthropic` and `OpenRouter` constants (`internal/chat/chat.go:36-40`), so the
two places do not drift into saying different things.

The rows come from ranging over `chat.Providers`, in that order. The `MODELS`
cell is chosen by `len(chat.DefaultModels[name]) > 0`. The `ABOUT` cell comes
from a package-level `map[string]string` next to the command. `terminalSafe` is
not needed: every string is a compiled-in constant, not something a provider or
a user wrote.

## Behavior spec sheet - `tests/behavior/issue-120.md`

Scenarios to write, each observable from outside by driving the built `owl`
binary with the `tests/behavior` harness (`newLayout`, `runOwl`, `mustOwl`,
`runOwlStdin` in `issue2_test.go` / `issue3_test.go` / `issue14_test.go`).
Tests go in `tests/behavior/issue120_test.go` as `TestS<k>...`.

- **S1 - the names are there before anything is configured, and with no daemon.**
  Given the built binary, no daemon started and nothing configured. When
  `owl providers supported` runs. Then it exits 0, names `anthropic` and
  `openrouter`, and says nothing about a socket or an unreachable daemon.
  (Contrast: `owl providers list` in the same layout cannot answer.)
- **S2 - each provider says how its models are decided.** The `anthropic` line
  says its models are Owl's own and `--model` is not needed; the `openrouter`
  line says the models are the account's own and names `--model`.
- **S3 - it says how to configure one.** Output names `owl providers add` and
  `--key-stdin`, so the discovery command leads to the next step.
- **S4 - it is discoverable, and takes no arguments.** `owl providers --help`
  lists `supported` among its commands; `owl providers supported nonsense`
  exits non-zero.
- **S5 - what it lists is what `owl providers add` accepts.** For every name the
  command prints, `owl providers add <name> --key-stdin` against a running
  daemon is not refused with "is not a provider Owl drives" (it may still be
  refused for a missing `--model`, which is S2's point and fine). This is the
  scenario that ties the listing to `chat.Providers` rather than to a second
  hardcoded list.
- **S6 - what is configured does not change what is supported.** Given a daemon
  with `anthropic` configured and `openrouter` not, `owl providers supported`
  still lists both, and its output is byte-identical to S1's.

Commit the sheet and the six failing tests together, before any implementation.
The sheet is the contract: never weaken it to make a test pass.

Note for S5: `chat.AddProvider` reaches no network - it only writes the store
and the credential store - so a provider can be added with no fake server, as
`TestS2ChatAnOpenRouterProviderCarriesItsModels` already does
(`tests/behavior/issue18_test.go:268`). Layouts keep credentials in a file, so
no test touches a real keychain.

## Steps, in order

1. Write `tests/behavior/issue-120.md` with S1-S6 above and
   `tests/behavior/issue120_test.go` with one failing `TestS<k>` each. Confirm
   they fail for the right reason (`supported` is not a command). Commit both
   together: `test(cli): spec owl providers supported`.
2. Add `newProvidersSupportedCmd` and register it. Red to green, one scenario
   at a time. Commit: `feat(cli): add owl providers supported`.
3. Add the guard test in `internal/cli` (decision 4): every `chat.Providers`
   entry has a note. Commit with step 2 or just after.
4. README: add `owl providers supported` to the command-overview table row at
   `README.md:319`. CHANGELOG: one bullet under `[Unreleased]`. Commit:
   `docs(cli): record owl providers supported`.
5. Self-review: re-read the issue and `git diff main...HEAD`. Every requirement
   has a scenario, every scenario a passing test, nothing out of scope changed.
6. `make lint && make test` - `go vet ./...`, `gofmt -l .` empty, `buf lint`,
   `go test ./...`. Both must be green before the PR.
7. Run the three verification agents (`security-reviewer`,
   `correctness-reviewer`, `behavior-verifier`) in parallel per AGENTS.md, each
   writing to a fresh per-round directory outside the repo; wait with
   `.agents/skills/bdd/scripts/wait-verdicts.sh`. Fix findings and re-run until
   all three `PASS` on the commit they saw.
8. `git push -u origin HEAD`, then open the PR against `main` with the body
   below. Do **not** merge - a person accepts the work.
9. `gh pr checks --watch --fail-fast`; fix on the branch and push until green.

Keep this file current as each step lands: the next Run starts with no memory
of this one.

## PR body

```
Closes #120

## What changed

`owl providers supported` lists the providers Owl drives - the `chat.Providers`
list - with a note on how each one's models are decided, so the names
`owl providers add` accepts are discoverable before anything is configured. It
needs no daemon and no configured provider, which is the point: `owl providers
list` can only report what is already there.

## Decisions made on my own

- The command prints from the compiled-in list rather than asking the daemon,
  so it answers with nothing configured and the daemon not running.
- Whether a provider needs `--model` is derived from `chat.DefaultModels`, the
  same thing `chat.checkModels` decides, rather than a second hardcoded list.
- The prose note lives in the CLI, reusing the `providers add` help's wording;
  a test asserts every `chat.Providers` entry has one, so a provider added
  later cannot print a blank note.
- README and CHANGELOG get one line each, as every other new command does. The
  docs page stays with #123, which this does not duplicate.
```

## Progress

- [x] Blocker check - clear.
- [x] Plan written (this file).
- [ ] Step 1 - spec sheet and red tests.
- [ ] Step 2/3 - command and guard test.
- [ ] Step 4 - README and CHANGELOG.
- [ ] Steps 5-7 - self-review, `make lint && make test`, verification agents.
- [ ] Steps 8-9 - push, PR, CI green.
