# ADR-0001: Single `owl` binary for everything headless

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Coding Owl has three headless components: the daemon that detects idle time and
runs Agents, the user-facing CLI that controls it, and (later) a Runner that
executes work on a remote VM and calls home. All three are Go, and they share most of
their dependency graph - the protocol types, the client, the config loader.

The obvious prior art splits the daemon out into its own binary: `docker` and
`dockerd`, `tailscale` and `tailscaled`. That split exists to solve privilege
separation and to let an ops team deploy the server without the client.

## Decision

One cobra entrypoint at `cmd/owl`, producing one binary:

- `owl daemon run` - runs the daemon in the foreground
- `owl daemon install` - writes and loads a launchd plist or systemd unit
- `owl runner run --server=...` - future remote-Runner mode
- everything else (`owl status`, `owl add`, `owl logs`, ...) acts as a client
  of the daemon

## Consequences

- One Homebrew formula, one release asset per platform, one file to copy to a
  VM. Distribution stays trivial for as long as this project is solo.
- Size overhead is negligible, because the daemon and the CLI already share
  nearly everything they link.
- The command tree has to stay disciplined: daemon-only flags must not leak
  into user-facing commands, and `owl --help` must not read like a server
  manual.
- If a stripped-down Runner artifact is ever genuinely needed, adding
  `cmd/owl-runner` as a thin `main` importing `internal/runner` costs nothing and
  changes no other code. Do not pre-split for it.

## Alternatives considered

**Separate `owld` binary.** Rejected. The problems the two-binary pattern
solves - running the daemon as a different user, shipping server and client to
different machines, letting distro packagers split them - are problems this
project does not have. A single-user laptop tool pays the packaging cost for
none of the benefit.
