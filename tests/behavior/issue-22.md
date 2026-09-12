# Issue #22: Desktop cask - codesign, notarize, coding-owl-desktop cask depending on the formula

The desktop app becomes installable (ADR-0009, ADR-0010). The release workflow
builds the Wails app on a mac, signs it, and attaches it to the release;
`scripts/bump-cask.sh` renders `Casks/coding-owl-desktop.rb` in the tap from
scratch on every release and pushes it.

The app is **ad-hoc signed and not notarised**, which the maintainer decided on
this issue: no Apple Developer Program membership is needed, and the acceptance
criterion about Gatekeeper is met by a `postflight_steps` block that clears
`com.apple.quarantine` rather than by notarisation. The cask declares
`depends_on formula: "coding-owl"` (ADR-0010), so there is exactly one daemon
per machine.

The scripts are what carry the behavior, so the scenarios drive the scripts
directly, with the tap and the git remote standing in as temporary repositories
on disk, as the issue #10 scenarios do. MVP releases are `darwin/arm64` only.

Two scenarios install software on the machine they run on and are gated on
`OWL_SMOKE_CASK=1`: one asks Homebrew what it makes of the rendered cask, and
one installs it and takes it away again.

## Scenarios

### S1 - the desktop release build produces a signed app in a zip, with its checksum
Given a checkout of the repository on an arm64 mac
When `scripts/build-desktop-release.sh v0.1.0` runs
Then it exits 0
And `CodingOwl-0.1.0-arm64.zip` in the release directory holds `Coding Owl.app`
And that bundle's `Contents/MacOS` holds the app's executable
And a checksum file beside the zip carries the zip's own sha256 and its name

### S2 - the app in the zip is ad-hoc signed and accepted as such
Given the zip of S1, unpacked
When `codesign --verify --deep --strict` runs against `Coding Owl.app`
Then it exits 0
And `codesign -dv` reports the signature as adhoc and the identifier as `dev.codingowl.desktop`

### S3 - the app reports the version it was built for
Given the zip of S1, unpacked
When its `Info.plist` is read
Then `CFBundleShortVersionString` is `0.1.0`, not `0.0.0`
And `CFBundleName` is `Coding Owl`

### S4 - the desktop release build refuses a version that is not a release version
Given a checkout of the repository
When `scripts/build-desktop-release.sh 0.1`, `scripts/build-desktop-release.sh nightly` and the script with no version at all each run
Then each exits with a non-zero code
And nothing is written to the release directory

### S5 - the cask points at the published asset with its checksum
Given a temporary tap checkout and a release whose zip can be downloaded
When `scripts/bump-cask.sh` runs for that version and checksum
Then `Casks/coding-owl-desktop.rb` in the tap names that version and that sha256
And its url is the release asset for the version it names
And it installs `Coding Owl.app`

### S6 - the cask brings the daemon with it and says what it runs on
Given the cask of S5
Then it declares `depends_on formula: "coding-owl"`
And it declares `depends_on arch: :arm64`
And its caveats say the app is ad-hoc signed rather than notarised, and that the cask clears the quarantine flag

### S7 - the cask clears the quarantine flag itself, and an install is not undone when it cannot
Given the cask of S5
Then it has a postflight step that removes `com.apple.quarantine` from the installed app
And that step is allowed to fail rather than aborting an install that has already put the app in place

### S8 - the cask is committed and pushed to the tap
Given a temporary tap checkout whose origin is a bare repository on disk
When `scripts/bump-cask.sh` runs
Then the tap's branch on that origin carries `Casks/coding-owl-desktop.rb` at the new version
And the commit is signed off and says what it bumped

### S9 - the cask at the same version changes nothing
Given a tap already serving the version being released
When `scripts/bump-cask.sh` runs again for it
Then it exits 0
And the tap's branch on the origin has no new commit

### S10 - a cask that was rendered but never pushed is pushed next time
Given a tap checkout committed at the new version whose origin is still at the version before
When `scripts/bump-cask.sh` runs again for that version
Then it exits 0
And the tap's branch on the origin carries the new version

### S11 - the cask is not written for an asset that is not there
Given a version whose zip cannot be downloaded
When `scripts/bump-cask.sh` runs
Then it exits with a non-zero code, saying the asset cannot be downloaded
And the tap is unchanged

### S12 - the cask refuses a version or a checksum that is not one
Given a temporary tap checkout
When `scripts/bump-cask.sh` runs with a version like `0.1`, or with a checksum that is not 64 hex characters, or with either missing
Then each exits with a non-zero code
And the tap is unchanged

### S13 - the release workflow ships the app and the formula from one release
Given `.github/workflows/release.yml`
Then the job that builds the desktop app runs on a mac and is guarded by the same tag check as the rest
And the job that publishes the release attaches the desktop zip alongside the archive and the checksums
And the job that bumps the cask needs that same release, so the cask and the formula move at one version

### S14 - a prerelease points the tap at neither the formula nor the cask
Given `.github/workflows/release.yml`
Then the cask job is skipped for a prerelease on the same condition as the formula job

### S15 - Homebrew reads the rendered cask without complaint
Given the rendered cask of S5 and `OWL_SMOKE_CASK=1`
When `brew style` runs against it
Then it exits 0

### S16 - installing the cask puts the app in place unquarantined, and taking it away removes it
Given the rendered cask of S5 and `OWL_SMOKE_CASK=1`
And two of its lines changed so it can be installed here: it points at the built zip on disk rather than at a release that does not exist yet, and it does not bring the daemon, which would install the formula on this machine
When `brew install --cask` runs for it with an applications directory of its own
Then `Coding Owl.app` is in that directory
And it carries no `com.apple.quarantine` attribute
And `brew uninstall --cask` takes it away again

What the cask points at is S5's and what it brings with it is S6's; this
scenario is about where the app lands and what is on it when it does.
