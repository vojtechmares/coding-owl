# Issue #60: Listener and socket not cleaned up on start-up errors after listening

The daemon listens on its unix socket in the foreground and removes the socket
when it stops (ADR-0002). Reported at commit `701b936`: the daemon's own
configuration, the credential store and the recovery of an earlier daemon's
Runs were all read after the socket was already being listened on, and a
failure in any of them returned without closing the listener or removing the
socket. A dead socket file was left behind until the next start found it
stale.

The fix is that everything that can fail without needing the listener -
configuration, credential store, recovery - runs before the daemon listens, so
that a daemon which cannot start never owns a socket. Anything that must fail
after listening closes the listener and removes the socket on the way out. The
permission window between listening and restricting the socket is issue #61
and out of scope.

The daemon scenarios drive `daemon.Run` directly, as the existing daemon tests
do. The end-to-end scenario drives the built `owl` binary.

## Scenarios

### S1 - a daemon whose configuration does not parse leaves no socket behind
Given a temporary XDG layout whose global configuration file does not parse
When the daemon is run
Then it returns an error naming the configuration file
And no file exists at the socket path

### S2 - a daemon whose credential store is unknown leaves no socket behind
Given a temporary XDG layout whose global configuration names a credential store that does not exist
When the daemon is run
Then it returns an error naming the credential store
And no file exists at the socket path

### S3 - `owl daemon run` with a broken configuration exits and leaves the daemon not running
Given a temporary XDG layout whose global configuration file does not parse
When `owl daemon run` is started
Then it exits non-zero and its output names the configuration file
And no file exists at the socket path
And `owl daemon status` reports the daemon is not running
