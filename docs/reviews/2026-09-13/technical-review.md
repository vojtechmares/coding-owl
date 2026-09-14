# Coding Owl technical review

Reviewed on 2026-09-13 at commit `701b936` (main, clean tree). Module
`github.com/vojtechmares/coding-owl`, Go 1.27.1, ~60k lines of Go across
187 files, of which ~14k are the behaviour suite under `tests/behavior`.

Judged against `CONTEXT.md` and the 34 ADRs under `docs/adr/`. All paths
below are relative to the repository root.

## Build and test results

| Step | Result |
| --- | --- |
| `go build ./...` | exit 0 |
| `go vet ./...` | exit 0 |
| `gofmt -l .` | clean |
| `go test -race -count=1 ./...` | exit 0, every package `ok`, no `WARNING: DATA RACE` |

Wall time under `-race` (Apple Silicon, with a second `go test ./...` from a
review agent competing for CPU): unit packages 1-20 s each (`git` 19 s,
`run` 15 s, `executor/host` 15 s, `shell` 12 s, `idle/system` 10 s);
`tests/behavior` 263 s. Total about 6 minutes.

Statement coverage (`go test -cover`, unit packages only; the behaviour suite
drives the built binary and is not counted):

| Package | Coverage |
| --- | --- |
| `internal/run` | 74.0% |
| `internal/queue` | 84.5% |
| `internal/store` | 78.9% |
| `internal/git` | 77.6% |
| `internal/gc` | 82.9% |
| `internal/verifier/agent` | 90.9% |
| `internal/verifier/command` | 85.7% |
| `internal/executor/host` | 93.1% |
| `internal/skill` | 68.5% |
| `internal/chat` | 85.8% |
| `internal/command` | 93.7% |
| `internal/driver/claudecode` | 85.6% |
| `internal/idle`, `internal/idle/system` | 85.7%, 90.7% |

Packages with no tests: `cmd/owl`, `cmd/owl-desktop`, `internal/desktop`,
`internal/agent`, `internal/driver`, `internal/drivers`, `internal/executor`,
`internal/verifier`, `internal/version` (the last six are interfaces or
one-liners; `internal/desktop` is covered indirectly by
`tests/behavior/issue{9,18,21,22}_test.go`).

CI (`.github/workflows/ci.yml`) runs build, vet, gofmt, `go test ./...`,
`go mod tidy -diff` and `buf lint`, but not `-race`.

## 1. Architecture and package structure

### Verdict

The code matches its ADRs unusually closely, and the ADR numbers are cited at
the point in the code that implements them. The dependency graph is a DAG
with a clear composition root:

```
cmd/owl         -> cli -> client -> gen (ConnectRPC)
cmd/owl-desktop -> desktop -> client
daemon (composition root) -> run, queue, store, project, account, chat, gc,
                              skill, credential, driver/claudecode,
                              executor/host, verifier/{command,agent}, idle
run -> account, agent, config, driver, executor, gc, git, idle, project,
       queue, shell, skill, store, verifier
chat -> command, credential, store          command -> stdlib only
project -> config, git, skill, store        config -> yaml only
```

- ADR-0001/0002: one `owl` binary, the daemon never forks and writes no PID
  file (`internal/daemon/daemon.go:76`); launchd supervises it
  (`internal/launchd/launchd.go:295`).
- ADR-0004: ConnectRPC over a unix socket, dirs 0700, socket 0600, careful
  stale-socket handling (`daemon.go:320-343`).
- ADR-0008: only daemon-side packages open SQLite; `cli` and `desktop` go
  through `client`. Verified by import graph.
- ADR-0005/0018: `driver.Driver`, `executor.Executor`, `verifier.Verifier`
  are Go interfaces; the host executor and the Claude Code driver compose
  freely (`daemon.go:157-160`).
- ADR-0022: chat commands are genuinely shell-free (`exec.LookPath` +
  `exec.CommandContext`, `internal/command/run.go:458-465`), allow-listed,
  argument-sanitised, and consent-gated on the exact argv that will run
  (`internal/chat/command.go:236-262`). `internal/shell` (`/bin/sh -c`) is
  imported only by `run` and `verifier/command`.
- ADR-0014: config discovery order is implemented exactly
  (`internal/project/project.go:31-37,260-287`); in-repo config is read from
  `refs/heads/<base>`, never from the worktree, so an Agent cannot change the
  terms it runs under.

### Smells

- `internal/run` is the god package: `run.go` is 1399 lines and the package
  imports 13 internal packages. It holds scheduling (`caps.go`), the run
  lifecycle, verification orchestration, the log broker, pause/resume,
  disposal, the idle watcher and the account ceiling. It is well organised
  into files, but the `Service` struct (`run.go:245-295`) has four mutexes and
  five maps, and `start` (`run.go:389-570`) does 15 distinct things under one
  lock.
- `internal/git/git.go` is 1268 lines covering worktrees, rebase, skill
  mirroring, excludes, diffs and version checks; `untar.go` was already split
  out, the rest should follow.
- `internal/cli` reaches past the client into daemon-side packages in a few
  places: `store.ErrAccountNameTaken` (`cli/account.go:128`),
  `account.CheckName/DirFor/EnsureDir` to create the Account's config
  directory under the daemon's data dir (`cli/account.go:141,157`),
  `drivers`/`agent` to run `setup-token` locally (`cli/account.go:166`). This
  is the one place the "everything through the API" rule of ADR-0008 bends.
- `daemon.go:192,196` builds two independent `queue.Service` instances.
  Harmless today because it is stateless; a trap if it ever grows state.
- `store.Job` and `queue.Job` are field-for-field duplicates with a
  hand-written `FromStore` (`internal/queue/queue.go:287-307`).
  `queue.Job.Position` is `int` with 0 meaning "not queued" while the column
  is nullable, which is where bug 1 below becomes invisible.
- The `config`/`skill` cycle is avoided by duplicating `skillName`
  (`internal/config/config.go:412`), acknowledged in a comment.
- Three copies of a bounded writer: `shell.capped`/`interleaved`
  (`shell.go:246-294`), `host.limitedBuffer` (`host.go:178-200`),
  `system` output cap (`idle/system/darwin.go:215-234`). Two copies of
  `resolve` (`git.go:490`, `gc.go:610`).

### Dead or test-only exported symbols

`skill.Names` (`skill/service.go:404-411`), `command.Timeout`,
`command.DeniedError` (never asserted with `errors.As`), `client.StatusError`
and `client.Kind*` (computed, never consumed; `cli.Run` exits 1 for
everything, `cli/cli.go:90-93`), `daemon.ErrAlreadyListening`,
`desktop.AllowOnce/AllowConversation/Refuse`, `host.process.Pid()` (only
reachable through a type assertion in tests). `store.Migration.Schema/Applied`
are returned but only tests read them. Nothing harmful; `client.Kind*` should
either drive exit codes or go.

## 2. Code quality

### Error handling and context propagation

Generally strong. Errors are wrapped with the operation and the path, refusals
(`RefusedError`, `CappedError`, `busyError`) are distinct from failures and
the watcher treats them differently (`run/watch.go:316-366`). Bookkeeping
writes that must not be lost use `context.WithoutCancel` plus a 10 s timeout
(`run.go:655,888,1096,1206`). Setup commands and the rebase run under the
Service's lifetime rather than the caller's (`run.go:345-359`,
`run/rebase.go:101`), so a client hanging up cannot leave a worktree
mid-rebase.

Gaps:

- Most git calls ignore cancellation entirely. `run()` hard-codes
  `context.Background()` (`git.go:1038-1040`); only `Rebase`, `FetchBase`,
  `abortRebase`, `Mirror` use `runWithin` with a real context. `AddWorktree`
  (a full checkout, runs smudge filters such as git-lfs), `RemoveWorktree`,
  `WorktreeIsClean`, `DiffPatch`, `DiffStat`, `CommitPath` cannot be
  interrupted by daemon shutdown.
- `CommitPath` (`git.go:1024`) commits the handoff without
  `-c commit.gpgsign=false`, unlike `Rebase` (`git.go:752`) which explains
  exactly why that matters unattended. A user with `commit.gpgsign=true` and a
  passphrase-protected key hangs the daemon with no deadline.
- End-of-run `DequeueJob` failures are logged and swallowed
  (`run.go:663,911,921,925`), so any Job that hits `ErrNotQueued` stays
  `active` with an ended Run until a daemon restart.
- `http.Server` has no `BaseContext` (`daemon.go:199-202`), so in-flight
  handlers never observe daemon cancellation; see bug 4.

### Goroutine and lifecycle safety

No data races under `-race`, and no leaks I could confirm. Shutdown ordering
in `daemon.go:258-278` is right: stop the watcher, close the runs (cancel,
release paused Agents, refuse new starts under the same lock, wait), then
`srv.Shutdown`, then remove the socket, then close the store. `run.Service.Close`
(`run.go:318-338`) correctly cancels before taking the `starting` lock so a
start stuck in a 15-minute setup command lets go.

Points of attention:

- `start` holds `s.starting` across worktree creation, rebase, fetch, skill
  placement and every setup command (`run.go:390-570`). The comment owns this
  cost, but it means `owl start` from a terminal blocks for minutes behind the
  watcher's own start, and every other Job waits even at
  `maxParallelRuns: 4`.
- The `starting` lock covers nothing in `queue.Service`: `Cancel`, `Reorder`
  and `Extend` run against the store with no coordination. That is bug 1.
- `kill()` (`run/interrupt.go:405-416`) logs at Error when `SignalGroup`
  returns `ErrProcessDone`, which can happen benignly if the Run's `Wait`
  returned but its deferred `forget()` has not yet run when the 5 s timer
  fires. Plausible, cosmetic.
- `SignalGroup` has a TOCTOU between `reaped.Load()` and `syscall.Kill(-pid)`
  (`host.go:130-133`); an atomic bool is not a lock. Tiny window, low
  severity, and it is exactly the hazard the guard exists for.
- `Watch` does not wait for an in-flight `begin` goroutine on the way out
  (`watch.go:63-69`); that is sound because `start` re-checks `stopping`
  under the lock and `runs.Close` waits for the Run it may start.

### SQLite usage

Solid. DSN `file:<path>?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)`
with `SetMaxOpenConns(1)` (`store/store.go:50-58`); every state change that
touches more than one row is in `inTx`; `FinishRun` spends the attempt only
when `outcome = ''` matched, so it is spent exactly once (`runs.go:68-83`);
`ReturnJobToQueue` decides pending/exhausted in one `UPDATE ... CASE ...
RETURNING` (`jobs.go:363`); every `*sql.Rows` is drained and closed before
the next statement on the single connection. Times are RFC 3339 text but
nothing orders by them (the comment at `runs.go:228-232` explains why).
Migrations are embedded, each in its own transaction with the
`PRAGMA user_version` bump inside it (`store.go:78-124`).

Weaknesses: the migration version is the index into the sorted file list, so
inserting or renaming a file mid-sequence silently applies the wrong one and
there is no checksum; `SetJobState` is an unconditional `UPDATE`
(`jobs.go:133`) while `MoveJobState` is a proper compare-and-set
(`jobs.go:140-151`) and is used only by gc; `UpsertJob` (ADR-0032) rewrites
only `prompt` on conflict and silently drops `planned/model/effort/ttl` of a
re-production, and will rewrite the prompt of an `active` Job mid-Run;
`account.Remove` is count-then-delete without a transaction
(`account.go:634-650`).

### Process handling

The host executor (`internal/executor/host/host.go`) is careful: `Setpgid`,
`stdin` from `/dev/null`, `Dir` required, SIGTERM to the group on cancel,
stderr into a mutex-protected 8 KiB buffer so a chatty stderr cannot deadlock
stdout, `SignalGroup` refused after `Wait` so a recycled pid is never
signalled, and a post-reap SIGTERM to the group to take children with the
Agent. Pause/resume/expire/kill in `run/interrupt.go` implement ADR-0011 and
ADR-0034 faithfully for the grace-window path, including SIGCONT before
SIGTERM on a frozen group and SIGKILL to the group five seconds later
(`interrupt.go:387-417`).

Gaps: bugs 2 and 3 below (a hang on an over-long stdout line, and SIGKILL
reaching only the Agent's pid on the daemon-stop path). Git operations:
branch names are always `<prefix>job-<id>`, every rev is addressed as
`refs/heads/...` behind `--end-of-options` or `--`, redirecting `GIT_*`
variables are scrubbed (`git.go:1108-1127`), `DeleteBranch` is only reachable
from `Drop` on a Job in `review` and `git branch -D` refuses a branch checked
out anywhere, so it can never touch the user's checkout. Rebase abort handling
is thorough and tested against real git, including a git killed mid-way
(`git/abort_test.go:197`, `git_test.go:1380`), and gc versus a Run in flight
is excluded by reading candidates before Jobs, skipping non-terminal states,
and sharing the `disposing` lock with accept/drop (`gc.go:175-179,285,308`).

## 3. Testing

### Shape

- Unit tests are hermetic and fast. Git tests run real git under
  `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_SYSTEM=/dev/null`, explicit
  author env and `git init -b main` (`git_test.go:27-36`), so they do not
  depend on the developer's `user.name`, default branch or signing config.
- The gc tests inject interleavings (`gc_internal_test.go`: `Interleave`,
  `BeforeDeciding`) to pin the exact orderings the race analysis depends on,
  which is the right way to test that class of bug.
- `tests/behavior` holds 405 tests in 22 files, each mapped to numbered
  scenarios in an `issue-N.md` beside it. `TestMain` builds `owl`,
  `fakeclaude` and `fakemachine` once (`issue2_test.go:44-125`); every daemon
  gets its own XDG layout, a file credential store and a stub machine, so no
  real keychain, Claude or idle detector is touched. They poll with `waitFor`
  (5 s deadline, 20 ms step) rather than sleeping, with deliberate 200 ms
  `still()` negative checks. No `t.Parallel()` anywhere in the suite.

### Critical-path coverage

Well covered: queue order, caps and which cap binds, TTL and exhaustion,
pause/resume/grace-window/SIGKILL group semantics, daemon recovery after a
crash, verification pass/fail/cut-short, verifier fresh session and verdict
parsing, skill locking and placement, rebase abort and conflict reporting,
idle transitions, chat consent.

Gaps that matter:

- No concurrency test in `store`, `queue` or `run` for a Job being cancelled,
  reordered or disposed of while `start` is between `scan` and
  `SetJobState` (bug 1).
- No test for an Agent that writes a stdout line over `maxLine` and then
  keeps writing (bug 2).
- No test that daemon shutdown kills a TERM-ignoring child of the Agent
  (bug 3); S20 covers the grace-window path only.
- No test that a daemon-side `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`
  is absent from an Agent's environment (bug 5); S9 in issue 14 checks
  presence, not absence.
- No test that daemon shutdown completes while a chat stream is parked on
  consent (bug 4).
- `CommitPath` has no test in `internal/git` at all, and there is no test of
  the production code under hostile repository config (`commit.gpgsign`,
  `status.showUntrackedFiles=no`); the harness scrubs global config for its
  own git calls but the code under test inherits `os.Environ()`.
- No test that an unknown YAML key is refused (it is not, bug 7).
- `internal/desktop` has no in-package tests; `internal/cli` has four.
- Migration ordering and partial failure are untested.

### Slow or flaky-prone tests

- `tests/behavior`: 263 s under `-race`, all serial. The 1 s grace-window
  scenarios assert `< 2.5 s` (`issue11_test.go:280-291`) and every `waitFor`
  has a 5 s ceiling; both are the first things that will flake on a loaded CI
  runner. `fakeclaude`'s 30 s release timeout bounds a hang.
- `TestRebaseKilledMidWayLeavesNoRebaseAndNoLock` is ~7 s (a `sleep 10`
  smudge filter, a 2 s context and the 5 s `WaitDelay`), nearly half of the
  `git` package's runtime. Correct, just slow.
- `executor/host` (15 s) and `shell` (12 s) are sleep-bound by design.
- CI does not run with `-race`, so the race-free result above is not
  something CI currently guarantees.

## 4. Correctness bugs

Ranked by severity. "Confirmed" means the path was traced in the code; nothing
below was reproduced at runtime.

### High

**1. A Job cancelled while its Run is being started is resurrected as
`active` and then lost.** Confirmed. `queue.Cancel` (`queue.go:311-316`) is
check-then-act on the store with no lock shared with `run.Service`.
`start` holds `s.starting` (`run.go:390`), reads the queue in `scan`
(`caps.go:83-109`), then spends up to minutes on `git.AddWorktree`, the
rebase, skill placement and the Project's setup commands
(`run.go:458-498`) before `StartRun` (`run.go:507`) and an unconditional
`SetJobState(active)` (`run.go:530`, `jobs.go:133`). If `owl jobs cancel`
lands in that window, the Job has `state=cancelled, position=NULL`; `start`
then inserts a Run for it and flips it back to `active`. Consequences:
`inFlight` does not count it (`caps.go:58` requires a position), so caps are
under-counted; at the end of the Run every branch of `carryOut` calls
`DequeueJob` (`run.go:911,921,925`), which requires `position IS NOT NULL`
(`jobs.go:247`), fails with `ErrNotQueued`, and is only logged, so the Job
stays `active` forever with no Run; or `requeue` sets it `pending` with no
position (`jobs.go:363`), invisible to `owl queue list`. On the next daemon
restart `requeueLeftOver` (`interrupt.go:455-470`) puts the cancelled Job
back in the queue. The same shape applies to `Reorder` (`queue.go:348`) and
to `dispose` (`dispose.go:88,128`: read `review`, then unconditional
`SetJobState`). Fix: claim with `MoveJobState(pending -> active)` before the
slow work and treat `false` as "somebody else decided", and give `DequeueJob`
an expected `from` state.

### Medium

**2. A Run hangs forever after one over-long stdout line.** Confirmed.
`execute` scans stdout with `maxLine = 8 MiB` (`run.go:47,1320-1333`). On
`bufio.ErrTooLong` the loop exits without draining the pipe and calls
`proc.Wait()` (`run.go:1335`). An Agent still writing more than the 64 KiB
pipe buffer blocks in `write(2)`; `cmd.Wait` never returns because
`WaitDelay` only starts after cancel or process exit. The Run holds its Job
until the daemon stops. The agent verifier already does this right
(`verifier/agent/agent.go:371-374` drains with `io.Copy(io.Discard, ...)`).
A Claude Code tool result over 8 MiB in `stream-json` is unusual but
possible. Fix: drain stdout unconditionally after the scan loop.

**3. On the daemon-stop and verifier-timeout paths SIGKILL reaches only the
Agent's pid, not its group.** Confirmed. ADR-0034 says the escalation "applies
to a Run being ended by a daemon that is stopping; that was already the
executor's behaviour". It is not: the grace-window path sends SIGKILL to the
group (`interrupt.go:401-416`), but the context-cancellation path relies on
os/exec's `WaitDelay` (`host.go:81-82`), which after 5 s calls
`os.Process.Kill()` on the pid only. A TERM-ignoring child (a stuck test
runner) survives daemon shutdown. The same applies to `shell.Run`
(`shell.go:193-194`) for a verification check that times out. The post-reap
group SIGTERM in `Wait` (`host.go:160-162`) does not help against a child
that ignores SIGTERM.

**4. Clean daemon shutdown fails while a chat exchange is open.** Confirmed.
`http.Server` has no `BaseContext` tied to the daemon's `ctx`
(`daemon.go:199-202`). A `SendMessage` stream parked on consent
(`chat/command.go:242-262`, up to 5 min) or in a provider stream
(`chat/client.go:20`, 5 min) makes `srv.Shutdown` hit its 5 s deadline;
`Run` returns `shutting down: context deadline exceeded` (`daemon.go:275`),
the process exits non-zero and launchd logs a failure. Fix:
`srv.BaseContext = func(net.Listener) context.Context { return ctx }`, or a
`chat.Service.Close` that cancels the waiters.

**5. The daemon's own credentials leak into Agents of every Account.**
Confirmed by reading. `host.Start` builds the environment as
`append(os.Environ(), inv.Env...)` (`host.go:72`); `accountEnv` adds only
`CLAUDE_CONFIG_DIR` and, when the token is non-empty, `CLAUDE_CODE_OAUTH_TOKEN`
(`driver/claudecode/claudecode.go:242-253`). Nothing anywhere unsets
`ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or a daemon-level
`CLAUDE_CODE_OAUTH_TOKEN`. So an `ANTHROPIC_API_KEY` in the daemon's
environment is used by every Agent regardless of Account (Claude Code
prefers it), and an Account with an empty token inherits the daemon's OAuth
token. Under launchd the environment is usually empty, so this bites `owl
daemon run` from a shell; it still contradicts the isolation promise of
ADR-0019. Fix: have the driver explicitly clear those variables.

**6. The fetch never feeds the rebase.** Confirmed. `FetchBase` updates only
`refs/remotes/<remote>/<base>` (`git.go:935`), while `Rebase` replays onto
`refs/heads/<base>` (`git.go:753`). Nothing fast-forwards the local base, so
every Run rebases onto whatever the user last pulled and the (up to two
minute) fetch has no effect on the outcome. `run/rebase.go:46-49`
half-acknowledges this, but ADR-0016 reads as though the fetch matters.
Either rebase onto the remote-tracking ref when the base has one, or drop the
fetch and reword the ADR.

**7. Non-strict YAML lets typos and cross-file keys pass silently.**
Confirmed. `yaml.Unmarshal` into `file` with no `KnownFields(true)`
(`config.go:299,496`), and one `file` struct serves both project and global
files. A misspelled `maxParalellRuns` silently means 1; a `checks:` block in
the global file is silently dropped; `credentialStore:` in a project file is
accepted. The `limits` block hand-builds strictness (`config.go:618-629`),
which shows the intent.

**8. `CommitPath` can hang unattended on a signing key.** Confirmed.
`git.go:1024` commits the handoff with `owlIdentity` only, through `run()`
under `context.Background()`; `Rebase` at `git.go:748-752` explains why
`commit.gpgsign=false` is needed unattended and then `CommitPath` omits it.

### Low

**9. A poisoned head-of-queue blocks everything behind it.** Confirmed.
`scan` always picks the oldest permitted Job; if `git.AddWorktree` fails for
it (branch name already exists, worktree directory left over from a previous
attempt where `SetJobWorkspace` failed at `run.go:470`) the error is returned
as a plain error, not a refusal or a block (`run.go:467-468`), the watcher
backs off (`watch.go:301-310`) and picks the same Job next time. Rebase
failures block the Job; worktree failures should too.

**10. `gc.prune` prunes the user's own worktrees.** Confirmed. `stale()`
(`gc.go:462-478`) lists every linked worktree of the Project and
`PruneWorktrees` (`git.go:654`) runs an unqualified `git worktree prune`. A
user's worktree on an unmounted volume gets its admin entry removed by Owl.
ADR-0015 does say "runs `git worktree prune` per Project", so this is
arguably by decision, but scoping `stale` to `WorktreeDir` keeps the same
behaviour for Owl's worktrees and leaves the user's alone.

**11. Listener and socket not cleaned up on start-up errors after
`net.Listen`.** Confirmed. `daemon.go:109-171`: `LoadGlobal`, `ParseKind`,
`credential.Open` and `runs.Recover` all return without `ln.Close()` or
`os.Remove(sock)`. Self-healing on the next start (`ECONNREFUSED`), but the
ordering is backwards: validate, then listen.

**12. Socket permission window.** Confirmed. `net.Listen` then `chmod 0600`
(`daemon.go:109-118`); the socket exists at umask permissions in between, and
a pre-existing `$XDG_RUNTIME_DIR/coding-owl` is not re-tightened.

**13. End-of-run `ErrNotQueued` is swallowed.** Confirmed
(`run.go:663,911,921,925`); `runs.go:264-268` acknowledges the gap. Fixing
bug 1 removes the main way to reach it, but the branches should still surface
it rather than log it.

### Plausible (not confirmed)

- `ClearStaleLocks` clears only `index.lock` and `HEAD.lock`
  (`git.go:866`); a git killed during a ref update also leaves
  `refs/heads/<branch>.lock` or `packed-refs.lock`, on which
  `abortRebase` would then fail and block the Job "for good on a file nobody
  is using", the exact outcome the function exists to prevent.
- `WorktreeIsClean` runs `status --porcelain` without
  `--untracked-files=all` (`git.go:262`); a user `status.showUntrackedFiles=no`
  makes untracked work look clean to both Owl and to git's own `worktree
  remove` guard.
- `FetchBase` reads `branch.<base>.remote` but not `branch.<base>.merge`
  (`git.go:964`), so a local `main` tracking `origin/trunk` warns every Run.
  Harmless while bug 6 stands.
- `queue.Cancel` on a Job that already has a branch leaves the branch behind:
  gc reclaims the worktree (`gc.go:560`) but only `Drop` deletes branches.
- Concurrent `Send` on one chat conversation is unserialised
  (`chat/send.go:466-509`); two streams can interleave history writes.
- Chat commands inherit `HOME`, so `diff.external`, `textconv` or
  `core.fsmonitor` from `~/.gitconfig` or `.git/config` would run a program on
  `git diff`/`git show` (`command/run.go:424-438`). User-controlled, not
  model-controlled, so within ADR-0022's threat model, but
  `GIT_CONFIG_GLOBAL=/dev/null` and `-c diff.external=` would close it.
- `LoadGlobal` returns `Global{}` (MaxParallelRuns 0) for a missing file
  (`config.go:793`) while `DefaultGlobal()` says 1; `run.go:1280` patches it
  but `daemon.go:125` uses the zero value directly and survives only because
  `collect` and `idleInterval` treat `<= 0` as default.
- `exhaust` is documented as moving Jobs "out of the queue"
  (`caps.go:224-225`) but `ReturnJobToQueue` never nulls `position`
  (`jobs.go:363`), so exhausted Jobs keep a position, `CountQueued` and
  `Reorder`'s bounds count Jobs the user cannot see, and pending positions
  show gaps.

## 5. Top 5 recommended improvements

1. **Make every Job state transition a compare-and-set, and claim the Job
   before the slow work.** Replace `SetJobState` at `run.go:530` and
   `dispose.go:128` with `MoveJobState`, give `DequeueJob` an expected `from`
   state, and move the claim (`pending -> active`) to right after `scan`,
   before `AddWorktree`, so `cancel`, `reorder` and `dispose` cannot race a
   start. Add a store-level table test that calls each transition from every
   other state, and a `run` test that cancels during a slow setup command.
   Fixes bugs 1, 13 and most of the "plausible" queue items; it is the only
   high-severity finding.

2. **Close the three process-boundary gaps in one change.** Drain stdout
   after the scan loop in `execute` (bug 2); make the daemon-stop path
   escalate to SIGKILL on the group by giving the host executor its own
   cancel goroutine (SIGTERM group, 5 s, SIGKILL group) instead of leaning on
   `WaitDelay`, and do the same in `shell.Run` (bug 3); have the driver clear
   `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN`
   before adding the Account's own (bug 5). Each needs one test: a long
   line followed by more output, a TERM-ignoring grandchild on daemon stop,
   and an absence check on the Agent's environment. Then ADR-0034's last
   paragraph becomes true.

3. **Give git a context and unattended-safe config everywhere.** Route
   `AddWorktree`, `RemoveWorktree`, `WorktreeIsClean`, `CommitPath`,
   `DiffPatch` and `DiffStat` through `runWithin` with the Service's
   lifetime, add `-c commit.gpgsign=false` to `CommitPath` and
   `--untracked-files=all` to `WorktreeIsClean`, and decide what `FetchBase`
   is for (rebase onto the remote-tracking ref, or drop it and amend
   ADR-0016). Add tests that set hostile config in the repository itself
   (`commit.gpgsign`, `status.showUntrackedFiles`), as
   `TestRebaseDoesNotAskARepositorysPreRebaseHook` already does for hooks.
   Fixes bugs 6 and 8 and the two git "plausible" items.

4. **Tie request handlers to the daemon's lifetime and make config strict.**
   Set `srv.BaseContext` to the daemon context (bug 4), validate config and
   credentials before `net.Listen` and close the listener on every early
   return (bugs 11, 12), and decode YAML with `KnownFields(true)` into two
   separate on-disk structs for project and global files (bug 7). Return
   `DefaultGlobal()` from `LoadGlobal` when the file is missing. These are
   small, contained, and each removes a class of silent misbehaviour.

5. **Shrink `internal/run` and `internal/git` along the seams that already
   exist, and run `-race` in CI.** Split `run.Service` into a scheduler
   (`scan`, `holds`, `exhaust`, ceiling), a run supervisor (`start`,
   `carryOut`, `execute`, pause/resume/expire) and a disposal/overview
   service that share the store; split `git.go` into worktree, rebase,
   mirror/skill, diff and excludes files; fold the three bounded writers and
   two `resolve` helpers into one each; delete the dead exports listed in
   section 1 or wire `client.Kind*` to exit codes. Add `-race` to the CI
   test step (it passes today, so this costs nothing but time) and consider
   marking the behaviour scenarios `t.Parallel()` per daemon, since each
   already has its own XDG layout and socket.

## Appendix: things that are notably well done

- Every `git` invocation is `-C`-scoped, ref-addressed as `refs/heads/...`,
  and uses `--end-of-options` or `--`; there is no shell anywhere near a
  user-controlled string.
- The rebase machinery (`git.go:726-862`) handles conflict, autostash
  conflict, killed-mid-way and abort-refused, each with a real-git test.
- The agent verifier reads its verdict from a file opened through `os.Root`
  with `O_NOFOLLOW`, fails closed on every ambiguity, and fences the plan and
  diff with a grown fence against prompt injection
  (`verifier/agent/agent.go:396-438`, `prompt.go:487-502`).
- Skill fetching is git-only with `protocol.ext.allow=never`,
  `credential.interactive=never`, `GIT_TERMINAL_PROMPT=0`, a safe tar
  unpacker (`git/untar.go`), content digests re-verified on every use, and
  symlink placement that never leaves a missing skill mid-swap
  (`skill/place.go:548-563`).
- The handoff and log files are opened with `O_NOFOLLOW` and size-bounded
  (`run.go:611-632,1297`), and `mkdirPrivate` tightens existing directories
  rather than trusting `MkdirAll`.
- The behaviour suite maps every test to a numbered scenario in a markdown
  file beside it, and is fully hermetic.
