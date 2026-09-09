# ADR-0005: Plugins are Go interfaces, not external processes

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

"Everything is a plugin" is a stated design goal - the point is that parts of
Coding Owl should be replaceable without surgery on the core. That goal says
nothing about *how* plugins load, and the options differ enormously in cost: a
Go interface is free, while an out-of-process plugin host means an ABI, version
negotiation, a serialisation boundary, process supervision, and a security
model.

A third-party plugin ecosystem is a possible future, not a current
requirement. Nobody is writing plugins for this project yet.

## Decision

Extension points are **Go interfaces**, with each implementation in its own
package, compiled into the binary. There is no plugin host, no dynamic loading,
no WASM runtime.

The first interfaces are:

- `Source` - where queued work comes from (see ADR-0008)
- `Driver` - which coding tool an Agent is (see ADR-0018)
- `Executor` - where an Agent runs (see ADR-0006, ADR-0018)
- `Verifier` - what decides a Run's work is acceptable (see ADR-0013)
- the idle `Detector` - platform-specific, selected by build tag

## Consequences

- Zero runtime machinery, full type safety, and fakes make every extension
  point trivially testable.
- No ABI or plugin-version compatibility burden while the design is still
  moving.
- Third parties cannot extend Coding Owl without forking and recompiling. That
  is accepted for now and is the main thing this ADR trades away.
- Discipline is required for the seam to stay real: core packages depend on the
  interface, never on a concrete implementation, and implementations do not
  import each other. If that erodes, the ADR has been silently reversed.
- Going external later is additive - the interfaces are already the boundary,
  so a `hashicorp/go-plugin` host or a WASM runtime becomes another
  implementation behind the same contract rather than a redesign.

## Alternatives considered

**`hashicorp/go-plugin` now.** Rejected as premature. It is the right answer
the day third parties exist, and pure overhead until then.

**WASM plugins.** Same verdict, with more immaturity in the Go host story.
