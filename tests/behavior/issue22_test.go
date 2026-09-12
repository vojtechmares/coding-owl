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

func TestS13CaskAndArchiveShipFromOneRelease(t *testing.T) {
	workflow := readFile(t, filepath.Join(repoDir, ".github", "workflows", "release.yml"))

	// The app can only be built on the platform it runs on, and nothing runs
	// before the tag is checked.
	desktop, _, ok := strings.Cut(workflow, "\n  release:")
	if !ok {
		t.Fatalf("the workflow has no release job:\n%s", workflow)
	}
	_, desktop, ok = strings.Cut(desktop, "\n  desktop:")
	if !ok {
		t.Fatalf("the workflow has no desktop job:\n%s", workflow)
	}
	if !strings.Contains(desktop, "runs-on: macos") {
		t.Errorf("the desktop job does not run on a mac:\n%s", desktop)
	}
	if !strings.Contains(desktop, "needs: guard") {
		t.Errorf("the desktop job is not guarded by the tag check:\n%s", desktop)
	}
	if !strings.Contains(desktop, "build-desktop-release.sh") {
		t.Errorf("the desktop job does not build the release:\n%s", desktop)
	}
	// One release carries both, so a user cannot install an app whose daemon
	// was never published.
	release, _, ok := strings.Cut(workflow, "\n  cask:")
	if !ok {
		t.Fatalf("the workflow has no cask job:\n%s", workflow)
	}
	_, release, _ = strings.Cut(release, "\n  release:")
	for _, want := range []string{"needs: [guard, desktop]", "CodingOwl-", "checksums.txt"} {
		if !strings.Contains(release, want) {
			t.Errorf("the release job does not carry %q:\n%s", want, release)
		}
	}
	_, cask, ok := strings.Cut(workflow, "\n  cask:")
	if !ok {
		t.Fatalf("the workflow has no cask job:\n%s", workflow)
	}
	for _, want := range []string{"needs: [guard, release]", "bump-cask.sh", "HOMEBREW_TAP_TOKEN"} {
		if !strings.Contains(cask, want) {
			t.Errorf("the cask job does not carry %q:\n%s", want, cask)
		}
	}
}

func TestS14CaskAPrereleasePointsTheTapAtNeither(t *testing.T) {
	workflow := readFile(t, filepath.Join(repoDir, ".github", "workflows", "release.yml"))

	const skip = "if: needs.release.outputs.prerelease == 'false'"
	if n := strings.Count(workflow, skip); n != 2 {
		t.Errorf("%d jobs are skipped for a prerelease, want the formula and the cask:\n%s", n, workflow)
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
