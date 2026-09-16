# ADR-0009: Desktop app is Wails with a React frontend

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Coding Owl ships a desktop dashboard alongside the CLI, in the same release.
It wants to feel native on macOS, live in the monorepo, and reuse the Go domain
code that already exists for the daemon.

## Decision

**Wails** - Go backend, React/TypeScript frontend - as a separate build target
from the `owl` binary.

The GUI is a **pure view**. Every action it can perform exists as a daemon RPC
that the CLI can also call; `internal/daemon` remains the single source of
truth. The app is a client of the daemon like any other.

Visual direction: the same design as codingowl.dev. A neutral-50 ground, white
where a surface is needed, hairline rules to divide, Geist for text and Geist
Mono for anything mechanical, and one sky blue spent sparingly. Colour says one
of four things and nothing else - blue is in motion, near-black is waiting for
you, rose is something that went wrong, grey is at rest - so a coloured thing
on screen always means something.

> **Amended 2026-09-17 (#36).** This originally read "liquid glass, night-sky
> and navy blues", and the app shipped that way in #9. The design review found
> the glass fought the content: a dashboard is mostly small text in dense
> tables, and blur, translucency and saturated navy all work against reading
> it. The site had already moved to the quieter design; the app follows it, so
> there is one look to maintain rather than two. The theme is two files -
> `frontend/src/theme/tokens.css` for the values and `frontend/src/style.css`
> for what uses them - so the next change is as small as this one was.

> **Superseded by ADR-0029.** This ADR originally fixed a day-one feature set
> for the app. There is no longer one: the core loop ships first in the daemon,
> and features land across daemon, API and desktop app together thereafter. Diff
> review remains explicitly out - that is what ADR-0007's branches are for.

## Consequences

- The Go side of the app reuses `internal/client`, so there is no second API
  surface and no risk of the GUI growing behaviour the CLI cannot reach.
- Node and pnpm join the build for the frontend. pnpm is the frontend's one
  package manager: its version is pinned in `package.json`, its lockfile is
  committed, and every build - local, CI, release - installs from that
  lockfile alone. (Amended 2026-09-14: npm until then.)
- Because the frontend talks to Go through Wails bindings rather than to the
  daemon directly, no TypeScript client is generated from the protobufs. This
  is worth stating because it removes what looks like an obvious argument for
  gRPC in ADR-0004 - the real argument there is streaming.
- Shipping a macOS cask eventually means codesigning and notarization, which
  needs an Apple Developer Program membership. **For now the app is ad-hoc
  signed and not notarised**, following the maintainer's Reviewdeck cask: the
  cask clears the quarantine flag itself in a postflight step. Notarisation is
  deferred, and with it the only administrative dependency a first release had.

## Alternatives considered

**Electron.** Heavier, and no reuse of the Go domain code.

**Tauri.** Good product, but introduces Rust as a second systems language in a
solo project.

**An HTTP dashboard served by the daemon** with the frontend embedded via Go
`embed`, plus a thin menubar wrapper later. Retained as the fallback if Wails
proves heavy in practice.
