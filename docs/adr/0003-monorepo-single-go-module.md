# ADR-0003: Monorepo with a single Go module

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

The project has four deliverables: the `owl` binary, the Wails desktop app, the
protobuf contract between them, and the codingowl.dev website. The first three
are coupled through one protocol - job definitions, idle-state events, job
status. A change to that protocol has to land in the daemon, the CLI, and the
GUI together or the pieces stop agreeing.

## Decision

One repository, one `go.mod` at the root. No `go.work`, no per-component
modules. `website/` lives in the same repo but is not Go.

## Consequences

- Protocol changes are atomic. There is no window in which the daemon and the
  GUI are built against different versions of the contract, and no dependency
  bumping ritual between components.
- One version number for the whole project; one CI pipeline builds everything.
- The Wails and GUI-side dependencies live in the same module as the CLI, so
  they appear in `go.sum` even for a plain `owl` build. Build tags and separate
  `main` packages keep the shipped binaries lean; the cost is confined to
  module metadata.
- Revisit only if the desktop app becomes a separately distributed product with
  its own release cadence. Nothing about the current plan points that way.

## Alternatives considered

**Multi-module with `go.work`.** Rejected. It buys independent versioning that
nobody needs here, and pays for it with `replace` directives and release
choreography for every protocol change.

**Separate repositories per component.** Rejected for the same reason, more so.
