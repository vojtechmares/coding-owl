# AGENTS.md

## Agent skills

### Issue tracker

Issues live as GitHub issues in `vojtechmares/coding-owl`, via the `gh` CLI.
See `docs/agents/issue-tracker.md`.

### Triage labels

Canonical vocabulary, unchanged: `needs-triage`, `needs-info`,
`ready-for-agent`, `ready-for-human`, `wontfix`.
See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root.
See `docs/agents/domain.md`.

### Queueing issues for Owl

`scripts/queue-ready-for-agent.sh` copies `ready-for-agent` issues into the
Coding Owl queue, one Job per issue, with a prompt that checks the issue for
blockers first and leaves a blocked one to be queued again later.
See `docs/agents/queue-from-github.md`.
