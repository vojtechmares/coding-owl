# Getting started

Six steps from a fresh install to reviewing work Owl did while you were away.
Install it first - `brew install vojtechmares/tap/coding-owl` and `brew
services start coding-owl` - as
[Installing](../../README.md#installing) describes, and read
[Risks](../../README.md#risks) before you queue anything: an Agent runs on your
machine as you, with no sandbox around it.

## 1. Add an Account

An Account is a subscription Owl runs work on, with a tool configuration
directory of its own so a night of Owl's work never touches your setup. The
token goes to the OS keychain.

```
owl account add work
```

This runs the coding tool's own token setup against the Account's directory and
takes the long-lived token it prints. On a machine that has a token already,
`owl account add work --token-stdin` reads one from standard input instead.

## 2. Register a Project

A Project is a git repository Jobs are queued against. The name defaults to the
directory basename and the base branch to the repository's current branch.

```
owl project add ~/code/my-app
```

## 3. Tell Owl how to verify work

Verification is what decides whether a Job is done, and it runs the Project's
own checks from the base branch, out of the Agent's reach. Commit a
`.coding-owl.yaml` on that branch naming the Account and the checks:

```yaml
# .coding-owl.yaml
apiVersion: codingowl.dev/v1
account: work
checks:
  - name: test
    run: go test ./...
```

Every field that file takes is under
[Configuration](../../README.md#configuration).

## 4. Queue a Job

A Job is a standing intent to do one piece of work in one Project. Run this
inside the Project, or name one with `--project`:

```
owl add "Add a --json flag to the status command"
```

The Job goes to the back of the queue, which is first in, first out. Its first
Run writes a plan, and the plan becomes the prompt the work is carried out
from.

## 5. Let it run, or make it

Owl starts work on its own once the machine has been idle for ten minutes and
is on AC power. To watch it happen now instead, run the Job at the head of the
queue yourself and follow the Agent's output:

```
owl start
owl logs <run> -f
```

`owl start` prints the run id to pass to `owl logs`. Touch the keyboard and the
Run freezes; leave the machine alone again and it carries on.

## 6. Review in the morning

`owl status` is the morning question: what ran, what is running, and what is
waiting for you. A Job that passed Verification is in review, and yours to keep
or refuse.

```
owl status
owl jobs show <job>
owl jobs accept <job>
owl jobs drop <job>
```

`owl jobs accept` keeps the branch exactly where the Agent left it and reclaims
the worktree; merging is yours to do. `owl jobs drop` deletes the branch and
the worktree.

## Where to go next

- [Working with Jobs](jobs.md) - queueing, reordering, following a Run, and
  deciding what to do with the work.
- [Projects and Accounts](projects-and-accounts.md) - registering repositories,
  managing subscriptions, and the standing instructions every Run reads.
