# ADR-0030: Verification checks are shell commands with optional expectations

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

ADR-0013 made Verification the gate on Job completion but did not say what a
check is. Two things complicate the obvious answer.

ADR-0022 forbade the shell entirely for the chat's commands, parsing to argv and
calling `execve` directly. Doing the opposite here looks inconsistent and needs
justifying.

And exit status is not a sufficient signal. `gofmt -l .` **exits zero** and
prints the offending filenames; judged by exit code it passes a Job with
unformatted code in it.

## Decision

A Project's checks are a list of named **shell commands**, each with an optional
expectation and a timeout:

```yaml
checks:
  - name: build
    run: go build ./...
  - name: test
    run: go test ./...
    timeout: 10m
  - name: fmt
    run: gofmt -l .
    expect: empty_output
```

`expect` defaults to exit zero. `empty_output` additionally requires stdout to
be empty. Checks run in the Job's worktree. **All checks run**, and every
failure is attached to the Job, rather than stopping at the first.

### On the asymmetry with ADR-0022

The shell is allowed here and forbidden there because of **who writes the
string**. Chat commands are composed by a model at runtime. Check commands are
written by the user and read from the Project's **base branch** (ADR-0014),
where the Agent being verified cannot reach them.

If check commands ever become model-authored - a Driver proposing its own
checks, a skill contributing them - this decision must be revisited, because its
entire basis will have gone.

## Consequences

- Real check idioms work: pipes, `&&`, `$( )`, and the `test -z "$(...)"` shape
  people already have in Makefiles and CI.
- A `blocked` Job reports everything that is wrong at once. When the report is
  read once a day, finding a second failure the next morning is expensive.
- Checks need timeouts or a hung test suite pins a Run until ADR-0011's
  interruption machinery notices. A default timeout is required, not optional.
- `expect` is a deliberately tiny vocabulary. It will attract extensions -
  expected exit codes, output matching, warning thresholds - and should be kept
  small until a real case forces each one.
- Checks execute whatever the Agent left in the worktree, including a
  `Makefile` it may have edited. Reading config from the base branch does not
  protect the commands those checks invoke. This is a real limit of Verification
  under host execution (ADR-0006), and containers are what would close it.

## Alternatives considered

**Argv only, no shell**, exactly as ADR-0022 does. One execution rule across the
whole system, nothing behaving differently depending on authorship. Rejected
because it fights ordinary idioms and needs an expectation mechanism anyway, so
it pays the cost without avoiding the complexity.

**One delegated command per Project** - `make check`, `task verify`. Almost no
configuration and reuses the repo's own entry point. Rejected because Owl gets a
single pass or fail, so a `blocked` Job can only say "checks failed" and the
desktop app has nothing structured to show.
