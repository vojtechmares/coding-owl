# Issue #61: Socket permission window and runtime directory never re-tightened

Only the owning user may connect to the daemon's unix socket; there is no
authentication on the handler (ADR-0004). Reported at commit `701b936`: the
socket was created at whatever the process umask allowed and only restricted to
`0600` afterwards, so for a moment it could be more permissive than that; and
the directory the socket lives in - `$XDG_RUNTIME_DIR/coding-owl`, or the state
directory - was created `0700` when missing but trusted as found when it
already existed.

The fix is that the socket is never more permissive than `0600` - it is created
under a umask of `0077`, restored once it exists - and that the socket's
directory is `0700` on every start, whether or not it already existed.
Authentication on the socket is out of scope.

The daemon scenarios drive `daemon.Run` directly, as the existing daemon tests
do. The end-to-end scenario drives the built `owl` binary.

## Scenarios

### S1 - the socket is 0600 and its directory 0700 under a permissive umask
Given a temporary XDG layout, no socket directory yet, and a process umask of `022`
When the daemon is run
Then the socket's mode is `0600`
And the socket directory's mode is `0700`
And once the daemon has stopped the process umask is `022` again

### S2 - a pre-existing 0755 socket directory is 0700 after start
Given a temporary XDG layout whose socket directory already exists with mode `0755`
When the daemon is run
Then the socket directory's mode is `0700`
And the daemon answers on the socket

### S3 - `owl daemon run` under a permissive umask restricts the socket and its runtime directory
Given a temporary XDG layout with `XDG_RUNTIME_DIR` set, whose `coding-owl` directory already exists with mode `0755`, and a process umask of `022`
When `owl daemon run` is started and answers
Then the socket under the runtime directory has mode `0600`
And the `coding-owl` directory under the runtime directory has mode `0700`
