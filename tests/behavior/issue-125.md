# Issue #125: owl setup, the first-time walkthrough

Nothing runs until three things are true: a daemon is up, `claude` is somewhere
the daemon will find it, and an Account is registered. Each is documented, and
a first-time user who has not read the documentation has no way to know which
of the three is missing - or that there were three.

`owl setup` walks them in that order. It reports what is already true rather
than redoing it, so a second run says so and stops; it asks only about what is
missing; and it ends by naming `owl project add`, which carries on into the
first Project (and, since issue #126, points at `owl project setup` from
there).

Two things it deliberately does not do. It never starts a daemon behind the
user's back: on a Homebrew install it names `brew services start coding-owl`,
and elsewhere on macOS it offers to write the launch agent `owl daemon install`
writes - an offer, answered, never assumed. And it never guesses where `claude`
is: it looks exactly where the daemon will look at Run time, and asks when that
finds nothing.

Writing `claudePath` edits the daemon's own `config.yaml`, which may already
carry other settings, so that one key is set and the rest of the file is left
as it was. The daemon reads that setting when it starts, so the command says
the daemon has to be restarted before it takes effect.

The answers are read from standard input, so the command works piped as well as
typed, and running out of input ends it rather than hanging - the same reading
`owl account add` and `owl project setup` do.

**A note for whoever verifies this.** `owl daemon install` loads a launch agent
into the login session with `launchctl bootstrap`, which is the real session
even when `HOME` points somewhere else. No scenario here answers yes to that
offer, and none should be made to: S3 exists to check that declining leaves the
machine alone.

## Scenarios

### S1 - a machine that is already set up says so and asks nothing
Given a running daemon, a `claude` the daemon would find, and a registered Account
When `owl setup` runs with nothing on its standard input
Then it exits 0
And it says the daemon is running, names where `claude` was found, and says an Account is registered
And it asks no question, and writes no file

### S2 - with no daemon it still does what it can, and says what is left
Given no daemon running, and a `claude` on the PATH
When `owl setup` runs
Then it exits non-zero, because it could not finish
And it says the daemon is not running and names what starts one
And it still reports `claude`, because that check needs no daemon
And it says to run `owl setup` again once the daemon is up

### S3 - the offer to write a launch agent is an offer
Given no daemon running, on macOS, and an owl that did not come from Homebrew
When `owl setup` runs and the offer is declined
Then it exits non-zero
And no launch agent file is written under the home directory it was given
And it names `owl daemon install` as what would do it later

### S4 - an owl that came from Homebrew is told to use brew services
Given no daemon running, and an owl binary whose own path is inside a Homebrew Cellar
When `owl setup` runs
Then it says to run `brew services start coding-owl`
And it does not offer to write a launch agent

### S5 - a claude the daemon would not find is asked for and written down
Given a running daemon, an Account, and no `claude` on the PATH or under `~/.local/bin`
When `owl setup` runs and is given the path to one
Then `<config home>/coding-owl/config.yaml` carries `claudePath` set to that path
And every other setting that file already carried is still there, unchanged
And it says the daemon has to be restarted before it uses it

### S6 - a path that is not a program to run is refused and asked again
Given the same, and a path that is a directory, and then a path that is not there
When each is offered in turn and then a real one is
Then each is refused, saying what was wrong with it
And the real one is written

### S7 - a claude the daemon would find is reported and nothing is written
Given a running daemon, an Account, and a `claude` on the PATH
When `owl setup` runs
Then it names the path it found
And the daemon's `config.yaml` is not created, and an existing one is not changed

### S8 - with no Account it asks for a name and registers one
Given a running daemon, a findable `claude`, and no Account registered
When `owl setup` runs, is given the name `work`, and is given the token
Then it exits 0
And `owl account list` shows an Account named `work`

### S9 - a name that is not a name is refused and asked again
Given the same
When a name Owl will not take is offered, and then `work`
Then the first is refused, saying so
And the Account registered is `work`

### S10 - running it again when everything is done changes nothing
Given a machine S8 has just finished setting up
When `owl setup` runs a second time
Then it exits 0, asks nothing, and reports all three as already done
And `owl account list` still shows exactly the one Account

### S11 - it ends by saying what comes next
Given a successful run
When its output is read to the end
Then it names `owl project add` as the next thing to do

### S12 - an answer that never comes ends the command rather than hanging
Given a running daemon, an Account, and no findable `claude`
When `owl setup` runs with its standard input closed
Then it exits non-zero rather than waiting
And it says it needed an answer and got none
And the daemon's `config.yaml` is not written
