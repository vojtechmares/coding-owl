# Issue #63: owl daemon run --verbose does not exist

ADR-0002 says debugging the daemon is just running it: `owl daemon run
--verbose` in a terminal shows exactly what the supervised process does.
Reported at commit `701b936`: the flag did not exist - the command refused it
as unknown - and the daemon logged at one level, with nothing to turn up.

The fix is that `owl daemon run` accepts `--verbose`, which turns the daemon's
log up to debug; without it the log stays at info. What the daemon says at
debug level is meant for reading while it runs, and one such line is said on
every start, so that turning the log up shows straight away. A log format or
file logging option is out of scope.

The scenarios drive the built `owl` binary.

## Scenarios

### S1 - `owl daemon run --verbose` is accepted and logs at debug level
Given a temporary XDG layout and no daemon running
When `owl daemon run --verbose` is started
Then it listens on the socket
And its log carries a line at level `DEBUG`

### S2 - without `--verbose` the daemon logs at info level
Given a temporary XDG layout and no daemon running
When `owl daemon run` is started
Then it listens on the socket and its log carries lines at level `INFO`
And its log carries no line at level `DEBUG`

### S3 - `owl daemon run --help` describes the flag
Given the `owl` binary
When `owl daemon run --help` runs
Then its output lists `--verbose` and says it turns the log up to debug
