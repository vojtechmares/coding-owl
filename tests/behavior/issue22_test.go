package behavior_test

// Behavior tests for issue #22. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-22.md. The release scenarios drive the repository's own
// scripts, with a bare repository standing in for the Homebrew tap as the issue
// #10 scenarios do. S15 and S16 ask the Homebrew on the machine they run on and
// are gated on OWL_SMOKE_CASK=1.

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const caskVersion = "v0.1.0"

// builtDesktop builds the desktop release once: a Wails build is slow and four
// scenarios read the same zip.
var (
	desktopOnce sync.Once
	desktopDist string
	desktopOut  result
)

func builtDesktopRelease(t *testing.T) (dist string, res result) {
	t.Helper()
	onDarwin(t)
	desktopOnce.Do(func() {
		dir, err := os.MkdirTemp("", "owldesktop")
		if err != nil {
			t.Fatal(err)
		}
		desktopDist = dir
		desktopOut = runScript(t, repoScript("build-desktop-release.sh"), repoDir,
			[]string{"DIST=" + dir}, caskVersion)
	})
	return desktopDist, desktopOut
}

// onDarwin skips a scenario that can only be true on the platform the app is
// built for (ADR-0010).
func onDarwin(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the desktop app is built for macOS only")
	}
}

// onSmoke skips a scenario that installs software on the machine it runs on.
func onSmoke(t *testing.T) {
	t.Helper()
	onDarwin(t)
	if os.Getenv("OWL_SMOKE_CASK") != "1" {
		t.Skip("set OWL_SMOKE_CASK=1 to let this scenario ask the machine's own Homebrew")
	}
}

// desktopZip is where the built zip and its checksum file are.
func desktopZip(dist string) (zip, sums string) {
	name := "CodingOwl-" + strings.TrimPrefix(caskVersion, "v") + "-arm64.zip"
	return filepath.Join(dist, name), filepath.Join(dist, name+".sha256")
}

// unzipped is the app bundle out of the zip, unpacked once per scenario.
func unzipped(t *testing.T) string {
	t.Helper()
	dist, res := builtDesktopRelease(t)
	if res.code != 0 {
		t.Fatalf("build-desktop-release.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	zip, _ := desktopZip(dist)
	into := t.TempDir()
	// ditto rather than unzip: it is what wrote the archive, and it keeps the
	// bundle's symbolic links and its signature intact.
	out, err := exec.Command("/usr/bin/ditto", "-x", "-k", zip, into).CombinedOutput()
	if err != nil {
		t.Fatalf("unpacking %s: %v\n%s", zip, err, out)
	}
	return filepath.Join(into, "Coding Owl.app")
}

// plist reads one key out of the app's Info.plist.
func plist(t *testing.T, app, key string) string {
	t.Helper()
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :"+key,
		filepath.Join(app, "Contents", "Info.plist")).CombinedOutput()
	if err != nil {
		t.Fatalf("reading %s from the app's Info.plist: %v\n%s", key, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (tp *tap) cask() string { return filepath.Join(tp.dir, "Casks", "coding-owl-desktop.rb") }

// caskEnv is what scripts/bump-cask.sh needs to work on a temporary tap.
func caskEnv(tp *tap, version, sha, assetURL string, extra ...string) []string {
	env := []string{
		"VERSION=" + version,
		"SHA256=" + sha,
		"TAP_DIR=" + tp.dir,
		"TAP_BRANCH=" + tp.branch,
		"ASSET_URL=" + assetURL,
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	}
	return append(env, extra...)
}

// renderedCask is the cask as the script writes it, without committing.
func renderedCask(t *testing.T) string {
	t.Helper()
	return renderedCaskFor(t, "0.1.0", testDigest)
}

func TestS1CaskTheDesktopBuildProducesASignedAppInAZip(t *testing.T) {
	dist, res := builtDesktopRelease(t)

	if res.code != 0 {
		t.Fatalf("build-desktop-release.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	zip, sums := desktopZip(dist)
	if _, err := os.Stat(zip); err != nil {
		t.Fatalf("no zip was written: %v", err)
	}
	app := unzipped(t)
	if _, err := os.Stat(filepath.Join(app, "Contents", "MacOS", "owl-desktop")); err != nil {
		t.Errorf("the bundle holds no executable: %v", err)
	}
	// The checksum file carries the zip's own digest, or the cask would point
	// at an asset with somebody else's.
	body, err := os.ReadFile(sums)
	if err != nil {
		t.Fatalf("no checksum file: %v", err)
	}
	line := strings.TrimSpace(string(body))
	if want := sha256Of(t, zip); !strings.HasPrefix(line, want) {
		t.Errorf("the checksum file says %q, want the zip's own %s", line, want)
	}
	if !strings.Contains(line, filepath.Base(zip)) {
		t.Errorf("the checksum file does not name the zip: %q", line)
	}
}

func TestS2CaskTheAppIsAdHocSignedAndAcceptedAsSuch(t *testing.T) {
	app := unzipped(t)

	out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput()

	if err != nil {
		t.Fatalf("codesign refused the bundle: %v\n%s", err, out)
	}
	details, err := exec.Command("/usr/bin/codesign", "-dv", app).CombinedOutput()
	if err != nil {
		t.Fatalf("codesign could not read the bundle: %v\n%s", err, details)
	}
	said := string(details)
	if !strings.Contains(said, "adhoc") {
		t.Errorf("the bundle is not ad-hoc signed:\n%s", said)
	}
	if !strings.Contains(said, "Identifier=dev.codingowl.desktop") {
		t.Errorf("the bundle does not carry owl's identifier:\n%s", said)
	}
}

func TestS3CaskTheAppReportsTheVersionItWasBuiltFor(t *testing.T) {
	app := unzipped(t)

	if got := plist(t, app, "CFBundleShortVersionString"); got != "0.1.0" {
		t.Errorf("the app reports version %q, want 0.1.0", got)
	}
	if got := plist(t, app, "CFBundleName"); got != "Coding Owl" {
		t.Errorf("the app is called %q, want Coding Owl", got)
	}
}

func TestS4CaskTheDesktopBuildRefusesAVersionThatIsNotOne(t *testing.T) {
	onDarwin(t)
	for _, args := range [][]string{{"0.1"}, {"nightly"}, {}} {
		dist := t.TempDir()

		res := runScript(t, repoScript("build-desktop-release.sh"), repoDir,
			[]string{"DIST=" + dist}, args...)

		if res.code == 0 {
			t.Errorf("build-desktop-release.sh %v exited 0\nstdout:\n%s", args, res.stdout)
		}
		left, err := os.ReadDir(dist)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != 0 {
			t.Errorf("build-desktop-release.sh %v wrote %d files for a version it refused", args, len(left))
		}
	}
}

func TestS5CaskPointsAtThePublishedAssetWithItsChecksum(t *testing.T) {
	cask := renderedCask(t)

	for _, want := range []string{
		`cask "coding-owl-desktop" do`,
		`version "0.1.0"`,
		`sha256 "` + testDigest + `"`,
		`url "https://github.com/vojtechmares/coding-owl/releases/download/v#{version}/CodingOwl-#{version}-arm64.zip"`,
		`app "Coding Owl.app"`,
	} {
		if !strings.Contains(cask, want) {
			t.Errorf("the cask does not carry %q:\n%s", want, cask)
		}
	}
}

func TestS6CaskBringsTheDaemonWithItAndSaysWhatItRunsOn(t *testing.T) {
	cask := renderedCask(t)

	// One daemon per machine (ADR-0010).
	if !strings.Contains(cask, `depends_on formula: "coding-owl"`) {
		t.Errorf("the cask does not bring the formula with it:\n%s", cask)
	}
	if !strings.Contains(cask, "depends_on arch: :arm64") {
		t.Errorf("the cask does not say it is arm64 only:\n%s", cask)
	}
	caveats, _, ok := strings.Cut(cask, "EOS\nend")
	if !ok {
		t.Fatalf("the cask has no caveats:\n%s", cask)
	}
	_, caveats, _ = strings.Cut(caveats, "caveats")
	for _, want := range []string{"ad-hoc signed", "notarised", "quarantine"} {
		if !strings.Contains(caveats, want) {
			t.Errorf("the caveats do not say %q:\n%s", want, caveats)
		}
	}
}

func TestS7CaskClearsTheQuarantineFlagItself(t *testing.T) {
	cask := renderedCask(t)

	step, _, ok := strings.Cut(cask, "uninstall")
	if !ok {
		t.Fatalf("the cask has no uninstall stanza to cut at:\n%s", cask)
	}
	_, step, ok = strings.Cut(step, "postflight_steps")
	if !ok {
		t.Fatalf("the cask has no postflight step:\n%s", cask)
	}
	for _, want := range []string{
		`"/usr/bin/xattr"`,
		`"-d", "-r", "com.apple.quarantine"`,
		`{{appdir}}/Coding Owl.app`,
	} {
		if !strings.Contains(step, want) {
			t.Errorf("the postflight step does not carry %q:\n%s", want, step)
		}
	}
	// An install that has already put the app in place must not be undone
	// because the flag could not be cleared.
	if !strings.Contains(step, "must_succeed: false") {
		t.Errorf("the postflight step aborts the install when it fails:\n%s", step)
	}
}

func TestS8CaskIsCommittedAndPushedToTheTap(t *testing.T) {
	tp := newTap(t)
	url, hits := servedAsset(t, http.StatusOK)

	res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, "0.1.0", testDigest, url))

	if res.code != 0 {
		t.Fatalf("bump-cask.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if *hits == 0 {
		t.Errorf("the asset was never checked before the cask was written")
	}
	body := tp.bareGit(t, "show", tp.branch+":Casks/coding-owl-desktop.rb")
	if !strings.Contains(body, `version "0.1.0"`) {
		t.Errorf("the tap's remote does not carry the cask for 0.1.0:\n%s", body)
	}
	msg := tp.bareGit(t, "log", "-1", "--format=%B", tp.branch)
	if !strings.Contains(msg, "Signed-off-by:") {
		t.Errorf("the tap commit is not signed off:\n%s", msg)
	}
	if !strings.Contains(msg, "cask") || !strings.Contains(msg, "0.1.0") {
		t.Errorf("the commit does not say what it bumped:\n%s", msg)
	}
}

func TestS9CaskAtTheSameVersionChangesNothing(t *testing.T) {
	tp := newTap(t)
	url, _ := servedAsset(t, http.StatusOK)
	if res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, "0.1.0", testDigest, url)); res.code != 0 {
		t.Fatalf("the first bump exited %d\n%s", res.code, res.stderr)
	}
	before := tp.bareGit(t, "rev-parse", tp.branch)

	res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, "0.1.0", testDigest, url))

	if res.code != 0 {
		t.Fatalf("the second bump exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if after := tp.bareGit(t, "rev-parse", tp.branch); after != before {
		t.Errorf("the tap moved from %s to %s with nothing to change",
			strings.TrimSpace(before), strings.TrimSpace(after))
	}
}

func TestS10CaskThatWasNeverPushedIsPushedNextTime(t *testing.T) {
	tp := newTap(t)
	url, _ := servedAsset(t, http.StatusOK)
	// A remote that is not there is a push that fails after the commit.
	tp.git(t, "remote", "set-url", "origin", filepath.Join(tp.dir, "..", "gone.git"))
	if res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, "0.1.0", testDigest, url)); res.code == 0 {
		t.Fatalf("bump-cask.sh reported success although the push could not work:\n%s", res.stdout)
	}
	tp.git(t, "remote", "set-url", "origin", tp.bare)

	res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, "0.1.0", testDigest, url))

	if res.code != 0 {
		t.Fatalf("the second bump exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	if body := tp.bareGit(t, "show", tp.branch+":Casks/coding-owl-desktop.rb"); !strings.Contains(body, `version "0.1.0"`) {
		t.Errorf("the tap's remote still does not carry the cask:\n%s", body)
	}
}

func TestS11CaskIsNotWrittenForAnAssetThatIsNotThere(t *testing.T) {
	tp := newTap(t)
	url, _ := servedAsset(t, http.StatusNotFound)

	res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, "0.1.0", testDigest, url))

	if res.code == 0 {
		t.Fatalf("bump-cask.sh exited 0 for an asset that answers 404\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "download") {
		t.Errorf("stderr does not say the asset cannot be downloaded:\n%s", res.stderr)
	}
	if _, err := os.Stat(tp.cask()); err == nil {
		t.Errorf("the cask was written anyway")
	}
}

func TestS12CaskRefusesAVersionOrChecksumThatIsNotOne(t *testing.T) {
	for _, bad := range []struct{ version, sha, what string }{
		{"nightly", testDigest, "version"},
		{"0.1", testDigest, "version"},
		{"0.1.0", "not-a-digest", "checksum"},
		// Hex, but not a sha256: a digest of the wrong length is a digest of
		// something else.
		{"0.1.0", testDigest[:32], "short checksum"},
		{"0.1.0", testDigest + "00", "long checksum"},
		{"", testDigest, "missing version"},
		{"0.1.0", "", "missing checksum"},
	} {
		tp := newTap(t)
		url, _ := servedAsset(t, http.StatusOK)

		res := runScript(t, repoScript("bump-cask.sh"), repoDir, caskEnv(tp, bad.version, bad.sha, url))

		if res.code == 0 {
			t.Errorf("bump-cask.sh accepted a bad %s\nstdout:\n%s", bad.what, res.stdout)
		}
		if strings.TrimSpace(res.stderr) == "" {
			t.Errorf("bump-cask.sh said nothing about the bad %s", bad.what)
		}
		if _, err := os.Stat(tp.cask()); err == nil {
			t.Errorf("a bad %s still wrote a cask", bad.what)
		}
	}
}

// jobHeader is a job's own line: two spaces, a name, a colon, nothing else. A
// job's text runs until the next one.
var jobHeader = regexp.MustCompile(`^  [A-Za-z0-9_-]+:$`)

// jobOf is one job out of the workflow, from its name to the next job. Cutting
// to the end of the file instead would let one job's text answer for another's,
// which is how a job that was never going to run read as one that was.
func jobOf(t *testing.T, workflow, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(workflow, "\n  "+name+":\n")
	if !ok {
		t.Fatalf("the workflow has no %s job:\n%s", name, workflow)
	}
	lines := strings.Split(rest, "\n")
	for at, line := range lines {
		if jobHeader.MatchString(line) {
			return strings.Join(lines[:at], "\n")
		}
	}
	return rest
}

// stepName is a step's own line, whatever order its keys are written in.
var stepName = regexp.MustCompile(`(?m)^      - (name|uses|run):`)

// stepOf is one step of a job, from its name to the next step. Cutting by
// where a key happens to sit inside a step would fail a workflow that is right
// and merely written in another order.
func stepOf(t *testing.T, job, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(job, "      - name: "+name+"\n")
	if !ok {
		t.Fatalf("the job has no %q step:\n%s", name, job)
	}
	if at := stepName.FindStringIndex(rest); at != nil {
		return rest[:at[0]]
	}
	return rest
}

// job is one job of the release workflow.
func job(t *testing.T, name string) string {
	t.Helper()
	return jobOf(t, readFile(t, filepath.Join(repoDir, ".github", "workflows", "release.yml")), name)
}

func TestS13CaskAndArchiveShipFromOneRelease(t *testing.T) {
	// The app can only be built on the platform it runs on, and nothing runs
	// before the tag is checked.
	desktop := job(t, "desktop")
	for _, want := range []string{"runs-on: macos", "needs: guard", "build-desktop-release.sh"} {
		if !strings.Contains(desktop, want) {
			t.Errorf("the desktop job does not carry %q:\n%s", want, desktop)
		}
	}
	// One release carries both, so a user cannot install an app whose daemon
	// was never published.
	release := job(t, "release")
	for _, want := range []string{
		"needs: [guard, desktop]",
		"download-artifact",
		// Against the bytes it is about to publish: the zip and its digest
		// travelled from the other job separately.
		`-c "$ZIP.sha256"`,
	} {
		if !strings.Contains(release, want) {
			t.Errorf("the release job does not carry %q:\n%s", want, release)
		}
	}
	// What is published, rather than what is merely mentioned somewhere in the
	// job: the zip and the checksums have to be arguments of the release.
	_, publish, ok := strings.Cut(release, "gh release create")
	if !ok {
		t.Fatalf("the release job publishes nothing:\n%s", release)
	}
	publish, _, _ = strings.Cut(publish, "\n\n")
	for _, want := range []string{"CodingOwl-", "coding-owl_", "checksums.txt"} {
		if !strings.Contains(publish, want) {
			t.Errorf("the release does not carry %q:\n%s", want, publish)
		}
	}
}

func TestS14CaskOneJobPointsTheTapAtBothAndAPrereleaseAtNeither(t *testing.T) {
	tap := job(t, "tap")

	// One job, so the two cannot race each other to the tap's branch.
	for _, want := range []string{"bump-formula.sh", "bump-cask.sh"} {
		if !strings.Contains(tap, want) {
			t.Errorf("the tap job does not render %q:\n%s", want, tap)
		}
	}
	workflow := readFile(t, filepath.Join(repoDir, ".github", "workflows", "release.yml"))
	if n := strings.Count(workflow, "run: ./scripts/bump-"); n != 2 {
		t.Errorf("the tap is pointed at from %d steps, want one job doing both:\n%s", n, workflow)
	}
	// The formula before the cask, which is the whole of what makes a half bump
	// safe: a newer daemon than app is the skew ADR-0004's breaking-change
	// detection exists for, and the other way round is not.
	formula := strings.Index(tap, "run: ./scripts/bump-formula.sh")
	cask := strings.Index(tap, "run: ./scripts/bump-cask.sh")
	if formula < 0 || cask < 0 || formula > cask {
		t.Errorf("the tap job does not render the formula before the cask:\n%s", tap)
	}
	// Each is given the checksum of the thing it points at, rather than of the
	// other one: swapped, both would fail their sha256 on install.
	for _, want := range []struct{ step, sha string }{
		{"Render and push the formula", "${{ needs.release.outputs.sha256 }}"},
		{"Render and push the cask", "${{ needs.desktop.outputs.sha256 }}"},
	} {
		step := stepOf(t, tap, want.step)
		if !strings.Contains(step, want.sha) {
			t.Errorf("the %q step is not given %s:\n%s", want.step, want.sha, step)
		}
	}
	// And the job waits for both of the jobs those come from.
	if !strings.Contains(tap, "needs: [guard, desktop, release]") {
		t.Errorf("the tap job does not wait for the app and the release:\n%s", tap)
	}
	if !strings.Contains(tap, "HOMEBREW_TAP_TOKEN") {
		t.Errorf("the tap job has no token to push with:\n%s", tap)
	}
	// And nothing reaches the tap for a prerelease.
	if !strings.Contains(tap, "if: needs.release.outputs.prerelease == 'false'") {
		t.Errorf("the tap job is not skipped for a prerelease:\n%s", tap)
	}
}

func TestS15CaskIsReadByHomebrewWithoutComplaint(t *testing.T) {
	onSmoke(t)
	tp := newTap(t)
	if res := runScript(t, repoScript("bump-cask.sh"), repoDir,
		caskEnv(tp, "0.1.0", testDigest, "", "DRY_RUN=1")); res.code != 0 {
		t.Fatalf("bump-cask.sh exited %d\n%s", res.code, res.stderr)
	}

	out, err := exec.Command("brew", "style", tp.cask()).CombinedOutput()

	if err != nil {
		t.Errorf("brew style refused the cask: %v\n%s", err, out)
	}
}

func TestS16CaskInstallsTheAppUnquarantinedAndTakesItAway(t *testing.T) {
	onSmoke(t)
	dist, res := builtDesktopRelease(t)
	if res.code != 0 {
		t.Fatalf("build-desktop-release.sh exited %d\n%s", res.code, res.stderr)
	}
	zip, _ := desktopZip(dist)
	// The cask Owl publishes, with two lines changed so it can be installed
	// here: it points at the zip on disk rather than at a release that does not
	// exist yet, and it does not bring the daemon, which would install the
	// formula on this machine. What it points at is S5's and what it brings is
	// S6's; this scenario is about where the app lands and what is on it.
	cask := renderedCaskFor(t, "0.1.0", sha256Of(t, zip))
	cask = replaceLine(t, cask, "  url ", `  url "file://`+zip+`"`)
	cask = replaceLine(t, cask, `  depends_on formula:`, "")
	// Homebrew installs casks from taps and from nowhere else, so the scenario
	// makes one of its own and takes it away again.
	name := installedFrom(t, cask)
	// An applications directory of its own, so the scenario does not put
	// anything in the one the person using this machine sees.
	appdir := t.TempDir()
	brew := func(args ...string) ([]byte, error) {
		cmd := exec.Command("brew", args...)
		cmd.Env = append(os.Environ(), "HOMEBREW_CASK_OPTS=--appdir="+appdir)
		return cmd.CombinedOutput()
	}

	out, err := brew("install", "--cask", name)

	if err != nil {
		t.Fatalf("brew install refused the cask: %v\n%s\ncask:\n%s", err, out, cask)
	}
	t.Cleanup(func() { _, _ = brew("uninstall", "--cask", name) })
	app := filepath.Join(appdir, "Coding Owl.app")
	if _, err := os.Stat(app); err != nil {
		t.Fatalf("the app is not where the cask put it: %v\n%s", err, out)
	}
	// Gatekeeper will not launch a quarantined bundle it cannot verify, which
	// is what the postflight step is for.
	got, _ := exec.Command("/usr/bin/xattr", "-p", "com.apple.quarantine", app).CombinedOutput()
	if flag := strings.TrimSpace(string(got)); flag != "" && !strings.Contains(flag, "No such xattr") {
		t.Errorf("the installed app is still quarantined: %s", flag)
	}

	if out, err := brew("uninstall", "--cask", name); err != nil {
		t.Fatalf("brew uninstall refused the cask: %v\n%s", err, out)
	}
	if _, err := os.Stat(app); err == nil {
		t.Errorf("the app is still installed after uninstalling it")
	}
}

// installedFrom puts the cask in a tap of its own and returns the name brew
// installs it by. The tap goes away with the scenario.
func installedFrom(t *testing.T, cask string) string {
	t.Helper()
	const tapName = "owlsmoke/cask"
	root, err := exec.Command("brew", "--repository", tapName).Output()
	if err != nil {
		t.Fatalf("brew --repository %s: %v", tapName, err)
	}
	where := strings.TrimSpace(string(root))
	// A tap left behind by an interrupted run would make the next one fail on
	// a name that already exists. brew untap refuses while something from it is
	// installed, so the directory goes either way.
	// Only ever this tap: a RemoveAll built from a name brew did not give back
	// is not something to run in somebody's Homebrew prefix.
	if !strings.HasSuffix(where, filepath.Join("owlsmoke", "homebrew-cask")) {
		t.Fatalf("brew --repository %s is %q, which is not the tap this scenario makes", tapName, where)
	}
	takeAway := func() {
		// The cask goes before the tap it came from: brew untap refuses while
		// something from it is installed, and an install left behind would
		// make the next run think there is nothing to do.
		_ = exec.Command("brew", "uninstall", "--cask", "--force", tapName+"/coding-owl-desktop").Run()
		_ = exec.Command("brew", "untap", tapName).Run()
		_ = os.RemoveAll(where)
	}
	takeAway()
	if out, err := exec.Command("brew", "tap-new", "--no-git", tapName).CombinedOutput(); err != nil {
		t.Fatalf("brew tap-new: %v\n%s", err, out)
	}
	t.Cleanup(takeAway)
	casks := filepath.Join(where, "Casks")
	if err := os.MkdirAll(casks, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casks, "coding-owl-desktop.rb"), []byte(cask), 0o644); err != nil {
		t.Fatal(err)
	}
	return tapName + "/coding-owl-desktop"
}

// renderedCaskFor is the cask as the script writes it for one version and
// checksum, without committing.
func renderedCaskFor(t *testing.T, version, sha string) string {
	t.Helper()
	tp := newTap(t)
	res := runScript(t, repoScript("bump-cask.sh"), repoDir,
		caskEnv(tp, version, sha, "", "DRY_RUN=1"))
	if res.code != 0 {
		t.Fatalf("bump-cask.sh exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	body, err := os.ReadFile(tp.cask())
	if err != nil {
		t.Fatalf("no rendered cask: %v", err)
	}
	return string(body)
}

// replaceLine swaps the one line starting with prefix for with, failing if
// there is not exactly one.
func replaceLine(t *testing.T, body, prefix, with string) string {
	t.Helper()
	lines := strings.Split(body, "\n")
	found := 0
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			out = append(out, line)
			continue
		}
		found++
		if with != "" {
			out = append(out, with)
		}
	}
	if found != 1 {
		t.Fatalf("%d lines start with %q, want one:\n%s", found, prefix, body)
	}
	return strings.Join(out, "\n")
}

func TestS17CaskTheWorkflowIsOneActionsWillRun(t *testing.T) {
	// A workflow only runs on a tag, where a mistake is found by the release
	// failing. This reads it beforehand: an expression naming a job the job it
	// is in does not depend on is exactly what it catches.
	if _, err := exec.LookPath("actionlint"); err != nil {
		t.Skip("actionlint is not installed; CI runs the same check on every push")
	}
	workflows, err := filepath.Glob(filepath.Join(repoDir, ".github", "workflows", "*.yml"))
	if err != nil || len(workflows) == 0 {
		t.Fatalf("no workflows to read: %v", err)
	}

	out, err := exec.Command("actionlint", workflows...).CombinedOutput()

	if err != nil {
		t.Errorf("actionlint refused the workflows: %v\n%s", err, out)
	}
	// And CI reads them too, so this holds for whoever has not installed it.
	ci := readFile(t, filepath.Join(repoDir, ".github", "workflows", "ci.yml"))
	if !regexp.MustCompile(`(?m)^\s+run: \|?\s*\n?\s*.*actionlint`).MatchString(ci) {
		t.Errorf("ci does not run actionlint over the workflows:\n%s", ci)
	}
}
