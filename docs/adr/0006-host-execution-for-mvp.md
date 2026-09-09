# ADR-0006: MVP agents run as host processes, not containers

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

The earlier design assumed agents would always execute inside containers, with
Docker first and Podman, Rancher Desktop and OrbStack behind a runtime
interface.

Containerising an agent is not one decision, it is several: image build and
distribution, mounting the working copy, injecting Claude Code credentials,
establishing a git identity inside the container, and deciding a network
policy. Each is real work before the first job can run.

There is also a wrinkle specific to this product. On macOS, Docker runs a Linux
VM. Coding Owl's entire premise is using a machine that is otherwise idle, so
keeping a VM hot to do it works directly against the thing being sold.

## Decision

For the MVP, the agent runs as a **child process on the host**, behind an
`Executor` interface. Containerised execution is a future implementation of
that same interface, not a rewrite.

The blast radius is bounded structurally instead: every job runs in its own git
worktree with the agent's working directory set there (ADR-0007).

## Consequences

- No Docker dependency, no image management, no container plumbing. The MVP can
  reach a working end-to-end loop far sooner.
- The agent inherits the user's already-authenticated Claude Code environment.
  Credential injection, the fiddliest part of the container path, simply does
  not arise.
- **There is no sandbox.** An unattended agent runs with the user's full
  filesystem and network access. The worktree bounds accidents; it does not
  bound intent, and it does not stop a process from writing outside its
  working directory. This is understood and accepted.
- Because unattended operation pushes toward pre-granting tool permissions, the
  combination deserves care: prefer Claude Code's own allowlist settings over
  blanket permission skipping.
- Revisit trigger: the first time a job needs isolation the host cannot give,
  or the first time Coding Owl runs a prompt the user did not write.

## Alternatives considered

**Docker-first with a runtime plugin interface.** Deferred rather than
rejected - it remains the intended destination, and ADR-0005's `Executor`
interface is the seam it will arrive through. It simply is not the cheapest
path to a working MVP.
