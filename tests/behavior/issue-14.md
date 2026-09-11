# Issue #14: Accounts: owl account add with token setup, isolated configuration directory per Run, keychain credential, Project to Account binding

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on the `PATH` of both the daemon
and the commands, in place of Claude Code. The XDG layout, the temporary
repository and the stub are as `tests/behavior/issue-5.md` describes them.

An Account is a subscription Owl runs work on, with a configuration directory of
its own so that Owl's nights never disturb the user's own tool setup (ADR-0019).
The secret is held by the **credential store**, and the database holds only a
reference to it. The store is the OS keychain on macOS and, on a platform that
has none, a file only its owner can read; the daemon's own configuration chooses
with `credentialStore: keychain` or `credentialStore: file`, and these scenarios
set `file` so that no test touches a real keychain.

`claude setup-token` prints a long-lived token to the terminal and saves it
nowhere, so `owl account add` runs it for the user and then takes the token they
paste. "A token on stdin" below means exactly that paste.

## Scenarios

### S1 - owl account add creates the Account, its configuration directory and its credential
Given a running daemon and no Accounts
When `owl account add work --token-stdin` runs with a token on stdin
Then it exits 0
And stdout names the Account and the directory it created
And `$XDG_DATA_HOME/coding-owl/accounts/work` is a directory nobody but its owner can read
And `owl account list` reports `work`, its driver `claude-code`, and that it has a credential

### S2 - the secret is in neither the database nor the daemon's log
Given the Account of S1
When the database and everything the daemon logged are read
Then the token appears in neither
And the database holds a reference to the credential instead

### S3 - the credential store is readable only by its owner
Given the Account of S1 and a daemon configured to keep credentials in a file
Then that file is readable only by its owner
And it holds the token under the Account's reference

### S4 - owl account add walks the user through the tool's token setup
Given a running daemon and the stub agent first on the PATH
When `owl account add work` runs with a token on stdin, without `--token-stdin`
Then the stub agent was invoked as `setup-token`
And it was invoked with `CLAUDE_CONFIG_DIR` set to the Account's configuration directory
And the Account ends up with the token that was pasted, not with anything the tool printed

### S5 - owl account add refuses a name that is taken
Given the Account of S1
When `owl account add work --token-stdin` runs again
Then it exits with a non-zero code
And stderr says the Account already exists
And `owl account list` still reports one Account

### S6 - owl account add refuses an empty token
Given a running daemon
When `owl account add work --token-stdin` runs with nothing on stdin
Then it exits with a non-zero code
And stderr says a token is needed
And no Account was created, and no configuration directory was left behind

### S7 - owl account add refuses a Driver Owl does not have
Given a running daemon
When `owl account add work --driver codex --token-stdin` runs with a token on stdin
Then it exits with a non-zero code
And stderr names the drivers Owl has
And no Account was created

### S8 - owl account list reports every Account, and says so when there are none
Given a running daemon with no Accounts
When `owl account list` runs
Then it exits 0 and says there are no Accounts
And after two Accounts are added, it reports both, in the order they were added

### S9 - a Run for a Project bound to an Account carries that Account's configuration directory and token
Given the Account of S1 and a registered Project whose base branch carries `.coding-owl.yaml` with `account: work`, and a pending Job against it
When `owl start` runs and the Run finishes
Then the environment the stub agent recorded has `CLAUDE_CONFIG_DIR` set to the Account's configuration directory
And it has `CLAUDE_CODE_OAUTH_TOKEN` set to the Account's token
And the directory is the Account's own, not the user's `~/.claude`

### S10 - a Project that names no Account refuses to start
Given a registered Project whose configuration names no Account, and a pending Job against it
When `owl start` runs
Then it exits with a non-zero code
And stderr says the Project names no Account and how to give it one
And `owl jobs show <job>` still reports the Job as pending with no Run

### S11 - a Project naming an Account that is not there refuses to start
Given a registered Project whose configuration names `account: missing`, and a pending Job against it
When `owl start` runs
Then it exits with a non-zero code
And stderr names `missing`
And the Job is still pending with no Run

### S12 - the Account a Job ran on is reported with the Job
Given the finished Run of S9
When `owl jobs show <job>` runs
Then it reports the Job's Account as `work`

### S13 - owl account remove takes the Account and its credential away
Given the Account of S1 with no Job against it
When `owl account remove work` runs
Then it exits 0
And `owl account list` no longer reports it
And the credential store no longer holds its token
And its configuration directory is still there, and stdout says where it is

### S14 - owl account remove refuses while a Job references the Account
Given the finished Run of S9
When `owl account remove work` runs
Then it exits with a non-zero code
And stderr says how many Jobs reference it
And `owl account list` still reports it

### S15 - owl account remove refuses an Account that is not there
Given a running daemon
When `owl account remove nothing` runs
Then it exits with a non-zero code
And stderr names `nothing`

### S16 - an Account records whether failover is allowed
Given a running daemon
When `owl account add work --token-stdin --failover` runs with a token on stdin
Then `owl account list` reports that failover is allowed for `work`
And for an Account added without the flag it reports that it is not
