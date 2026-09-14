# Coding Owl - production readiness review

Reviewed at commit `701b936` on `main`, 2026-09-13. Repository:
`vojtechmares/coding-owl` (private). The whole history spans four days
(2026-09-09 to 2026-09-12, 199 commits), and there is no git tag, no GitHub
release, and no `coding-owl` formula or cask in the tap yet.

## Verdict

**Usable by the author only.** The engineering underneath is unusually careful
for a four-day-old project - 36.5k lines of Go, 77 test files, a behavioural
suite driven by a fake Claude binary, a release workflow that is more defensive
than most - but nothing has ever been released, the README is one line, and the
unattended path has a gap that would stop a stranger's first night cold: Owl
denies every permission prompt and never grants any tool, so a real Claude
Code run cannot edit a file unless the user hand-writes an undocumented
`settings.json`. Until that is closed and a first tagged release has actually
gone through the workflow, it cannot be called ready for early adopters.

## 1. Build and release

### What exists

- `Makefile` (`build`, `desktop`, `test`, `lint`, `generate`); version is
  stamped from `git describe` into `internal/version.Version`
  (`Makefile:1-7`).
- CI: `.github/workflows/ci.yml` runs on every push: `go build`, `go vet`,
  `gofmt`, `go test ./...`, `go mod tidy -diff`, `buf lint`, actionlint over
  the workflows, "generated code is current" (`buf generate && git diff`), and
  `buf breaking` against `origin/main` (`ci.yml:25-61`). A second macOS job
  builds the Wails app and runs two behavioural pins (`ci.yml:63-85`). Recent
  runs are green (`gh run list`: last 8 all `success`, ~3 min each).
- Release: `.github/workflows/release.yml` on `v*` tags. A `guard` job refuses a
  tag not on `main` (`release.yml:22-44`); `desktop` builds the app on macOS;
  `release` cross-compiles `owl` for darwin/arm64 with `CGO_ENABLED=0 -trimpath
  -ldflags "-s -w"` (`scripts/build-release.sh:44-45`), verifies the desktop
  zip's checksum against the bytes it received, merges one `checksums.txt`,
  generates notes, and publishes with `gh release create`. A `tap` job then
  renders and pushes the formula and cask (`scripts/bump-formula.sh`,
  `scripts/bump-cask.sh`), formula first for the skew reason ADR-0010 gives
  (`release.yml:267-274`), serialised by a concurrency group.
- Tagging: `scripts/release.sh` (svu-derived or explicit version, dirty-tree
  and branch checks, pushes an annotated tag). ZeroVer, no prereleases
  (`release.sh:10`).

### Assessment

- Reproducibility: good on paper - `-trimpath`, pinned Go via `go.mod`, pinned
  Wails CLI `v2.15.0` in both workflows, `CGO_ENABLED=0` (pure-Go
  `modernc.org/sqlite`). Not verified: nobody has run the release workflow.
  `git tag` is empty and `gh release list` is empty.
- The Homebrew tap (`vojtechmares/homebrew-tap`) currently holds `statica`,
  `caffeinum` and `reviewdeck` only. `bump-formula.sh` and `bump-cask.sh`
  create the files on first run, so this is by design, but it means the
  install path in ADR-0010 is untested end to end.
- `HOMEBREW_TAP_TOKEN` is checked for presence up front (`release.yml:295-303`)
  - good - but whether the secret is set in the repo could not be verified
  from here.
- Code signing: the app is **ad-hoc signed and not notarised**
  (`build-desktop-release.sh:9-12`, `codesign --sign -` at line 77). The cask
  strips `com.apple.quarantine` in a `postflight` step with
  `must_succeed: false` (`bump-cask.sh:119-123`). This is an accepted trade in
  ADR-0010 and follows the maintainer's existing Reviewdeck cask. It is fine for
  a personal tap; it is not fine for a general public release, where a
  Gatekeeper prompt on first launch of an unnotarised app is a support ticket.
- Platform: darwin/arm64 only, hard-coded in both build scripts and the
  formula (`depends_on arch: :arm64`). Linux is compiled (`GOOS` builds) but
  has no idle detector (`internal/idle/system/other.go:23-25` returns an error
  for every read), no service unit (`deploy/` holds only a launchd plist;
  ADR-0002 promises systemd), and `owl daemon install` refuses on non-darwin
  (`internal/cli/cli.go:201-203`).
- Dependency hygiene: the Renovate onboarding PR #25 is still open and
  `renovate.json` does not exist; no Dependabot; `govulncheck` is not in CI.
- No `LICENSE`, `CHANGELOG` or `SECURITY.md` at the repo root.

## 2. Installation and upgrade

### Install paths

- Homebrew (intended): `brew install vojtechmares/tap/coding-owl && brew
  services start coding-owl`, then `brew install --cask
  vojtechmares/tap/coding-owl-desktop`. Neither artifact exists yet.
- Manual: `owl daemon install` writes `~/Library/LaunchAgents/
  dev.codingowl.owld.plist` and `launchctl bootstrap`s it, carrying `PATH` and
  the XDG variables from the installing shell (`internal/launchd/launchd.go:47-53`,
  `104-137`). The template is `deploy/launchd/dev.codingowl.owld.plist` with
  `KeepAlive` and `RunAtLoad`. Re-running replaces the agent, which is the
  documented upgrade mechanism (`cli.go:189-199`).

### A concrete first-run problem with the Homebrew path

The formula's service block sets `environment_variables PATH:
std_service_path_env` (`bump-formula.sh:118`), i.e.
`/opt/homebrew/bin:/opt/homebrew/sbin:/usr/bin:/bin:/usr/sbin:/sbin`. Claude
Code's native installer puts `claude` in `~/.local/bin` (on this machine:
`/Users/vojta/.local/bin/claude -> ~/.local/share/claude/versions/2.1.270`).
The driver finds the binary with `exec.LookPath("claude")`
(`internal/driver/claudecode/claudecode.go:257-263`), so a daemon started by
`brew services` will fail every Run with "claude is not installed, or not on
the daemon's PATH". The launchd path avoids this only because it copies the
user's `PATH`. Nothing documents this, and there is no `claudePath` setting.

### Filesystem layout

Implemented as ADR-0014 says, XDG-honouring on both platforms
(`internal/xdg/xdg.go:41-70`):

```
~/.config/coding-owl/config.yaml                 daemon config
~/.local/share/coding-owl/owl.db                 SQLite (0600, dir 0700)
~/.local/share/coding-owl/worktrees/<job-id>/
~/.local/share/coding-owl/accounts/<name>/       CLAUDE_CONFIG_DIR per Account (0700)
~/.local/share/coding-owl/skills/, worktree-config/
~/.local/state/coding-owl/owld.sock (0600)       or $XDG_RUNTIME_DIR/coding-owl/
~/.local/state/coding-owl/logs/<run-id>.jsonl
~/.local/state/coding-owl/daemon.log             (launchd install only)
```

Socket path length is checked against 104 bytes (`xdg.go:74-79`).

### Schema migrations

- Mechanism: embedded `internal/store/migrations/0001..0011_*.sql`, applied in
  filename order inside a transaction each, progress in `PRAGMA user_version`
  (`internal/store/store.go:76-124`). WAL mode, `busy_timeout`, `foreign_keys`
  on, single connection.
- Forward-only, no down migrations, no backup before migrating. A migration
  that fails mid-way rolls back that one file and the daemon refuses to start
  (`daemon.go:95-98`).
- Tested only for "fresh database applies all, second open applies none"
  (`store_test.go:25-60`). There is no test that opens a database created by
  an older schema and upgrades it, because there has never been an older
  release. Acceptable pre-1.0, but the first real upgrade will be the first
  test.
- Config files carry `apiVersion: codingowl.dev/v1` and anything else is
  refused (`internal/config/config.go:20-22`).

### Socket protocol compatibility

- ConnectRPC over the unix socket; `buf breaking` with `FILE` rules runs in CI
  against `main` (`buf.yaml`, `ci.yml:55-61`). That guards an older client
  against a newer daemon. The reverse (newer app calling an RPC an older
  daemon lacks) is acknowledged as uncovered in ADR-0004 and handled by
  release ordering only. The client does not compare its version with the
  daemon's beyond reporting it in `owl daemon status` and the desktop status
  bar (`internal/client/client.go:92`, `internal/desktop/desktop.go:131`); a
  mismatch produces a bare `Unimplemented` rather than "please upgrade the
  daemon".
- No authentication on the socket; mode 0600 is the whole access control
  (`daemon.go:113-118`). Correct for a single-user local daemon.

## 3. Operability

### Daemon lifecycle

- `owl daemon run` is foreground-only, no fork, no PID file (ADR-0002;
  `cli.go:135-159`). SIGINT/SIGTERM trigger graceful shutdown; a second
  signal is fatal by design.
- Stale socket handling is careful: it dials first, only removes on
  `ECONNREFUSED`, and refuses to touch anything that is not a socket
  (`daemon.go:320-343`).
- Shutdown order is deliberate: stop the idle watcher, end Runs (SIGTERM then
  SIGKILL after 5 s), then `http.Server.Shutdown` with a 5 s deadline
  (`daemon.go:258-278`).
- `git >= 2.38` is checked at startup, before listening (`daemon.go:104-107`,
  `internal/git/version.go:14`). Claude Code `>= 2.1.0, < 3.0.0` is checked
  before every Run (`claudecode.go:43-46`, `150-166`).

### Logging

- `log/slog` text handler to stderr, default level, no flags
  (`cli.go:155`). ADR-0002 advertises `owl daemon run --verbose`; that flag
  does not exist (`owl daemon run --verbose` -> `unknown flag`).
- No structured JSON option, no level control, no rotation. The launchd
  template and the formula both append to a single file forever
  (`dev.codingowl.owld.plist:18-21`, `bump-formula.sh:116-117`). Per-Run
  agent streams are kept as `logs/<run-id>.jsonl` with no retention policy.
  An always-on daemon with one 10 s idle poll per tick will grow that file
  slowly, but a chatty night of `stream-json` per Run will not be small.

### Observability

- `owl daemon status` (version, uptime, socket), `owl status` (the "morning
  question": review queue, blocked with reasons, unfinished work, which
  concurrency cap is binding via `CappedError`, `run.go:411-415`), `owl
  jobs show` (effective system prompt), `owl logs -f`, `owl gc`. Help text
  for all of these is unusually good (see section 5).
- No health endpoint beyond `GetStatus`, no metrics. Adequate for a local
  daemon.

### Crash recovery

- On start, `runs.Recover` marks every Run still "in progress" as
  `interrupted` with reason "the daemon stopped before this run ended" and
  requeues the Job (`internal/run/run.go:366-376`), exactly as ADR-0011 says.
- Agents are children in their own process group (`host.go:60`); a daemon that
  dies hard (SIGKILL, power loss) orphans them. GC then reports such a Job as
  `abandoned` rather than deleting anything (`internal/gc/gc.go:50`,
  `481-512`). There is no attempt to find and kill orphaned `claude`
  processes on restart; they run until they exit on their own.
- Process-group signalling is guarded against pid reuse after reap
  (`host.go:112-115`, `142-144`).

### Disk hygiene

- GC runs at start and on an interval (`daemon.go:213-229`; default from
  `gc.DefaultInterval`, configurable via `garbageCollection.interval`). It
  reclaims worktrees of done/cancelled/gone Jobs, `git worktree prune`s, treats
  a merged branch as accept, and reports rather than deletes anything with
  uncommitted changes (`owl gc --help`, `gc.go`). `owl jobs accept --force`
  is the only override. Well aligned with ADR-0015.
- Not collected: Run logs, `skills/<digest>` caches, Account config dirs of
  removed Accounts (not verified either way for the last one).

## 4. Safety of unattended execution

### What stops a runaway agent

| Control | Present | Evidence |
| --- | --- | --- |
| Deny all permission prompts | yes | `--permission-prompts none`, `claudecode.go:186`; flag confirmed on Claude Code 2.1.270 |
| Spend cap per Run | opt-in only | `--max-budget-usd` only when `budgetUSD > 0` in Project config (`claudecode.go:191-193`); no default |
| Attempt limit (TTL) | yes, default 10 | migration `0006_ttl.sql`, `owl add --ttl`, `owl jobs extend` |
| Account utilization ceiling | opt-in | `accounts.<name>.limits.fiveHourMax/weeklyMax`; Run is ended mid-flight when crossed (`internal/run/ceiling.go:150-152`) |
| Concurrency caps | yes, default 1/1/unlimited | `config.go:206-214`, ADR-0021 |
| Freeze on user return, SIGTERM after grace, SIGKILL 5 s later | yes | ADR-0011/0034, `host.go:28,63-64`, `internal/run/interrupt.go` |
| Wall-clock limit per Run | **no** | only `setupTimeout` (15 min) and verifier timeouts exist (`run.go:64-71`); an Agent that hangs on a machine nobody touches runs until the user returns |
| Verification gates completion, no retry | yes | ADR-0013, `internal/verifier/command` |

The missing wall-clock cap matters because the other backstops are all opt-in
or conditional: with no `budgetUSD`, no ceiling configured and an idle machine,
the only thing that ends a wedged Run is the user sitting down.

### The permission gap

`--permission-prompts none` means "anything that would prompt is denied; the
permission mode still decides everything else" (Claude Code's own help text).
In the default permission mode every file edit and most Bash calls prompt.
Owl passes no `--permission-mode`, no `--allowedTools`, and writes no
`settings.json` into the Account's `CLAUDE_CONFIG_DIR` - `grep -rn
"settings.json\|allowedTools\|permission-mode" internal/` finds nothing
outside the chat allowlist. ADR-0012 lists those flags as "available per
Run" and ADR-0006 says "prefer Claude Code's own allowlist settings over
blanket permission skipping", but neither the code nor any document says who
writes the allowlist or what it should contain. The behavioural suite cannot
catch this because `tests/behavior/fakeclaude` accepts any flags.

Consequence: on a fresh install, following only what the repo says, a Run
will start, the Agent will be refused every write, and the Job will land in
`blocked` or `review` with an empty diff. Either Owl needs to seed the Account
directory with a sane default `settings.json` (allow `Edit`, `Write`, `Bash`
with a curated list, deny the rest), or the README must tell the user to do it
and `owl account add` must check that it has been done.

### Secrets

- Account tokens (`claude setup-token` output) go to the macOS keychain via
  `/usr/bin/security` by absolute path (`credential/keychain_darwin.go:15`),
  service `coding-owl`; `owl.db` stores only a reference. Elsewhere a
  `credentials.json` at mode 0600 in a 0700 directory, written atomically
  (`credential/file.go:16, 107-133`).
- The keychain `Set` passes the secret as a `security` argument, visible in
  `ps` to the same user for the call's duration; the code says so and accepts
  it (`keychain_darwin.go:58-61`).
- At Run time the token is handed to the Agent as `CLAUDE_CODE_OAUTH_TOKEN`
  in its environment (`claudecode.go:33, 249-252`). Same-user processes can
  read it from `ps -E`; on a single-user laptop that is the same trust
  boundary as the keychain item itself.
- Chat provider keys (Anthropic/OpenRouter) follow the same path.

### Host execution blast radius

ADR-0006 states plainly: "There is no sandbox. An unattended agent runs with
the user's full filesystem and network access ... This is understood and
accepted." The worktree is a working-directory bound only
(`host.go:42-52` refuses an empty `Dir`). The Agent inherits the daemon's full
environment (`host.go:54`). Mitigations that are real: config and checks are
read from the base branch, never the worktree (ADR-0014); `security` and
`ioreg`/`pmset` are invoked by absolute path so an Agent cannot shadow them
(`keychain_darwin.go:12-15`, `idle/system/darwin.go:42`); prompts are
denied, not auto-approved.

This is an accepted, documented risk. It is documented in an ADR, not in
anything a user installing from Homebrew would read.

## 5. Documentation and user-facing readiness

- `README.md` is 13 bytes: the title. No install, no quickstart, no config
  reference, no description of what the thing does. `CONTEXT.md` (glossary)
  and 34 ADRs are excellent for a contributor and useless as a manual.
- No example `config.yaml` or `.coding-owl.yaml` anywhere; the keys
  (`maxParallelRuns`, `graceWindow`, `idle.after/requirePower/interval`,
  `credentialStore`, `garbageCollection.interval/reviewAfter`,
  `accounts.<name>.maxParallel/limits.fiveHourMax/weeklyMax`, `phases`,
  `checks`, `setup`, `skills`, `budgetUSD`, `unattendedClauses`, `account`,
  `verification`) are discoverable only from `internal/config/config.go:216-293`.
- `docs/desktop/` is ten screenshots with no text.
- CLI help is the best documentation the project has. Every command's `Long`
  text explains the concept, the default and the neighbouring verb, in plain
  prose (`owl add`, `owl pause`, `owl gc`, `owl jobs accept` are good
  examples). Built to the scratchpad and exercised: `owl --help` lists 16
  commands; nested help all renders.
- Error messages: consistent `owl: <sentence>` on stderr, exit 1. "daemon not
  running at <sock>: dial unix ...: connect: invalid argument" leaks the dial
  error; `owl daemon status` checks the socket path length first but `owl
  status` / `owl add` do not, so an over-long path reports "daemon not
  running" (tested with a long `XDG_STATE_HOME`). Cosmetic.
- `owl --version` prints `owl version 701b936` from the Makefile stamp.

### Test suite

`go test ./...` run locally at `701b936`: every package `ok`, exit 0;
`tests/behavior` takes 248 s and drives the daemon through a fake `claude`
binary. CI has been green on every recent push. The suite is thorough about
Owl's own mechanics and, by construction, silent about whether the real
Claude Code does anything useful with the flags it is given.

## 6. Open issues and PRs

- `gh issue list --state open`: two issues. #1 is the MVP PRD (99 user
  stories, `ready-for-human`); #36 is a design review of the desktop theme
  (`ready-for-human`). Everything agent-workable has been closed - the
  `ready-for-agent` queue is empty.
- `gh pr list --state open`: only #25, Renovate onboarding.
- Nothing in the tracker says "not ready"; nothing says "ready" either. There
  is no release-tracking issue, and none of the blockers below is filed.

## Blockers before a first public release

1. **Close the permission gap.** Decide and implement how an unattended Agent
   is allowed to edit and run anything: seed `accounts/<name>/settings.json`
   with a default allowlist, or pass `--permission-mode acceptEdits` plus an
   `--allowedTools` list from Project config, and make `owl jobs show` print
   the effective permissions. Add a behavioural test that asserts the flags
   or the file. Without this the product does not do what the PRD says.
2. **Make the Homebrew service find `claude`.** Either resolve the binary at
   install time and store its path, add a `claudePath` setting, or extend the
   service `PATH` with `~/.local/bin` and document the limitation.
3. **Cut a real release.** Tag `v0.1.0`, let `release.yml` run, confirm the
   formula and cask land in the tap, `brew install` on a clean machine, and
   fix whatever breaks. The workflow has never executed.
4. **Write the README**: what it does, the risk model (host execution, no
   sandbox, denied prompts), install (brew and manual), quickstart (account
   add, project add, add, start, status, accept), a config reference with a
   full example file, and where things live on disk.
5. **A wall-clock limit per Run**, or at minimum a default `budgetUSD`, so
   that a wedged Agent on an untouched machine ends without a human.
6. **Add a LICENSE** and decide whether the repository goes public with it.

## Nice to have

- `--verbose` / `--log-format json` on `daemon run` (ADR-0002 already
  promises the first), and a retention policy for `logs/<run-id>.jsonl`.
- Upgrade-path test for migrations: keep a fixture database per released
  schema and assert `Open` upgrades it.
- Client-side version handshake: turn `Unimplemented` into "the daemon is
  older than this client; upgrade it".
- Kill orphaned Agent process groups on daemon restart instead of only
  reporting the Job as abandoned.
- Notarise the desktop app before any general (non-tap) distribution.
- Merge or close the Renovate PR; add `govulncheck` to CI.
- Linux: a systemd unit template in `deploy/` and an idle detector, or state
  clearly that Linux is `owl start`-only.
- `owl status` / `owl add` should fail on socket path length the way
  `owl daemon status` does, and hide the raw dial error behind "start it with
  brew services start coding-owl".
- Turn `docs/desktop/*.jpg` into a short page and file issues for the
  blockers above so the tracker reflects readiness.
