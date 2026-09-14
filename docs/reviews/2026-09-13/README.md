# Coding Owl project review

Reviewed on 2026-09-13 at commit `701b936` on `main` (clean tree). Three independent reviews were run - technical, dependency and production readiness - and this report combines them. The full analyses sit beside this file as `technical-review.md`, `dependency-review.md` and `production-readiness-review.md`.

## Overall verdict

**Solid engineering, not yet a product.** The code matches its 34 ADRs closely, builds and passes `go test -race ./...` with no races, has zero known vulnerabilities and no removable weight in its dependency tree. What is missing is everything a stranger would need: a first tagged release, a README, and - most importantly - a way for the unattended agent to actually be allowed to edit files. One confirmed high-severity race in the queue and a handful of medium process-boundary bugs should be fixed before anyone else runs it overnight.

Readiness level: **usable by the author only**. Early adopters are one focused week away, provided the six blockers below are closed.

## Health at a glance

| Area | State | Evidence |
| --- | --- | --- |
| Build, vet, gofmt | clean | all exit 0 |
| Tests | all pass under `-race` | ~6 min wall time, 405 behavioural scenarios plus unit suites |
| Coverage of core packages | 74-93% | run 74%, queue 85%, store 79%, git 78%, host executor 93% |
| Direct dependencies | 7, all latest | `go mod tidy` clean, `govulncheck` and `npm audit` clean |
| Release binary | 21.3 MB, no cgo | desktop app 15 MB, cgo for the Wails webview only |
| Releases and tags | none | release workflow has never run, tap has no formula |
| README | 13 bytes | CLI help text is the only user-facing documentation |
| Open tracker items | 2 issues, 1 PR | none of the blockers below is filed |

## 1. Technical review

### What is good

- Architecture is a clean DAG with a single composition root in the daemon. CLI and desktop never touch SQLite and go through the ConnectRPC client, exactly as ADR-0004 and ADR-0008 require.
- Every git call is `-C`-scoped, addressed through `refs/heads/...` behind `--end-of-options`, and never passes through a shell. The rebase machinery handles conflict, autostash conflict, killed-mid-way and abort-refused, each with a real-git test.
- SQLite usage is careful: WAL, `busy_timeout`, single connection, multi-row changes in transactions, attempts spent exactly once via a conditional update.
- The host executor sets its own process group, feeds stdin from `/dev/null`, guards against pid reuse after reap, and implements the ADR-0011 and ADR-0034 grace-window escalation faithfully.
- The verifier reads verdicts through `os.Root` with `O_NOFOLLOW`, fails closed, and fences plan and diff against prompt injection. Skill fetching is git-only with interactive credentials and ext protocols disabled.
- The behaviour suite is fully hermetic (own XDG layout, fake `claude`, fake machine per daemon) and every test maps to a numbered scenario in a markdown file beside it.

### Confirmed bugs, by severity

**High**

1. **A Job cancelled while its Run is starting is resurrected and then lost.** `queue.Cancel`, `Reorder` and `dispose` share no lock with `run.Service`. `start` holds its own lock across minutes of worktree creation, rebase and setup commands, then does an unconditional `SetJobState(active)` at `internal/run/run.go:530`. A cancel landing in that window is overwritten; the Job then has no queue position, is under-counted by the caps, every end-of-run `DequeueJob` fails and is only logged, and a daemon restart requeues the cancelled Job. Fix: claim the Job with the existing compare-and-set `MoveJobState` right after `scan`, before the slow work.

**Medium**

2. **A stdout line over 8 MiB hangs the Run forever.** The scanner exits on `ErrTooLong` without draining the pipe, so the Agent blocks in `write(2)` and `Wait` never returns (`run.go:1320-1335`). The agent verifier already drains correctly; copy that.
3. **SIGKILL reaches only the Agent's pid on daemon stop and verifier timeout.** The context-cancel path relies on `os/exec.WaitDelay`, which kills the pid, not the group (`internal/executor/host/host.go:81-82`). ADR-0034's last paragraph claims otherwise. A TERM-ignoring grandchild survives shutdown.
4. **Clean shutdown fails while a chat exchange is parked on consent.** `http.Server` has no `BaseContext`, so a stream waiting up to 5 minutes makes `Shutdown` hit its 5 s deadline and the daemon exits non-zero (`internal/daemon/daemon.go:199-202`).
5. **Daemon credentials leak into every Account's Agent.** The environment is `os.Environ()` plus the Account's variables; nothing clears `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or a daemon-level `CLAUDE_CODE_OAUTH_TOKEN` (`host.go:72`, `internal/driver/claudecode/claudecode.go:242-253`). Contradicts ADR-0019's isolation.
6. **The fetch never feeds the rebase.** `FetchBase` updates `refs/remotes/...` while `Rebase` replays onto `refs/heads/...`, so the up-to-two-minute fetch has no effect on the outcome (`internal/git/git.go:753,935`).
7. **YAML is not strict.** No `KnownFields(true)`, and one struct serves both project and global files, so typos and cross-file keys pass silently (`internal/config/config.go:299,496`).
8. **`CommitPath` can hang on a signing key.** It omits `-c commit.gpgsign=false` and runs under `context.Background()` (`git.go:1024`), unlike `Rebase` which explains exactly why that matters unattended.

**Low**: a poisoned head-of-queue Job blocks everything behind it on worktree failure; `gc.prune` prunes the user's own worktrees; the listener and socket are not cleaned up on early start errors; the socket briefly exists at umask permissions; end-of-run `ErrNotQueued` is swallowed.

### Smells

- `internal/run/run.go` (1399 lines, four mutexes, five maps) and `internal/git/git.go` (1268 lines) are the god packages. Both have natural seams already.
- Three copies of a bounded writer, two copies of `resolve`, and a handful of dead exports (`client.Kind*`, `skill.Names`, `command.DeniedError`).
- CI does not run with `-race`, so today's race-free result is not something CI guarantees.
- Testing gaps line up with the bugs: no test for cancel-during-start, an over-long stdout line, a TERM-ignoring grandchild, credential absence in the Agent environment, or hostile repository config.

## 2. Dependency review

### Verdict

Nothing needs to go. Seven direct Go dependencies, all at their latest release; two runtime npm dependencies (react, react-dom). `go mod tidy` is clean, `govulncheck` and `npm audit` report nothing. The `owl` binary is cgo-free and links no Wails code at all, so the heavy desktop dependency is quarantined to the desktop target as ADR-0009 hoped.

| Dependency | Usage | Verdict | Effort |
| --- | --- | --- | --- |
| `connectrpc.com/connect` + `protobuf` | 7 services, 36 RPCs, 2 server streams | keep | - |
| `spf13/cobra` | 39 commands, three levels deep | keep | replace: days for 0.6 MB |
| `modernc.org/sqlite` | store driver | keep | pure Go is the point; +4 MB |
| `wailsapp/wails/v2` | desktop only, one file | keep | cgo for the macOS webview only |
| `gopkg.in/yaml.v3` | config decode, comment-preserving skill edits | **switch import** to `go.yaml.in/yaml/v3` | 30 min |
| `oklog/ulid/v2` | one call site, ordering never relied on | optional remove, `crypto/rand.Text()` | 15 min |

### Notes

- The module graph looks heavy (142 modules) but only 28 reach any build. The rest is the Wails CLI's own dependencies and never links.
- `gopkg.in/yaml.v3` upstream has been archived since May 2022. The maintained fork has the same API and is already in the graph.
- Cheap follow-ups: bump linked `golang.org/x/*` and `samber/lo`; bump `actions/*` majors and actionlint; consider pinning buf's version in `buf-action`; vite 8 and react 19.3 when next touching the frontend.
- Buf plugin pins and the Wails CLI pin match `go.mod` exactly. Good.

## 3. Production readiness

### Verdict

**Usable by the author only.** Nothing has ever been released, the README is one line, and on a fresh install the unattended path does not work.

### Blockers before a first public release

1. **Close the permission gap.** The driver passes `--permission-prompts none` but no `--permission-mode`, no `--allowedTools`, and never seeds a `settings.json` in the Account's `CLAUDE_CONFIG_DIR`. In the default mode every edit prompts, so every edit is denied and the Job lands in review with an empty diff. The fake-claude behaviour suite cannot catch this. Decide who writes the allowlist, implement it, and make `owl jobs show` print the effective permissions.
2. **Make the Homebrew service find `claude`.** The formula's service PATH is the standard Homebrew set, which excludes `~/.local/bin` where the native installer puts `claude`. Every Run under `brew services` fails with "claude is not installed".
3. **Cut `v0.1.0` and prove the pipeline.** The release workflow, tap push, formula and cask have never executed. Install on a clean machine and fix what breaks.
4. **Write the README**: what it does, the risk model (host execution, no sandbox, denied prompts), install, quickstart, a full example config, and where things live on disk. The config keys are currently discoverable only from `internal/config/config.go`.
5. **Add a wall-clock limit per Run** or a default `budgetUSD`. Today every backstop is opt-in, so a wedged Agent on an untouched machine runs until the user sits down.
6. **Add a LICENSE.**

### What is already right

- CI runs build, vet, gofmt, tests, tidy check, `buf lint`, `buf breaking` against `main`, actionlint and a generated-code check. All recent runs green.
- The release workflow is defensive: a guard job refuses tags not on `main`, checksums are verified, the formula is pushed before the cask for the skew reason ADR-0010 gives.
- Forward-only embedded migrations via `PRAGMA user_version`, each in its own transaction. Untested across versions only because no version exists yet.
- Restart reconciliation marks in-flight Runs interrupted and requeues the Job. Stale socket handling is careful. Tokens live in the macOS keychain, files are 0600 in 0700 directories, and secrets are written atomically.
- CLI help text is excellent and is currently the best documentation the project has.

### Nice to have

`--verbose` and JSON logs with a retention policy for per-Run logs; a migration upgrade fixture per released schema; a client/daemon version handshake instead of a bare `Unimplemented`; killing orphaned Agent process groups on restart; notarisation before non-tap distribution; merge the Renovate PR and add `govulncheck` to CI; a systemd unit and idle detector for Linux or a clear statement that Linux is `owl start`-only; file the blockers as issues.

## Recommended order of work

1. Fix the queue race (bug 1) with compare-and-set transitions and claim-before-slow-work. It is the only high-severity finding and it removes a class of "plausible" queue issues too.
2. Close the permission gap and the Homebrew PATH gap (readiness blockers 1 and 2). Without these, nobody but the author gets a working first night.
3. One change for the three process-boundary bugs: drain stdout, group SIGKILL on daemon stop, scrub credentials from the Agent environment. One test each.
4. Give every git call a context and unattended-safe config; decide what the fetch is for.
5. `BaseContext`, listen-after-validate, strict YAML, `DefaultGlobal()` for a missing file.
6. README, LICENSE, a per-Run wall-clock limit, then tag `v0.1.0` and run the release end to end.
7. Housekeeping: yaml import path, `-race` in CI, split `run` and `git`, dedupe helpers, bump actions.
