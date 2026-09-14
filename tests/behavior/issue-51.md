# Issue #51: Clean daemon shutdown fails while a chat exchange is open

The daemon's server hands every request a context of its own, unrelated to the
daemon's lifetime. Reported at commit `701b936`: a chat exchange parked on
consent, which waits up to five minutes for the user to answer, or parked on a
provider that is slow to answer, keeps its connection open through the
daemon's shutdown. The shutdown's five second deadline runs out, the daemon
reports that it failed to shut down, exits non-zero, and launchd logs a
failure.

The fix is that a request's context is derived from the daemon's, so stopping
the daemon ends what is parked, and that a request ended this way is reported
to its client as cancelled rather than as Owl breaking. The consent and
provider timeouts are not changed (out of scope), and neither is what a
half-finished exchange leaves behind.

The scenarios drive the built `owl` binary and the desktop app's own Go API
against a daemon, with the fake model provider of issue #18 standing in for a
real one. "Within the shutdown deadline plus a margin" means within eight
seconds of the daemon being told to stop.

## Scenarios

### S1 - the daemon stops cleanly while an exchange is parked on consent
Given a running daemon and a chat exchange whose model has asked to run a command, which nobody has answered
When the daemon is sent `SIGTERM`
Then it exits with status 0 within the shutdown deadline plus a margin

### S2 - the client parked on consent is told the exchange was cancelled
Given the exchange of S1
When the daemon has been told to stop
Then the app reports the end of the answer with an error that says it was cancelled, rather than waiting on

### S3 - the daemon stops cleanly while an exchange is parked on the provider
Given a running daemon and a chat exchange whose provider has been asked and has not answered
When the daemon is sent `SIGTERM`
Then it exits with status 0 within the shutdown deadline plus a margin
And the app reports the end of the answer with an error that says it was cancelled

### S4 - a request the daemon ended is reported as cancelled, not as Owl breaking
Given a request that ended because its context was cancelled, or because its deadline passed
When the failure is given its code for the client
Then the code is the cancelled code, or the deadline-exceeded code, and not the internal one
