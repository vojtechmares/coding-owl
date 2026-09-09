# ADR-0018: Driver and Executor are separate plugin axes

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Coding Owl is not meant to be a Claude Code launcher forever - opencode, pi and
others should be usable. ADR-0012 anticipated this: *"worth revisiting if
`Executor` ever needs to run a different agent."*

"Which tool" and "where it runs" are independent questions. Conflating them
multiplies: three tools across three runtimes is nine implementations, and every
container fix gets made three times.

## Decision

Two orthogonal interfaces, in the sense of ADR-0005.

**`Driver`** knows one coding tool: how to build its argv, parse its output
stream, inject the unattended contract (ADR-0017), and cap its spend. Each Driver declares its **capabilities**:

```
Capabilities{ streaming_output, budget_cap,
              permission_modes, usage_reporting }
```

**`Executor`** knows one placement: host process now, container later. It starts
a command, signals it, and waits.

Any Driver composes with any Executor. `claude-code` is the only Driver in the
MVP.

## Consequences

- Containerisation gets written once, against `Executor`, and every Driver
  inherits it.
- Owl must degrade honestly on capability. Since ADR-0026 removed Session reuse,
  `resumable_session` is no longer required of anything - a Driver without one is
  first-class, because continuity is a committed handoff document rather than a
  conversation.
- `usage_reporting` is what ADR-0020's ceiling depends on. A Driver that cannot
  report utilization cannot be scheduled under a ceiling, only under a
  reactive limit.
- Claude-specific mechanics move behind the Driver: `--append-system-prompt`,
  `--permission-prompts none`, `--max-budget-usd`, `CLAUDE_CONFIG_DIR`. Nothing outside `internal/driver/claudecode` names them.
- The `Driver` interface is wide, and widening it later is a change to every
  implementation. Capabilities exist so it can stay wide without forcing every
  Driver to implement everything.

## Alternatives considered

**One interface per tool-and-placement pair** - `ClaudeCodeHost`,
`ClaudeCodeDocker`, `OpencodeHost`. Each implementation is simple and free to
exploit its own quirks. Rejected for the multiplication and the duplicated
container logic.

**Driver only, with placement folded in.** Fewest abstractions today, and it
matches an MVP where host execution is the only placement. Rejected because it
puts ADR-0006's deferred container work inside every Driver.
