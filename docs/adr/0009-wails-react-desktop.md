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

Visual direction: liquid glass, night-sky and navy blues.

Day-one surface: queue management, a live stream of the running job's output,
pause and resume, and job history with status and branch name. Diff review is
explicitly out - that is what ADR-0007's branches are for.

## Consequences

- The Go side of the app reuses `internal/client`, so there is no second API
  surface and no risk of the GUI growing behaviour the CLI cannot reach.
- Node and npm join the build for the frontend.
- Because the frontend talks to Go through Wails bindings rather than to the
  daemon directly, no TypeScript client is generated from the protobufs. This
  is worth stating because it removes what looks like an obvious argument for
  gRPC in ADR-0004 - the real argument there is streaming.
- Shipping a macOS cask means codesigning and notarization, which needs an
  Apple Developer Program membership. That is administrative lead time, not
  engineering time, and it is the likeliest thing to block a first release.

## Alternatives considered

**Electron.** Heavier, and no reuse of the Go domain code.

**Tauri.** Good product, but introduces Rust as a second systems language in a
solo project.

**An HTTP dashboard served by the daemon** with the frontend embedded via Go
`embed`, plus a thin menubar wrapper later. Retained as the fallback if Wails
proves heavy in practice.
