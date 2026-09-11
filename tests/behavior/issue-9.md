# Issue #9: Desktop bootstrap: Wails and React, pure view over the shared client, liquid-glass theme, parity with the core loop

The desktop app is a client of the daemon like the CLI (ADR-0009). Its Go side
is the `internal/desktop` package: the struct the app binds to the frontend,
whose every method is a call into `internal/client`. The scenarios drive that
package in-process, over the daemon's unix socket, against the same temporary
XDG layout, repository and stub agent that `tests/behavior/issue-5.md`
describes, and confirm what the app did by asking the `owl` binary.

The app cannot be driven from a test without a display, so what the frontend
is made of is checked on disk: the theme is one tokens file, and everything
else consumes it. `wails build` is exercised opt-in, because it downloads the
frontend's dependencies.

"The app" below means an `internal/desktop.App` created for the layout's
socket, exactly as `cmd/owl-desktop` creates it.

## Scenarios

### S1 - the app says the daemon is down, and names the socket
Given a layout with no daemon running
When the app is asked for the daemon's status
Then it reports the daemon as not running
And names the socket path it tried
And no method of the app panics or hangs

### S2 - the app connects over the socket and reports the daemon
Given a running daemon
When the app is asked for the daemon's status
Then it reports the daemon as running, with the daemon's version and socket path

### S3 - the app reaches the daemon only through the shared client
Given the repository
When the imports of `internal/desktop` and `cmd/owl-desktop` are listed
Then `internal/desktop` imports `internal/client`
And neither package imports the generated protobuf code, ConnectRPC, the store, the git package, or a SQL driver

### S4 - projects reflect daemon state
Given a running daemon and a Project registered with `owl project add`
When the app lists Projects
Then it lists that Project with the name, path and base branch `owl project list` reports
And after `owl project remove` the app no longer lists it

### S5 - the queue and the Job list reflect daemon state
Given a running daemon, a Project, and two Jobs queued with `owl add`
When the app lists Jobs
Then it lists both, `pending`, in queue order with positions one and two
And the app's overview counts two pending Jobs
And after `owl queue remove` of the first, the app lists the second at position one

### S6 - Job detail carries the plan, the handoff, the verification output and a diff summary
Given a running daemon, a Project with one passing and one failing check, and a planned Job whose Agent writes a handoff and commits a file
When the Job's Runs have finished and the app is asked for the Job's detail
Then the detail carries the plan the planning Run wrote
And the handoff as it stands on the Job's branch
And every check by name with whether it passed and what it printed
And a diff summary naming the committed file with its added lines, and the totals

### S7 - the same detail is on the wire for the CLI
Given the Job of S6
When `owl jobs show <job>` runs
Then it prints the handoff under `handoff:`
And a `diff:` line naming how many files changed and the added and removed lines

### S8 - the live log view streams a running Run
Given a running daemon and a Job whose Agent waits to be released
When `owl start` starts a Run and the app is asked to follow that Run's log
Then the app emits each line of the Run's structured output as an event, in order, as it is written
And when the Agent is released and exits, the app emits that the stream ended
And a Run that does not exist is refused with an error, not an event

### S9 - start, accept and drop from the app are reflected in owl status
Given a running daemon, a Project, and a Job in review that the app started
When the app accepts the Job
Then `owl jobs show` reports it `done`
And given a second Job in review
When the app drops it
Then `owl jobs show` reports it `cancelled` with its branch gone
And starting with an empty queue reports that nothing was started, with no error

### S10 - the theme is one tokens file that everything consumes
Given the frontend sources under `cmd/owl-desktop/frontend/src`
When they are scanned
Then `theme/tokens.css` exists and declares colour, blur, radius and spacing custom properties
And no other source file contains a colour literal (hex, `rgb(`, `hsl(`)
And the palette in the tokens file is night-sky and navy blues: every hue is between 200 and 260 degrees or grey

### S11 - the app builds
Given the Wails CLI, Node and npm, and `OWL_DESKTOP_BUILD=1`
When `make desktop` runs
Then it exits 0 and leaves an app bundle under `cmd/owl-desktop/build/bin`
And without `OWL_DESKTOP_BUILD` the scenario is skipped, not failed
