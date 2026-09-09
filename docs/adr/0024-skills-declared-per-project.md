# ADR-0024: Skills are declared per Project and managed by Owl

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Agents do better work with reusable skills - house style, review checklists,
domain conventions. Claude Code accepts them through `--plugin-dir` and
`--plugin-url`.

Unattended execution changes the risk profile of updating them. A skill that
silently moves between Tuesday and Thursday means two Jobs in one Project ran
under different instructions, and a bad upstream change degrades every Project
overnight with nothing in the diff to explain it.

## Decision

Skills are declared in **Project configuration**, by source and ref:

```yaml
skills:
  - git: github.com/x/go-review
    ref: v1.4.0
  - git: github.com/me/house-style
    ref: main
    auto_update: true
```

Owl materialises them into a cache and hands them to the Driver in whatever form
its tool takes (`--plugin-dir` for `claude-code`).

**Pinned is the default.** `owl skills update` resolves new refs deliberately.
`auto_update` is opt-in per skill. The resolved version of every skill is
recorded on each Run.

Skills are managed through an `owl skills` command group in the spirit of
`npx skills`, and through the desktop app.

## Consequences

- A behaviour change is always attributable: the Run records exactly which skill
  versions were in play.
- `owl skills` is a real subcommand surface - list, add, remove, update, show -
  and the desktop app needs a view for it.
- Skills are the obvious first thing to deliver externally, and the natural
  first tenant of `plugins.codingowl.dev` when that arrives. They are the
  worked example that the plugin story in ADR-0005 has otherwise lacked.
- Materialisation needs a content-addressed cache under the data directory and
  an integrity check on fetch.
- **A skill is a code-execution vector.** It ships instructions, and possibly
  tools, into an Agent that runs unsandboxed on the host (ADR-0006). Skill
  sources deserve the same scrutiny as dependencies, and `auto_update: true` on
  a third-party skill is a standing invitation. Pinning by default is as much
  about this as about reproducibility.

## Alternatives considered

**Thin passthrough** - Project config names plugin directories and Owl passes
them unchanged. Nothing to build and it inherits upstream improvements free.
Rejected because there is then no pinning and no record of what actually ran,
which is most of what "managed" needs to mean here.

**A Driver-neutral skill format** that each Driver translates for its tool. The
only way one skill genuinely works across every Agent, and where a registry
would eventually want to be. Deferred as too speculative to design against a
single real implementation.
