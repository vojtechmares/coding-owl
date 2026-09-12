# ADR-0010: Homebrew ships a `coding-owl` formula and a `coding-owl-desktop` cask

- **Status:** Accepted
- **Date:** 2026-09-09
- **Amended:** 2026-09-12, to say which direction of skew ADR-0004 guards,
  and which half of a release therefore arrives first. It said the reverse.

## Context

Two artifacts need distributing - the `owl` binary with its daemon, and the
desktop app - both from the author's own tap at
`github.com/vojtechmares/homebrew-tap`. Homebrew's model separates formulae
(CLI software, `brew services`) from casks (signed `.app` bundles), and naming
them is not a free choice.

## Decision

- Formula **`coding-owl`** - the `owl` binary plus the daemon service, started
  with `brew services start coding-owl`.
- Cask **`coding-owl-desktop`** - the Wails app.
- The cask declares `depends_on formula: "coding-owl"`.

MVP releases publish binaries as GitHub Release assets. A dedicated download
CDN at `releases.codingowl.dev` and a plugin registry at
`plugins.codingowl.dev` are out of scope for now.

## Consequences

- `brew services start coding-owl` reads correctly, because the formula ships a
  daemon rather than only a CLI.
- The cask dependency guarantees exactly one daemon per machine instead of the
  app bundling a second copy that drifts out of sync with the CLI.
- The cask follows the maintainer's existing Reviewdeck cask: ad-hoc signed,
  not notarised, quarantine cleared by a postflight step, rendered from scratch
  by the release workflow on every release. The formula follows the tap's
  existing GoReleaser-generated `statica` formula. Both are pushed to the tap by
  CI with a `contents:write` token. Versions are ZeroVer (`v0.x.y`) with no
  prereleases until SemVer is adopted.
- Two artifacts must be released in lockstep, and users will inevitably end up
  with one of them older than the other. ADR-0004's Buf breaking-change
  detection covers an older app against a newer daemon and not the reverse, so
  the daemon is the half to get there first: the release renders the formula
  before the cask, and the cask depends on the formula.
- The formula name does not match the binary name (`owl`). This is ordinary in
  Homebrew - `ripgrep` installs `rg`.
- Pointing formulae at GitHub Release assets rather than a custom domain keeps
  `brew bump-formula-pr` and third-party update bots working. Moving to a
  custom CDN later means hand-maintaining URLs and checksums, and should be a
  deliberate trade.

## Alternatives considered

**One name for both.** Rejected on hard mechanics: a formula and a cask sharing
a name inside one tap makes `brew install vojtechmares/tap/coding-owl`
ambiguous and forces `--formula`/`--cask` on users forever. This is exactly why
the Docker cask is named `docker-desktop`.

**`coding-owl-cli` plus `coding-owl-desktop`.** More explicit, but it misnames
the thing: the formula ships a daemon, and `brew services start
coding-owl-cli` reads as nonsense.
