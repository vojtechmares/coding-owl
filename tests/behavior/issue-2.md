# Issue #2: Scaffold: one owl binary, daemon on a unix socket, owl daemon status over ConnectRPC

All scenarios drive the built `owl` binary from the outside, or the daemon
in-process through the shared client package. "XDG layout" means a temporary
directory tree with `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and
`HOME` pointing into it and `XDG_RUNTIME_DIR` unset unless a scenario says
otherwise.

## Scenarios

### S1 - daemon run listens on the socket in the foreground
Given a temporary XDG layout and no daemon running
When `owl daemon run` is started
Then within five seconds a unix socket exists at `$XDG_STATE_HOME/coding-owl/owld.sock`
And the process that was started is itself the listening daemon (its log line reports the same pid the parent spawned, so it did not fork or detach)
And the log written to stdout/stderr contains the socket path
And no file named `*.pid` exists anywhere under the XDG layout

### S2 - daemon status reports version, uptime and socket path
Given a daemon started as in S1 from a binary built with version `1.2.3-test`
When `owl daemon status` runs in a second process with the same XDG layout
Then it exits 0
And stdout contains a `version: 1.2.3-test` line
And stdout contains an `uptime:` line whose value parses as a duration
And stdout contains a `socket:` line naming `$XDG_STATE_HOME/coding-owl/owld.sock`

### S3 - daemon status with no daemon fails clearly
Given a temporary XDG layout and no daemon running
When `owl daemon status` runs
Then it exits with a non-zero code
And stderr says the daemon is not running and names the socket path it tried
And stdout is empty

### S4 - socket path prefers XDG_RUNTIME_DIR
Given a temporary XDG layout with `XDG_RUNTIME_DIR` also set to a temporary directory
When `owl daemon run` is started
Then the socket is created at `$XDG_RUNTIME_DIR/coding-owl/owld.sock`
And no socket is created under `$XDG_STATE_HOME`
And `owl daemon status` in the same environment reports that socket path

### S5 - socket path falls back to the home state directory
Given `HOME` set to a temporary directory and `XDG_STATE_HOME` and `XDG_RUNTIME_DIR` unset
When `owl daemon run` is started
Then the socket is created at `$HOME/.local/state/coding-owl/owld.sock`

### S6 - over-long socket path is refused
Given `XDG_STATE_HOME` set to a directory whose resulting socket path exceeds 104 bytes
When `owl daemon run` is started
Then it exits with a non-zero code
And stderr says the socket path is too long and prints the path
And no socket file is created

### S7 - config, data and state directories resolve per ADR-0014
Given a temporary XDG layout
When `owl daemon run` is started
Then its startup log names the config directory `$XDG_CONFIG_HOME/coding-owl`, the data directory `$XDG_DATA_HOME/coding-owl` and the state directory `$XDG_STATE_HOME/coding-owl`
And when those variables are unset the same log names `$HOME/.config/coding-owl`, `$HOME/.local/share/coding-owl` and `$HOME/.local/state/coding-owl`

### S8 - status RPC through the shared client, daemon in-process
Given a daemon started in-process on a temporary socket under a temporary XDG layout
When the shared client package is asked for the daemon status
Then it returns the daemon's version, a non-negative uptime and the socket path the daemon is listening on
And when the daemon is stopped the same call returns an error that identifies the daemon as not running

### S9 - SIGTERM stops the daemon cleanly
Given a daemon started as in S1
When it receives SIGTERM
Then it exits 0 within five seconds
And the socket file is removed

### S10 - a stale socket file is replaced
Given a temporary XDG layout with a leftover `owld.sock` file that nothing is listening on
When `owl daemon run` is started
Then it listens on that path and `owl daemon status` succeeds

### S11 - a second daemon on the same socket is refused
Given a daemon started as in S1
When a second `owl daemon run` is started with the same XDG layout
Then the second process exits with a non-zero code
And its stderr says a daemon is already listening on that socket
And the first daemon keeps answering `owl daemon status`

### S12 - the CLI reaches the daemon only through the shared client
Given the repository source
When the import graph of the CLI package is listed
Then it imports the shared client package
And it imports no generated protobuf or Connect package directly

### S13 - CI runs tests, buf lint and buf breaking on every push
Given the repository source
When `.github/workflows/ci.yml` is read
Then it triggers on push
And it runs `go test`, `buf lint` and `buf breaking` against the main branch
