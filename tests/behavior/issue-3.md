# Issue #3: Projects: register, list, show, move, rename, remove; config discovery from the base branch

All scenarios drive the built `owl` binary from the outside against a running
daemon. "XDG layout" is the temporary directory tree of issue #2: `HOME`,
`XDG_CONFIG_HOME`, `XDG_DATA_HOME` and `XDG_STATE_HOME` point into it and
`XDG_RUNTIME_DIR` is unset. "Temporary repository" means a real `git init`
repository under that layout whose base branch (`main` unless a scenario says
otherwise) carries at least one commit; every config file a scenario mentions
is committed to the branch it names, never left only in the working tree.

The recognised config `apiVersion` is `codingowl.dev/v1`.

## Scenarios

### S1 - add proposes the directory basename
Given a running daemon and a temporary repository at `<tmp>/api`
When `owl project add <tmp>/api` runs
Then it exits 0
And `owl project list` shows one Project named `api` whose path is `<tmp>/api`

### S2 - --name overrides the proposed name
Given a running daemon and a temporary repository at `<tmp>/api`
When `owl project add <tmp>/api --name backend` runs
Then it exits 0
And `owl project list` shows one Project named `backend` and none named `api`

### S3 - a duplicate name is refused and the message names the flag
Given a running daemon with a Project named `api` already registered
When `owl project add <tmp>/other-api` is run for a second repository whose basename is also `api`
Then it exits with a non-zero code
And stderr says a Project named `api` already exists and names the `--name` flag
And `owl project list` still shows exactly one Project

### S4 - a path that is not a git repository is refused
Given a running daemon and a directory `<tmp>/plain` that is not a git repository
When `owl project add <tmp>/plain` runs
Then it exits with a non-zero code
And stderr says the path is not a git repository and names the path
And `owl project list` shows no Project for it

### S5 - a name that could not be a directory is refused
Given a running daemon and a temporary repository
When `owl project add <path> --name a/b` runs
Then it exits with a non-zero code
And stderr says the name is invalid
And no Project is registered

### S6 - show reports the Project and the config file it loaded from the repository root
Given a running daemon and a registered Project whose base branch carries `.coding-owl.yaml` with `apiVersion: codingowl.dev/v1` and `branchPrefix: root/`
When `owl project show <name>` runs
Then it exits 0
And stdout has a `name:` line with the Project name, a `path:` line with its path and a `base branch:` line with its base branch
And stdout has a `config:` line naming `main:.coding-owl.yaml`
And stdout has a `branch prefix:` line reading `root/`

### S7 - discovery order, first match wins
Given a running daemon and a registered Project whose base branch carries all five in-repo config files - `.coding-owl.yaml`, `.config/coding-owl.yaml`, `.config/.coding-owl.yaml`, `.meta/coding-owl.yaml` and `.meta/.coding-owl.yaml` - each with a distinct `branchPrefix`
When the highest-priority file is removed from the base branch and `owl project show <name>` is run again, repeatedly, until none are left
Then each run names the next file in the order `.coding-owl.yaml`, `.config/coding-owl.yaml`, `.config/.coding-owl.yaml`, `.meta/coding-owl.yaml`, `.meta/.coding-owl.yaml`
And each run reports that file's own `branchPrefix` as the effective value

### S8 - the config-home fallback is used when the repository carries no config
Given a running daemon and a registered Project named `api` whose base branch carries no config file
And a file at `$XDG_CONFIG_HOME/coding-owl/api/config.yaml` with `apiVersion: codingowl.dev/v1` and `branchPrefix: fallback/`
When `owl project show api` runs
Then stdout's `config:` line names that absolute path
And the `branch prefix:` line reads `fallback/`

### S9 - an in-repo config outranks the config-home fallback
Given the Project of S8 with `.coding-owl.yaml` also committed to its base branch carrying `branchPrefix: root/`
When `owl project show api` runs
Then the `config:` line names `main:.coding-owl.yaml`
And the `branch prefix:` line reads `root/`

### S10 - no config anywhere falls back to defaults
Given a running daemon and a registered Project with no config file in the repository and none under the config home
When `owl project show <name>` runs
Then it exits 0
And the `config:` line reads `(none)`
And the `branch prefix:` line reads the default `owl/`

### S11 - config is read from the base branch, not the working tree
Given a running daemon and a registered Project whose base branch `main` carries `.coding-owl.yaml` with `branchPrefix: committed/`
When the same file in the working tree is edited to `branchPrefix: dirty/` without committing
And `owl project show <name>` runs
Then the `branch prefix:` line still reads `committed/`

### S12 - config on another branch is ignored
Given the Project of S11 with the working tree edit committed to a second branch `feature` which is then checked out
When `owl project show <name>` runs
Then the `branch prefix:` line still reads `committed/`
And the `config:` line still names `main:.coding-owl.yaml`

### S13 - an unknown apiVersion is refused
Given a running daemon and a registered Project whose base branch carries `.coding-owl.yaml` with `apiVersion: codingowl.dev/v99`
When `owl project show <name>` runs
Then it exits with a non-zero code
And stderr names the file, says the `apiVersion` is not recognised and prints the offending value

### S14 - a missing apiVersion is refused
Given a running daemon and a registered Project whose base branch carries `.coding-owl.yaml` with `branchPrefix: nope/` and no `apiVersion` key
When `owl project show <name>` runs
Then it exits with a non-zero code
And stderr names the file and says `apiVersion` is missing

### S15 - move updates the path and leaves identity intact
Given a running daemon and a Project named `api` registered at `<tmp>/api`, whose `owl project show` output was recorded
When the repository directory is moved to `<tmp>/moved` and `owl project move api <tmp>/moved` runs
Then it exits 0
And `owl project show api` reports the new path
And its `name:` and `registered:` lines are unchanged from before the move

### S16 - move refuses a path that is not a git repository
Given a running daemon and a Project named `api`
When `owl project move api <tmp>/plain` runs for a directory that is not a git repository
Then it exits with a non-zero code
And `owl project show api` still reports the original path

### S17 - rename moves the Project's configuration directory
Given a running daemon and a Project named `api` with a config-home file at `$XDG_CONFIG_HOME/coding-owl/api/config.yaml`
When `owl project rename api backend` runs
Then it exits 0
And `$XDG_CONFIG_HOME/coding-owl/backend/config.yaml` exists with the same contents
And `$XDG_CONFIG_HOME/coding-owl/api` no longer exists
And `owl project show backend` succeeds and `owl project show api` exits non-zero

### S18 - a failed rename leaves nothing behind
Given a running daemon with Projects `api` and `backend`, each with a config-home directory holding a distinct `config.yaml`
When `owl project rename api backend` runs
Then it exits with a non-zero code
And stderr says a Project named `backend` already exists
And `owl project list` still shows both `api` and `backend`
And both config-home directories still exist with their original contents

### S19 - remove drops the Project
Given a running daemon and a Project named `api`
When `owl project remove api` runs
Then it exits 0
And `owl project list` no longer shows `api`
And `owl project show api` exits with a non-zero code saying no such Project

### S20 - commands on an unknown Project fail clearly
Given a running daemon with no Project named `ghost`
When `owl project show ghost`, `owl project move ghost <path>`, `owl project rename ghost other` and `owl project remove ghost` each run
Then each exits with a non-zero code
And each names `ghost` in its stderr

### S21 - migrations run on daemon start and are idempotent
Given a temporary XDG layout with no database
When the daemon is started, a Project is registered, the daemon is stopped and started again
Then `$XDG_DATA_HOME/coding-owl/owl.db` exists after the first start
And the second start exits no differently and logs no migration error
And `owl project list` after the second start still shows the Project
And starting the daemon a third time leaves the database schema version unchanged from after the first start

### S22 - project commands report a stopped daemon
Given a temporary XDG layout with no daemon running
When `owl project list` runs
Then it exits with a non-zero code
And stderr says the daemon is not running and names the socket path

### S23 - a subdirectory of a repository is refused, naming the root
Given a running daemon and a temporary repository at `<tmp>/api` containing a subdirectory `sub`
When `owl project add <tmp>/api/sub` runs
Then it exits with a non-zero code
And stderr says the path is not the root of its git repository and names `<tmp>/api`

### S24 - --base-branch overrides the repository's current branch
Given a running daemon and a temporary repository whose branch `develop` carries `.coding-owl.yaml` with `branchPrefix: dev/` while `main` carries one with `branchPrefix: main/`, with `main` checked out
When `owl project add <path> --base-branch develop` runs and `owl project show <name>` follows
Then the `base branch:` line reads `develop`
And the `config:` line names `develop:.coding-owl.yaml`
And the `branch prefix:` line reads `dev/`

### S25 - a base branch that does not exist is refused
Given a running daemon and a temporary repository with only a `main` branch
When `owl project add <path> --base-branch nope` runs
Then it exits with a non-zero code
And stderr names the branch `nope`
And no Project is registered
