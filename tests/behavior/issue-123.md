# Issue #123: Docs - the Desktop app chat's model provider integration (Anthropic, OpenRouter)

The deliverable is a page of prose, so most scenarios are about a file on disk
and what it says. What makes them behavior rather than a spell-check is that
every claim the page makes is measured against the thing it describes: the Go
source of truth in `internal/chat` and `internal/cli`, the `owl` binary itself,
and the website's own registry of published pages. A page nobody checks drifts,
and the issue asks specifically that it not.

"The page" means `docs/guide/chat-providers.md`. "The CLI help" means the `Long`
text of `newProvidersCmd` and `newProvidersAddCmd` in `internal/cli/providers.go`
- the wording the issue says to lift rather than rewrite. "Published" means the
website resolves a repository-relative link to it as a page under `/docs/`
rather than as a link out to GitHub, which is what `PUBLISHED` in
`website/src/loaders/repo.ts` decides.

The scenarios that drive the binary use the harness `tests/behavior/issue-18.md`
describes: the XDG layout, a daemon with `credentialStore: file` so no test
touches a real keychain, and a provider pointed at nothing in particular,
because adding one sends no request.

## Scenarios

### S1 - the page is in the repository and published on the website
Given the repository
When the page, `website/src/content.config.ts` and `website/src/loaders/repo.ts` are read
Then `docs/guide/chat-providers.md` exists and opens with an H1
And the docs collection carries an entry for it with an id, a title, a description and an order
And `PUBLISHED` maps `docs/guide/chat-providers.md` to the page that entry's id publishes
And the link rewriter resolves a target relative to the file it is written in, which is what a page under `docs/guide/` needs and what GitHub does too

The last one is checked as source on disk rather than by building the site:
`make test` is Go, the website is built by its own workflow, and this is how
`tests/behavior/issue-9.md` and `tests/behavior/issue-18.md` already check what
the frontend is made of.

### S2 - it documents every provider Owl drives, and nothing else as one
Given `chat.Providers`, which is the closed set of providers
When the page is read
Then every name in `chat.Providers` appears on it
And no other name is presented as a provider - `openai` in particular, which is a wire shape OpenRouter speaks and not a provider

### S3 - Anthropic's models are the ones Owl knows, and need no --model
Given `chat.DefaultModels[chat.Anthropic]`
When the page is read
Then it lists exactly those models
And says no `--model` is needed for Anthropic
And `owl providers add anthropic --key-stdin` with no `--model` exits 0, and `owl providers list` reports those same models

### S4 - OpenRouter needs --model, for the reason the page gives
Given a running daemon
When the page is read and `owl providers add openrouter --key-stdin` runs with no `--model`
Then the page says `--model` is required for `openrouter` because what it offers is the user's own account's business
And the command exits non-zero, naming the flag

### S5 - a key is read from standard input and nowhere else
Given a running daemon
When the page is read and `owl providers add anthropic` runs without `--key-stdin`
Then the page shows `owl providers add <provider> --key-stdin` reading the key from standard input
And says the key goes to the credential store - the OS keychain - with only a reference to it in the database, citing ADR-0019 and ADR-0022
And the command exits non-zero, saying a key is read from standard input
And `owl providers add --help` offers no flag that takes a key as an argument, so the page is not describing a second way in that does not exist

### S6 - every `owl providers` subcommand the page names is real, and add, list and remove are all named
Given `owl providers --help`
When the page is read
Then it names `owl providers add`, `owl providers list` and `owl providers remove`
And every `owl providers <subcommand>` it names is one the binary has

This direction only, deliberately. Issue #120's `owl providers supported` is
not on `main`; checking the other way round would turn this page red the moment
that lands, for a page that was right when it was written. Documenting a command
that is not there is the dangerous direction, and that is the one guarded.

### S7 - it says what a provider is not
Given `CONTEXT.md`, where Account and Driver are defined
When the page is read
Then it distinguishes a chat model provider from an Agent's Account and from a Driver, saying what each of those is and where it is configured instead
And the commands it names for those are ones the binary really has
And links to `CONTEXT.md`
And repeats the chat's own framing: it reads what Owl knows and never acts (ADR-0022)

### S8 - nothing on the page dangles
Given the page
When every relative Markdown link on it is resolved against its own directory
Then each target is a file that exists in the repository, so the page is right on GitHub as well as on the website

### S9 - the page and the CLI help say the same thing
Given the `Long` text of `newProvidersCmd` and `newProvidersAddCmd`
When both and the page are read with whitespace normalized
Then the page carries the sentences of that help text, so the explanation cannot fork into two that disagree

### S10 - the repository points at the page
Given `README.md`
When its Desktop app section is read
Then it links to the page, so the page is reachable from where the chat is described
