# Issue #64: Socket path length is checked only by owl daemon status

A unix socket path can be at most 103 bytes on the platforms Owl targets
(ADR-0004, ADR-0014). Reported at commit `701b936`: only `owl daemon status`
checked that before dialing and said so; every other command that talks to the
daemon dialed anyway and, with an over-long state directory, reported the
daemon as not running with the dialer's raw words - "connect: invalid
argument" - which is not what is wrong.

The fix is that the check lives in the one client every command and the
desktop app talk to the daemon through (ADR-0004), so that every command
reports an over-long socket path the same way: the path is too long, with the
path and the limit, rather than that the daemon is not running. `owl daemon
status` reuses it. Moving the socket to a shorter place is out of scope.

The client scenario drives the `client` package directly. The end-to-end
scenarios drive the built `owl` binary with a state directory that puts the
socket path over the limit.

## Scenarios

### S1 - `owl status` says the socket path is too long
Given a temporary XDG layout whose `XDG_STATE_HOME` puts the socket path over 103 bytes, and no daemon running
When `owl status` runs
Then it exits non-zero
And its error says the path is too long and names the path
And its error does not say the daemon is not running or that the argument is invalid

### S2 - `owl add` says the socket path is too long
Given the same layout, and a prompt to queue
When `owl add work --project api` runs
Then it exits non-zero
And its error says the path is too long and names the path
And its error does not say the daemon is not running or that the argument is invalid

### S3 - `owl daemon status` says the same thing
Given the same layout
When `owl daemon status` runs
Then it exits non-zero and its error says the path is too long and names the path

### S4 - the client refuses an over-long socket path before dialing
Given a client for a socket path over 103 bytes
When the daemon's status is asked for
Then the error says the path is too long and names the path
And the error is not the daemon not running
