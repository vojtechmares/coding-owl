# Issue #112: Edit an Account's standing instructions in the desktop app

An Account's standing instructions are text every Run on that Account reads,
whichever Project the Run is for (ADR-0037). Owl keeps them in the Account's own
configuration directory, under whatever that Account's coding tool calls the
file - `CLAUDE.md` for the `claude-code` Driver - so the tool reads them itself
and Owl injects nothing. Editing them by hand means knowing where that directory
is; the desktop app is where an Account is already looked at, so it is where the
file is editable.

These scenarios drive the desktop app's Go side, the `internal/desktop` package,
in-process over the daemon's socket, exactly as `tests/behavior/issue-9.md`
describes: the app is a pure view over the daemon (ADR-0009), so what the app
saves is what the daemon wrote, and what the app reads back is what a reopened
window would show. The Account, its configuration directory and its credential
are as `tests/behavior/issue-14.md` sets them up, with credentials in a file so
that no test touches a real keychain.

"Reopened" below means a second app over the same socket, because what was saved
has to outlive the window that saved it.

## Scenarios

### S1 - instructions saved through the app come back through it, and are on disk
Given a running daemon and the Account `work`, which nobody has written standing instructions for
When the app is asked for that Account's instructions
Then it reports `CLAUDE.md`, at that file's place in the Account's configuration directory, with no text
And when instructions are saved through the app and the app is reopened, it reads back exactly what was saved
And that text is what `CLAUDE.md` in the Account's configuration directory holds
And the app told its frontend nothing: saving is a request, not something that happens to it

### S2 - saving a blank box clears them and takes the file away
Given the Account of S1 with standing instructions saved through the app
When text that is blank is saved through the app
Then what comes back has no text
And `CLAUDE.md` is gone from the Account's configuration directory
And the app reads the Account's instructions back as none, as it did before any were written
