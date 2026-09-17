# GitHub issues into the Owl queue

`scripts/queue-ready-for-agent.sh` copies every open GitHub issue that carries
`ready-for-agent` into the Coding Owl queue, one Job per issue.

```sh
./scripts/queue-ready-for-agent.sh --dry-run   # what it would queue
./scripts/queue-ready-for-agent.sh             # queue it
./scripts/queue-ready-for-agent.sh --help      # every flag
```

Issues are queued lowest number first, so that the oldest work is queued first
and, the queue being first in first out (ADR-0025), leaves it first.
`--highest-first` turns that around, for a repository where the newest issue is
the one that matters. Either way `--limit` takes its issues from that end.

It stands in for the GitHub Source that ADR-0008 and ADR-0032 describe, until
one exists. A real Source would produce Jobs with `source: github` and
`source_ref: issue:108`, and the unique constraint on that pair would be its
idempotency. A shell script cannot write those columns - `owl add` produces
through the local queue Source - so it does the same job by reading the queue
back, which is why the prompt starts with a marker.

## What a Job's prompt says

The prompt is built from one template in the script. It carries:

- **a marker, `[gh#108]`, as the first thing on the first line.** It is how a
  later pass recognises the Job as this issue's. `owl queue list` cuts a prompt
  short at 60 characters, so the marker leads, or running the script again
  would queue everything a second time.
- **the issue by number, title and URL**, and the `gh` commands that read it,
  ready to paste. The Agent never has to work out which ticket it is on, or
  search for it.
- **the blocker check**, below.
- **what to do with the work**: the repo's own conventions, tests first,
  Conventional Commits with `--signoff`, a pull request whose body opens with
  `Closes #108`, and no merge - Owl's Verification runs after the Agent exits
  and a person accepts or drops the work.

The issue, not the prompt, is the specification. The prompt says so, and the
Agent reads the issue fresh every Run, because an issue can change between
being queued and being worked on.

## Blocked issues come back to the queue

Every Run checks its issue for blockers before it does anything else:
`gh issue view` for the state and the labels, the `dependencies/blocked_by` and
`sub_issues` APIs, the timeline for an open pull request it waits on, and the
body and comments for a blocker written in prose.

An issue is blocked when it has stopped being ready - it is closed, or lost
`ready-for-agent`, or carries `needs-info`, `needs-triage`, `ready-for-human`
or `wontfix` - or when something it depends on has not landed, or when it needs
credentials or an irreversible action the Agent does not have. Ambiguity is not
a blocker: the unattended contract (ADR-0017) says to assume and write it down.

A blocked Run comments once on the issue, changes nothing at all - no plan, no
handoff, no commit, no branch - and stops. The Job ends **blocked**: a planning
Run that writes no `.coding-owl/HANDOFF.md` has produced no plan, which fails
the Run, and a failed Run blocks its Job. That is the right end for it, because
there is nothing to review.

The issue keeps its label, so the next pass of the script sees a blocked Job
for a still-ready issue and queues it again. That is the whole loop: an issue
that was blocked is returned to the queue and tried again later, within the
attempt cap below, and the Agent never works around a blocker or relabels an
issue on its own - relabelling is a person's decision (`docs/agents/triage-labels.md`).

Because the blocked path relies on the planning Run, leave planning on.
`--no-plan` queues Jobs that are carried out directly, and a blocked one of
those ends in **review** with an empty diff instead, which needs a person to
drop it before the issue can come back.

## What a second pass does

Job state for the issue | what the script does
--- | ---
none | queues it
`pending`, `active` | leaves it alone - it is already queued or running
`review`, `done` | leaves it alone - the work exists and it is your turn
`blocked`, `exhausted` | queues it again, unless `--no-requeue`
`cancelled` | leaves it alone - you dropped it by hand; `--force` overrides

An issue is queued again at most `--max-attempts` times, three by default. An
issue that comes back every pass and ends the same way every time is not
waiting on a blocker that will clear - something about it, or about the
Project, needs a person - and queueing it for ever would spend an attempt a
night on it. The script says so and leaves it; `--force` queues it anyway.

## The Agent needs gh

Owl's default allowlist (ADR-0035) grants an Agent its editor, the repository
and the usual build and test runners. It does not grant `gh`, and an Agent that
cannot run `gh` cannot read its issue - it will stop and say so.

Grant it in the Project's own `.coding-owl.yaml`, on the base branch:

```yaml
allowedTools:
  - Bash(gh:*)
```

The script warns when it can see neither that grant nor one in the Account's
`settings.json`, and warns when the Project names no Account, since Runs then
fail before an Agent starts.

## Relation to the Ralph loop

`docs/agents/queue-worker.md` works the same label with a Ralph loop, in your
terminal, one issue per iteration. This script hands the same queue to Owl
instead, to be worked while the machine is idle. Use one or the other for a
given issue, not both at once: neither knows about the other's branches.
