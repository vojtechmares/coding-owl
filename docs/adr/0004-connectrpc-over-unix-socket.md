# ADR-0004: ConnectRPC over a unix socket

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

Three consumers need to talk to the daemon: the CLI, the desktop app, and
eventually a remote Runner. The API needs to be type-safe across a Go server and
Go clients, and it needs **streaming** - tailing a running job's output is a
core interaction, not an extra.

Two axes were initially conflated and are worth separating: the *protocol*
(gRPC vs REST) and the *transport* (unix socket vs TCP). They are independent -
gRPC runs over a unix socket perfectly well.

There is also an explicit non-technical force: the author wants deeper hands-on
gRPC experience, and this is a sound use case for it rather than a contrived
one.

## Decision

**ConnectRPC** (connectrpc.com) with **Buf** for code generation, linting, and
breaking-change detection. Service definitions live in `proto/`. The daemon
listens on a unix socket under the state directory -
`~/.local/state/coding-owl/owld.sock`, or `$XDG_RUNTIME_DIR/coding-owl/owld.sock`
where that is set (see ADR-0014).

All client-side logic lives in one shared `internal/client` package, so the CLI
and the GUI can never diverge in how they talk to the daemon.

TCP transport with OIDC authentication is explicitly deferred, along with
remote daemons generally.

## Consequences

- A single connect-go handler serves the Connect, gRPC, and gRPC-Web protocols
  at once. `grpcurl` works against the socket, and a unary call is also just an
  HTTP POST with a JSON body - so type safety no longer costs debuggability,
  which was the one real argument for REST.
- connect-go is built on `net/http`, so it binds to a unix socket listener with
  no h2c setup. Server-streaming - the log tail - works over HTTP/1.1; only
  full bidirectional streaming would need HTTP/2. Plain grpc-go mandates HTTP/2
  everywhere.
- Buf's breaking-change detection guards one direction of skew and not the
  other. `breaking: use: [FILE]` forbids removing or renumbering what is
  already there, so a client that has not been upgraded keeps working against a
  newer daemon. It permits additions, so a newer client calling something an
  older daemon never had is not covered - it gets Unimplemented. The daemon is
  therefore the half to upgrade first, which is what ADR-0010's lockstep
  release is for.
- `buf` becomes a required tool in the build and in CI.
- Adding TCP later is a listener swap; adding auth is an interceptor. Neither
  touches service definitions.

## Alternatives considered

**Plain grpc-go.** Loses the JSON/curl debugging path and the HTTP/1.1
streaming fallback, and mandates HTTP/2 for no gain on a local socket.

**REST with OpenAPI.** Rejected. It drags in a code-generation toolchain the
project does not otherwise want, and streaming has to be bolted on with SSE or
websockets.
