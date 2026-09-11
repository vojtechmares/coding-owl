# Issue #10: Release and formula: release workflow, coding-owl formula with brew services, owl daemon install

The release is CI: `scripts/release.sh` tags from `main`, and the workflow at
`.github/workflows/release.yml` builds the tag, publishes GitHub Release assets
and points the Homebrew tap at them. The scripts are what carry the behavior, so
the scenarios drive the scripts and the built `owl` binary directly, with the
tap and the git remote standing in as temporary repositories on disk.

`owl daemon install` is for machines without Homebrew: it writes a launchd
agent and hands it to launchctl. Its scenarios run on macOS, with a stub
`launchctl` first on the command's `PATH` in place of the real one, as the stub
agent of issue #5 stands in for Claude Code. The scenario that hands the agent
to the real launchd is a smoke test, gated on `OWL_SMOKE_LAUNCHD=1`, because it
registers a service with the machine it runs on.

MVP releases are `darwin/arm64` only (ADR-0010) and versions are ZeroVer,
`v0.x.y`, with no prereleases.

## Scenarios

### S1 - the release build produces a darwin/arm64 archive and its checksum
Given a checkout of the repository
When `scripts/build-release.sh v0.1.0` runs
Then it exits 0
And `coding-owl_v0.1.0_darwin_arm64.tar.gz` in the release directory holds an executable named `owl`
And that binary is a Mach-O arm64 executable
And `checksums.txt` beside it names the archive with the archive's own sha256

The release directory is `dist/`, and the `DIST` environment variable names
another, so a scenario can build somewhere of its own.

### S2 - the built binary reports the version it was built for
Given the archive of S1, unpacked
When the unpacked `owl --version` runs on an arm64 mac
Then it reports `0.1.0`, not `dev`

### S3 - the release build refuses a version that is not a release version
Given a checkout of the repository
When `scripts/build-release.sh 0.1`, `scripts/build-release.sh nightly` and `scripts/build-release.sh` with no version at all each run
Then each exits with a non-zero code
And each says what it wanted instead
And the release directory holds no archive

### S4 - the rendered formula points at the release asset with its checksum
Given a temporary tap checkout
When `scripts/bump-formula.sh` runs as a dry run for version `0.1.0` and a known checksum
Then it exits 0
And the rendered `Formula/coding-owl.rb` declares the class `CodingOwl`, version `0.1.0`, that checksum, and a url under `https://github.com/vojtechmares/coding-owl/releases/download/v0.1.0/`
And it installs the binary as `owl`
And it declares a `service` block running `owl daemon run` with `keep_alive true`
And nothing was committed to the tap

### S5 - publishing the formula commits it to the tap and pushes it
Given a temporary tap checkout whose remote is a bare repository, and a release asset that can be downloaded
When `scripts/bump-formula.sh` runs for version `0.1.0`
Then it exits 0
And the tap's branch holds `Formula/coding-owl.rb` for `0.1.0`, signed off
And the bare remote holds that commit on the tap branch

### S6 - publishing the same version again changes nothing
Given the tap of S5, already carrying `0.1.0`
When `scripts/bump-formula.sh` runs again for `0.1.0` with the same checksum
Then it exits 0
And it says the formula already points at that version
And the tap has no second commit

### S7 - a release asset that cannot be downloaded stops the formula being written
Given a temporary tap checkout and an asset URL that answers 404
When `scripts/bump-formula.sh` runs for `0.1.0`
Then it exits with a non-zero code
And stderr says the asset cannot be downloaded
And the tap holds no `Formula/coding-owl.rb`

### S8 - a version or a checksum that is not well formed is refused
Given a temporary tap checkout
When `scripts/bump-formula.sh` runs with version `nightly`, and again with a checksum that is not 64 hex characters
Then each exits with a non-zero code
And each stderr names what was wrong
And the tap holds no `Formula/coding-owl.rb`

### S9 - cutting a release refuses a working tree with uncommitted changes
Given a clone of the repository on `main` with an uncommitted file
When `scripts/release.sh v0.1.0` runs
Then it exits with a non-zero code
And stderr says the working tree is dirty
And no tag was created

### S10 - cutting a release refuses a branch that is not main
Given a clone of the repository on a branch other than `main`
When `scripts/release.sh v0.1.0` runs
Then it exits with a non-zero code
And stderr names the branch it is on and the branch releases come from
And no tag was created

### S11 - a dry run reports the version and changes nothing
Given a clone of the repository on `main`, up to date with its remote
When `scripts/release.sh --dry-run v0.2.0` runs
Then it exits 0
And it reports `v0.2.0`
And no tag was created locally or on the remote

### S12 - cutting a release tags the commit and pushes the tag
Given a clone of the repository on `main`, up to date with a bare remote
When `scripts/release.sh --yes v0.2.0` runs
Then it exits 0
And `v0.2.0` is an annotated tag on the checked out commit
And the remote carries that tag
And nothing but the tag was pushed

### S13 - cutting a release refuses a version that already exists
Given the clone of S12, whose remote already carries `v0.2.0`
When `scripts/release.sh --yes v0.2.0` runs again
Then it exits with a non-zero code
And stderr says the tag already exists
And it refuses before it starts tagging, rather than letting git fail
And the remote still carries exactly one `v0.2.0`

### S21 - a formula that was committed but never pushed is pushed the next time
Given a temporary tap checkout whose remote refuses the push, and a run that failed on it
When the remote is reachable again and `scripts/bump-formula.sh` runs for the same version
Then it exits 0
And the remote carries the formula for that version

### S15 - owl daemon install writes a launchd agent that runs the daemon
Given a temporary HOME on macOS
When `owl daemon install` runs
Then it exits 0
And `$HOME/Library/LaunchAgents/dev.codingowl.owld.plist` exists
And read as a property list it names the label `dev.codingowl.owld`, runs the path of the running `owl` with the arguments `daemon run`, has `KeepAlive` and `RunAtLoad` set true, and writes both its output streams to a log file under the state directory
And it is a plist `plutil -lint` accepts
And the state directory it logs into is readable only by its owner

### S16 - owl daemon install hands the agent to launchctl
Given the temporary HOME of S15 and a stub `launchctl` first on the PATH
When `owl daemon install` runs
Then the stub was asked to `bootstrap` the plist into the user's own domain
And the command's output says where the agent was written and how to check on it

### S17 - installing twice replaces the agent that is already there
Given an agent already installed
When `owl daemon install` runs again
Then it exits 0
And the stub `launchctl` was asked to remove the old agent before bootstrapping the new one
And the plist on disk is still valid

### S18 - owl daemon install --print writes nothing
Given a temporary HOME on macOS
When `owl daemon install --print` runs
Then it exits 0
And it prints the plist to standard output
And `$HOME/Library/LaunchAgents` holds nothing
And launchctl was never invoked

### S19 - a launchd failure is reported, not swallowed
Given a stub `launchctl` that exits non-zero when it is asked to bootstrap
When `owl daemon install` runs
Then it exits with a non-zero code
And stderr holds what launchctl printed

### S20 - the installed agent runs a daemon owl daemon status can reach
Given a real launchd, with `OWL_SMOKE_LAUNCHD=1` set
When `owl daemon install` runs and the agent starts
Then `owl daemon status` reports a version, an uptime and the socket path
And unloading the agent afterwards stops the daemon
