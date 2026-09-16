# ADR-0028: Model and effort are chosen per phase

- **Status:** Accepted
- **Date:** 2026-09-09
- **Amended:** 2026-09-17, models name their vendor and Drivers declare what they serve

## Context

ADR-0026 split a Job into a planning phase and an execution phase, and they have
very different shapes. Planning is short and decides everything downstream - a
wrong approach wastes the night regardless of how well it is carried out.
Execution is long, comparatively mechanical, and is where the utilization
ceiling (ADR-0020) actually gets consumed.

Claude Code exposes `--model` and `--effort`, so the choice is available per
invocation rather than being a property of the Account.

## Decision

Defaults are per phase:

```yaml
plan:    { model: anthropic/claude-opus, effort: xhigh }
execute: { model: anthropic/claude-opus, effort: high }
```

### A model names its vendor

A model is written `vendor/model`. The vendor is Owl's half and says which
Driver can serve it; everything after the slash is the Driver's own vocabulary,
passed through as written.

That split is what makes one spelling cover both a **versionless alias** -
`anthropic/claude-opus`, which follows whatever the vendor currently calls its
latest Opus - and a **pinned** model like `anthropic/claude-opus-5`. Owl does
not need to know which it was handed, so a Project stays current by default and
pins only when it means to.

The vendor is checked; the name is not. Refusing a model Owl has not heard of
would make the list a ceiling, and vendors ship models faster than Owl ships
releases. A Driver therefore accepts any name for its own vendor and refuses
every other vendor by name, in `resolve`, before the Job gets a worktree.

Because a name that is accepted is not thereby advertised, the set has to be
discoverable some other way: each Driver **declares the models it serves**, and
`owl models` and the desktop app list them with the alias-or-pinned distinction
on each. That is the only way a user learns the vocabulary without reading the
source or being rejected by a configuration file.

Settings cascade **global → Project → Job**, narrowest winning, and
`owl add --model --effort` overrides for a single Job.

`owl jobs show <id>` prints the effective model and effort, alongside the
effective system prompt it already prints (ADR-0017).

## Consequences

- Quality is prioritised over headroom. Running Opus for execution as well as
  planning will reach the ceiling sooner than a cheaper execution model would;
  that is the deliberate trade, and ADR-0020's ceiling is the backstop that keeps
  it from becoming the user's problem.
- `xhigh` on planning costs little in absolute terms, because planning is short.
  It is the highest-leverage token spend in the whole system.
- The cascade is a fourth place to look when behaviour surprises someone, which
  is why the effective values have to be printable.
- Model choice is now a lever against the ceiling. When headroom is short, the
  answer is to lower the execution model rather than to queue less.

## Anticipated

Per-**task** models, once Jobs gain task decomposition. A Task entity would sit
between Job and Run in ADR-0027's model and carry its own model and effort. It
is not designed here, and it is noted so that the configuration shape does not
harden around phases being the only axis: `plan` and `execute` are keys in a map,
not two fixed fields.

## Alternatives considered

**One model and effort for the whole Job.** One knob, no phase vocabulary, and
simpler to read back. Rejected because it forfeits exactly the trade the two
phases offer - buying quality where the leverage is and economising where the
volume is.

**No model configuration at all**, inheriting whatever each Account's Claude Code
settings say. Zero design, and it respects settings already tuned by hand.
Rejected because it removes the only lever against the ceiling and makes it
impossible to let one Job think harder than the rest.
