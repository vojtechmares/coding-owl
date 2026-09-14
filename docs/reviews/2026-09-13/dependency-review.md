# Dependency review - coding-owl

Reviewed on 2026-09-13 against `main` at `701b936`. Module `github.com/vojtechmares/coding-owl`, `go 1.27.1`, toolchain go1.27.1 darwin/arm64.

## Summary

- Seven direct Go dependencies, all at their latest released version. None has a pending update.
- `go mod tidy` is clean. `govulncheck ./...` reports no vulnerabilities. `npm audit` reports none.
- The `owl` binary builds with `CGO_ENABLED=0` (the release script already does this). Only the desktop target needs cgo, and only because of Wails' macOS webview - that is unavoidable and confined.
- Two dependencies could go with modest effort, and one should change its import path:
  - `github.com/oklog/ulid/v2` - one call site, ordering never relied on; `crypto/rand.Text()` (Go 1.24+) does the same job in one line.
  - `github.com/spf13/cobra` - could be replaced by `flag` plus a hand-rolled dispatcher, but the CLI has 39 commands three levels deep, so the effort is days for a 0.6 MB saving. Not recommended.
  - `gopkg.in/yaml.v3` - the code is right, the import path is stale: upstream is archived (last release May 2022). The maintained drop-in fork `go.yaml.in/yaml/v3` is already in the module graph.
- The rest earn their keep: connect and protobuf are the API per ADR-0004, modernc sqlite is the price of a cgo-free binary, Wails is the desktop per ADR-0009 and its weight does not leak into `owl`.

## Verdict table

| Dependency | Usage | Verdict | Effort | Rationale |
|---|---|---|---|---|
| `connectrpc.com/connect` v1.21.0 | 24 files: `internal/daemon` (9), `internal/client` (8), generated `codingowlv1connect` (7). 7 services, 36 RPCs, 2 server-streams | keep | - | ADR-0004 chose it for streaming plus JSON debuggability over a unix socket; `net/rpc` has no streaming and plain HTTP would mean hand-writing 36 handlers and clients plus SSE. Adds ~1.9 MB with protobuf. |
| `google.golang.org/protobuf` v1.36.12 | 15 files: `gen/` (7), `internal/daemon` (6), `tests/behavior` (2) | keep | - | Inseparable from connect. Version matches the `buf.gen.yaml` plugin pin exactly. |
| `github.com/spf13/cobra` v1.10.2 | 9 files, all in `internal/cli`. 39 commands, 27 flag sets, `ExactArgs`/`NoArgs`, `SetOut`/`SetErr`/`SetArgs` in tests | keep | replace: days | Three-level command tree with generated help and typed flags. A `flag`-based rewrite would reimplement most of that to save 0.64 MB and two modules (`pflag`, `mousetrap` - the latter windows-only and not linked here). Last release 2025-12-03; maintained, no update pending. |
| `github.com/oklog/ulid/v2` v2.1.2 | 1 file, 1 call: `internal/queue/queue.go:64` (`Local.Ref`) | remove (optional) | 15 min | The ref only needs uniqueness (ADR-0032); nothing orders by it (queue orders by `position, id`). `crypto/rand.Text()` gives a 26-char base32 128-bit token with no import. Costs 50 KB and zero transitive modules, so it is also harmless to leave. One word in ADR-0032's table would change. |
| `github.com/wailsapp/wails/v2` v2.15.0 | 1 file: `cmd/owl-desktop/main.go` (`//go:build darwin`) | keep | - | ADR-0009. Links 14 third-party modules into the desktop app only; `owl` links none of them (verified with `go list -deps`). Requires cgo for the macOS webview - desktop target only. v3 is still beta (`v3.0.0-beta.20`); stay on v2. |
| `gopkg.in/yaml.v3` v3.0.1 | 3 files: `internal/config/config.go` (struct decoding), `internal/skill/service.go` (2 - `yaml.Node` round-trip that preserves comments) | keep, switch import to `go.yaml.in/yaml/v3` | 30 min | YAML is the user-facing config format (ADR-0014 names `.coding-owl.yaml`), and the skill writer edits one key of a user's file while keeping their comments - `encoding/json` and TOML cannot do that. But `gopkg.in/yaml.v3` has been unmaintained since May 2022 (repo archived); `go.yaml.in/yaml/v3` is the maintained fork with the same API, and `v3.0.4` is already in the module graph via wails. Three import lines and `go mod tidy`. |
| `modernc.org/sqlite` v1.58.0 | 2 files in `internal/store` (driver registration, constraint error codes) | keep | - | Pure Go, so `CGO_ENABLED=0` release builds and cross-compiles work. Largest single cost: ~4 MB of binary and 8 transitive modules (`libc`, `mathutil`, `memory`, `bigfft`, `uuid`, `go-humanize`, `go-strftime`, `x/sys`). `mattn/go-sqlite3` would need cgo; `ncruces/go-sqlite3` (wasm) is comparable in size with no clear gain. |

### Frontend (`cmd/owl-desktop/frontend`)

| Package | Installed | Latest | Verdict | Notes |
|---|---|---|---|---|
| `react`, `react-dom` | 19.2.8 | 19.3.0 | keep | The only runtime deps. `react` imported in 10 of 15 source files. No router, no state library, no UI kit - lean. |
| `typescript` (dev) | 5.9.3 | 7.0.2 | keep on 5.x | TS 7 is the native-port major; `^5.6.3` range holds it back deliberately. Not urgent. |
| `vite` (dev) | 7.3.6 | 8.3.0 | keep, bump when convenient | Major behind. `^7.0.0` range. |
| `@vitejs/plugin-react` (dev) | 5.2.0 | 6.1.1 | bump with vite | Goes with the vite 8 move. |
| `@types/react`, `@types/react-dom` (dev) | 19.2.18, 19.2.5 | 19.3.x | keep | Minor. |

Lockfile holds 119 packages. `npm audit`: 0 vulnerabilities. `wailsjs/runtime/package.json` is generated by the Wails CLI, not a dependency to manage.

One oddity: `npm outdated` printed `react` and `react-dom` as `MISSING` although `node_modules/react` exists. That looks like a stale local `node_modules` rather than a lock problem - a fresh `npm ci` should clear it. CI does a fresh install, so it does not affect releases.

## Usage detail

Import counts (`grep -rln` on `*.go`, 173 hand-written files plus 14 generated):

```
connectrpc.com/connect        24 files  internal/daemon 9, internal/client 8, gen/.../codingowlv1connect 7
google.golang.org/protobuf    15 files  gen/codingowl/v1 7, internal/daemon 6, tests/behavior 2
github.com/spf13/cobra         9 files  internal/cli 9
gopkg.in/yaml.v3               3 files  internal/skill 2, internal/config 1
modernc.org/sqlite             2 files  internal/store 2
github.com/oklog/ulid/v2       1 file   internal/queue 1
github.com/wailsapp/wails/v2   1 file   cmd/owl-desktop 1
```

### connect

`proto/codingowl/v1` defines 7 services and 36 RPCs, two of them server-streaming (`JobService.StreamRunLog`, `ChatService.SendMessage`). The client dials the unix socket through a custom `http.Transport.DialContext` (`internal/client/client.go:72`); the daemon does `net.Listen("unix", ...)` (`internal/daemon/daemon.go:109`). Error codes in use: `InvalidArgument`, `NotFound`, `AlreadyExists`, `Internal`, `Unavailable`, `FailedPrecondition`. This is exactly the shape ADR-0004 describes, and streaming rules out `net/rpc`. Replacing with hand-written HTTP+JSON would re-create the generated code by hand and lose `grpcurl` and breaking-change detection.

### cobra

39 `cobra.Command` values across `cli.go`, `account.go`, `project.go`, `providers.go`, `queue.go`, `run.go`, `skill.go`, `gc.go`, `status.go`, nested three deep (`owl skills add <source>`, `owl daemon run`). Flag types used: `StringVar` (12), `BoolVar` (9), `IntVar` (2), `StringArrayVar`, `BoolVarP`, plus `Flags().Changed`. Tests drive it through `SetArgs`/`SetOut`/`SetErr`/`ExecuteContext`. No completion generation, no docs generation, so `go-md2man` is in the graph but never linked.

### ulid

```go
// internal/queue/queue.go:64
func (Local) Ref() (string, error) { return ulid.Make().String(), nil }
```

Every `ORDER BY` in `internal/store` uses `position`, `id`, `name`, `rowid` or `updated_unix`; none uses `ref`. The time-prefix of a ulid is therefore only cosmetic. A drop-in with no dependency:

```go
func (Local) Ref() (string, error) { return rand.Text(), nil } // crypto/rand, Go 1.24+
```

### yaml.v3

`internal/config` decodes `.coding-owl.yaml` and the global `config.yaml` into tagged structs (about 30 tagged fields, some `any` for the polymorphic `maxParallel` values). `internal/skill/service.go` does something the standard library cannot: it parses the user's file into a `yaml.Node` tree, replaces one key (`skills`), and renders it back with comments intact, working around yaml.v3 dropping blank lines with a marker comment. Changing the config format is a product decision (ADR-0014), not a dependency one.

`gopkg.in/yaml.v3`'s last release is v3.0.1 (2022-05-27) and the upstream repo is archived. The YAML org's maintained fork `go.yaml.in/yaml/v3` keeps the same package API; `v3.0.4` is already in this module graph (pulled by wails) and `v3.0.5` is available. Switching is a three-line import change.

### wails

The desktop app under its real build tags (`-tags desktop,production`) links these third-party modules: `wails/v2`, `samber/lo`, `tkrajina/go-reflector`, `wailsapp/mimetype`, `pkg/browser`, `pkg/errors`, `rivo/uniseg`, `leaanthony/{go-ansi-parser,slicer,u}`, `golang.org/x/{net,text}`, plus `connect` and `protobuf` from `internal/client`. Cgo packages: `runtime/cgo`, `wails/v2/pkg/assetserver/webview`, `wails/v2/internal/frontend/desktop/darwin`. `CGO_ENABLED=0` with those tags fails to compile, as expected.

Modules that sit in `go.mod` as `// indirect` but are linked by no build on this platform: `labstack/echo/v4`, `bep/debounce`, `go-ole`, `mousetrap`, `go-colorable`, `bytebufferpool`, `go-webview2`, `x/crypto`. Echo is Wails' dev server, webview2 and go-ole are Windows. They are there because module pruning lists them for other GOOS values, and they cost nothing at link time.

The wider noise: `go list -m all` reports 142 modules and `go mod graph` 283 edges, but only 28 modules reach any build or test. The other 114 come from the Wails module's own `go.mod`, which carries its CLI's dependencies (charmbracelet, go-git, pterm, ...). That is what makes `go list -m -u all` print 63 updatable modules of which none is direct. ADR-0009 anticipated Wails being heavy and kept an embedded HTTP dashboard as the fallback; on the evidence here the weight is real but quarantined to the desktop target, so the fallback is not needed.

### modernc sqlite

Pure Go translation of SQLite. The `owl` binary has no cgo packages at all (verified with `go list -deps -f '{{.CgoFiles}}'`), and `scripts/build-release.sh:44` builds with `CGO_ENABLED=0 -trimpath -ldflags "-s -w"`.

## Version freshness

Direct dependencies (`go list -m -u -json`): no updates available for any of the seven.

| Module | Version | Released |
|---|---|---|
| connectrpc.com/connect | v1.21.0 | 2026-09-08 |
| github.com/oklog/ulid/v2 | v2.1.2 | 2026-07-23 |
| github.com/spf13/cobra | v1.10.2 | 2025-12-03 |
| github.com/wailsapp/wails/v2 | v2.15.0 | 2026-08-17 |
| google.golang.org/protobuf | v1.36.12 | 2026-08-10 |
| gopkg.in/yaml.v3 | v3.0.1 | 2022-05-27 (upstream archived) |
| modernc.org/sqlite | v1.58.0 | 2026-09-01 |

Indirect updates worth taking because they are linked into the desktop app: `golang.org/x/net` v0.56.0 -> v0.59.0, `golang.org/x/text` v0.39.0 -> v0.42.0, `golang.org/x/sys` v0.47.0 -> v0.48.0 (linked into `owl` via modernc), `samber/lo` v1.49.1 -> v1.53.0. `go get -u golang.org/x/net golang.org/x/text golang.org/x/sys golang.org/x/crypto github.com/samber/lo && go mod tidy` is safe. `github.com/golang/protobuf` v1.5.0 appears in the graph as deprecated but is neither required nor linked.

Unmaintained: only `gopkg.in/yaml.v3` (see above). `pkg/errors` (linked into the desktop app via Wails) is frozen too, but that is Wails' choice, not this project's.

## Security

- `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` (v1.8.0): `No vulnerabilities found.`
- `npm audit` in `cmd/owl-desktop/frontend`: `found 0 vulnerabilities`.

## Transitive weight and binary size

| Measure | Value |
|---|---|
| `go mod graph` edges | 283 |
| `go list -m all` modules | 142 |
| `go.mod` require entries | 43 (7 direct, 36 indirect) |
| Modules linked by any build or test | 28 |
| `go.sum` lines | 151 |
| `owl`, default build (`CGO_ENABLED=0`) | 31.7 MB |
| `owl`, release build (`-trimpath -s -w`) | 21.3 MB |
| `owl`, release build gzipped | 7.2 MB |
| `owl-desktop` binary in the last `wails build` (`build/bin/owl-desktop.app`) | 15.1 MB (app bundle 15 MB) |

Marginal binary cost per dependency, measured by building a tiny `main` with and without each import (`-trimpath -s -w`, `CGO_ENABLED=0`). Base main imports `net/http`, `database/sql`, `encoding/json`, `fmt`, `os`:

| Import | Binary | Marginal |
|---|---|---|
| base | 3.5 MB | - |
| modernc.org/sqlite | 7.5 MB | +4.0 MB |
| connect + protobuf timestamppb | 5.4 MB | +1.9 MB |
| spf13/cobra | 4.1 MB | +0.64 MB |
| gopkg.in/yaml.v3 | 4.1 MB | +0.58 MB |
| oklog/ulid/v2 | 3.5 MB | +0.05 MB |

Cgo: `owl` needs none. `owl-desktop` needs it for the Wails macOS webview and nothing else; the frontend and `internal/desktop` are cgo-free.

## Tidy

`go mod tidy` produced no diff in `go.mod` or `go.sum` (restored with `git checkout` afterwards). CI also enforces this with `go mod tidy -diff`.

## Tooling and CI dependencies

| Tool | Pinned | Latest | Note |
|---|---|---|---|
| buf | unpinned via `bufbuild/buf-action@v1` (`setup_only`) | buf v1.73.0, action v1.5.0 | `buf lint`, `buf generate`, `buf breaking` in CI. `buf.yaml` v2, `STANDARD` lint, `FILE` breaking. Consider pinning `version:` in the action for reproducible `buf generate` checks. |
| buf remote plugins | `protocolbuffers/go:v1.36.12`, `connectrpc/go:v1.21.0` | same | Match `go.mod` exactly. Good - keep them moving together. |
| Wails CLI | `go install ...wails/v2/cmd/wails@v2.15.0` (CI, `scripts/build-desktop-release.sh`) | v2.15.0 | Matches the library version. |
| actionlint | `@v1.7.7` via `go run` | v1.7.12 | Harmless lag. |
| svu | unpinned (`brew install caarlos0/tap/svu`), local release script only | - | Not in CI. |
| Node | 24 (`setup-node`) | - | Frontend build in the desktop and release jobs. |
| `actions/checkout` | v4 | v7.0.1 | |
| `actions/setup-go` | v5 | v7.0.0 | |
| `actions/setup-node` | v4 | v7.0.0 | |
| `actions/upload-artifact` | v4 | v7.0.1 | |
| `actions/download-artifact` | v4 | v8.0.1 | Upload and download must move together. |

Actions are referenced by major tag, not SHA. That is the usual trade-off; the release workflow pushes to another repo with a PAT, so SHA pinning there would be the higher-value hardening if wanted.

## Recommended actions, in order of value

1. Switch `gopkg.in/yaml.v3` to `go.yaml.in/yaml/v3` (three imports, `go mod tidy`). Moves the one unmaintained direct dependency to its maintained fork with no API change.
2. Bump the linked indirect modules: `golang.org/x/{net,text,sys,crypto}`, `github.com/samber/lo`.
3. Optionally drop `oklog/ulid/v2` for `crypto/rand.Text()` and update the one table row in ADR-0032.
4. When touching the frontend next: vite 8 with `@vitejs/plugin-react` 6, react 19.3.
5. Bump `actions/*` majors and `actionlint` at leisure; consider pinning `buf`'s version in `buf-action`.

Not recommended: replacing cobra, connect, protobuf, modernc sqlite or Wails. Each is either an ADR-level decision or would cost days for under a megabyte.
