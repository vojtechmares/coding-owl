package behavior_test

// Behavior tests for issue #125. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-125.md. They drive the built owl binary with the
// answers written onto its standard input ahead of time.
//
// None of them answers yes to the offer to write a launch agent, and none
// should be made to: owl daemon install runs `launchctl bootstrap`, which
// reaches the real login session whatever HOME says. S3 is the scenario that
// checks declining leaves the machine alone.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// daemonConfig is the daemon's own file, which is where claudePath is written
// (ADR-0014).
func daemonConfig(l *layout) string {
	return filepath.Join(l.config, "coding-owl", "config.yaml")
}

// setupRun runs owl setup with input on its standard input, under the binary
// at bin and the PATH in path. An empty bin is the one the harness built.
//
// The PATH is this command's own, not the daemon's. The lookup for claude is
// one owl setup does in its own process, so a scenario can take claude away
// from it without taking git away from the daemon.
func setupRun(t *testing.T, l *layout, bin, path, input string) result {
	t.Helper()
	if bin == "" {
		bin = owlBin
	}
	cmd := exec.Command(bin, "setup")
	cmd.Env = l.withoutEnv("PATH").withEnv("PATH=" + path).env
	cmd.Stdin = strings.NewReader(input)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running owl setup: %v", err)
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// mustSetupRun is setupRun for the scenarios where it has to succeed.
func mustSetupRun(t *testing.T, l *layout, path, input string) result {
	t.Helper()
	res := setupRun(t, l, "", path, input)
	if res.code != 0 {
		t.Fatalf("owl setup exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	return res
}

// claudeOnPath is a PATH the stub agent is on, so the lookup finds it exactly
// where the daemon would. claudeNowhere is one it is not on; a layout's home is
// a temporary one, so `~/.local/bin` under it is empty too, which is the other
// place the lookup goes.
func claudeOnPath() string {
	return fakeClaudeDir + string(os.PathListSeparator) + fakeMachineDir
}

func claudeNowhere() string { return fakeMachineDir }

// launchAgents are the launch agent files under a layout's home, which must
// stay empty: no scenario here asks for one to be written.
func launchAgents(t *testing.T, l *layout) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(l.home, "Library", "LaunchAgents", "*"))
	if err != nil {
		t.Fatalf("looking for launch agents: %v", err)
	}
	return found
}

func TestS1SetupOnAMachineAlreadySetUpAsksNothing(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	before := readFile(t, daemonConfig(l))

	res := mustSetupRun(t, l, claudeOnPath(), "")

	for _, want := range []string{"daemon", "claude", "account"} {
		if !strings.Contains(strings.ToLower(res.stdout), want) {
			t.Errorf("owl setup says nothing about the %s:\n%s", want, res.stdout)
		}
	}
	if strings.Contains(res.stdout, "?") {
		t.Errorf("owl setup asked something although nothing was missing:\n%s", res.stdout)
	}
	if got := readFile(t, daemonConfig(l)); got != before {
		t.Errorf("owl setup wrote the daemon's configuration although nothing was missing:\nbefore\n%s\nafter\n%s", before, got)
	}
}

func TestS2SetupWithNoDaemonDoesWhatItCanAndSaysWhatIsLeft(t *testing.T) {
	l := newLayout(t)

	res := setupRun(t, l, "", claudeOnPath(), "n\n")

	if res.code == 0 {
		t.Fatalf("owl setup with no daemon exited 0\nstdout:\n%s", res.stdout)
	}
	whole := res.stdout + res.stderr
	if !strings.Contains(whole, "not running") {
		t.Errorf("owl setup does not say the daemon is not running:\n%s", whole)
	}
	// The claude check needs no daemon, so it still happened.
	if !strings.Contains(whole, "claude") {
		t.Errorf("owl setup skipped the claude check although it needs no daemon:\n%s", whole)
	}
	if !strings.Contains(whole, "owl setup") {
		t.Errorf("owl setup does not say to run it again once the daemon is up:\n%s", whole)
	}
}

func TestS3TheOfferToWriteALaunchAgentIsAnOffer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the launch agent offer is macOS's")
	}
	l := newLayout(t)

	res := setupRun(t, l, "", claudeOnPath(), "n\n")

	if res.code == 0 {
		t.Fatalf("owl setup with no daemon exited 0\nstdout:\n%s", res.stdout)
	}
	if found := launchAgents(t, l); len(found) != 0 {
		t.Fatalf("owl setup wrote %v although the offer was declined", found)
	}
	whole := res.stdout + res.stderr
	if !strings.Contains(whole, "owl daemon install") {
		t.Errorf("owl setup does not name owl daemon install as what would do it:\n%s", whole)
	}
}

func TestS4AnOwlFromHomebrewIsToldToUseBrewServices(t *testing.T) {
	l := newLayout(t)
	// An owl whose own path is inside a Cellar, reached through the symlink
	// Homebrew puts on PATH, which is how a Homebrew install looks.
	cellar := filepath.Join(l.root, "brew", "Cellar", "coding-owl", "0.1.0", "bin")
	binDir := filepath.Join(l.root, "brew", "bin")
	for _, dir := range []string{cellar, binDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body, err := os.ReadFile(owlBin)
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(cellar, "owl")
	if err := os.WriteFile(real, body, 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(binDir, "owl")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}

	res := setupRun(t, l, linked, claudeOnPath(), "n\n")

	whole := res.stdout + res.stderr
	if !strings.Contains(whole, "brew services start coding-owl") {
		t.Errorf("owl setup does not name brew services for a Homebrew install:\n%s", whole)
	}
	if strings.Contains(whole, "owl daemon install") {
		t.Errorf("owl setup offered a launch agent to a Homebrew install:\n%s", whole)
	}
	if found := launchAgents(t, l); len(found) != 0 {
		t.Errorf("owl setup wrote %v", found)
	}
}

func TestS5AClaudeTheDaemonWouldNotFindIsAskedForAndWrittenDown(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	before := readFile(t, daemonConfig(l))
	claude := filepath.Join(fakeClaudeDir, "claude")

	res := mustSetupRun(t, l, claudeNowhere(), claude+"\n")

	got := readFile(t, daemonConfig(l))
	if !strings.Contains(got, "claudePath: "+claude) {
		t.Errorf("the daemon's configuration does not carry the path given:\n%s", got)
	}
	// Everything that was in the file before is still in it.
	for _, ln := range strings.Split(strings.TrimSpace(before), "\n") {
		if ln = strings.TrimSpace(ln); ln == "" {
			continue
		}
		if !strings.Contains(got, ln) {
			t.Errorf("the daemon's configuration lost %q:\n%s", ln, got)
		}
	}
	if !strings.Contains(res.stdout, "restart") {
		t.Errorf("owl setup does not say the daemon has to be restarted:\n%s", res.stdout)
	}
}

func TestS6APathThatIsNotAProgramIsRefusedAndAskedAgain(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	claude := filepath.Join(fakeClaudeDir, "claude")
	missing := filepath.Join(l.root, "nowhere", "claude")

	res := mustSetupRun(t, l, claudeNowhere(), l.root+"\n"+missing+"\n"+claude+"\n")

	if strings.Count(res.stdout, "is not") < 2 {
		t.Errorf("owl setup does not say what was wrong with each refused path:\n%s", res.stdout)
	}
	if got := readFile(t, daemonConfig(l)); !strings.Contains(got, "claudePath: "+claude) {
		t.Errorf("the daemon's configuration does not carry the path that was usable:\n%s", got)
	}
}

func TestS7AClaudeTheDaemonWouldFindIsReportedAndNothingIsWritten(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	before := readFile(t, daemonConfig(l))

	res := mustSetupRun(t, l, claudeOnPath(), "")

	if !strings.Contains(res.stdout, filepath.Join(fakeClaudeDir, "claude")) {
		t.Errorf("owl setup does not name where it found claude:\n%s", res.stdout)
	}
	if got := readFile(t, daemonConfig(l)); got != before {
		t.Errorf("owl setup changed the daemon's configuration:\nbefore\n%s\nafter\n%s", before, got)
	}
}

func TestS8SetupWithNoAccountAsksForANameAndRegistersOne(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)

	mustSetupRun(t, l, claudeOnPath(), "work\n"+testToken+"\n")

	if out := mustOwl(t, l, "account", "list").stdout; !strings.Contains(out, "work") {
		t.Errorf("owl account list does not show the account setup registered:\n%s", out)
	}
}

func TestS9ANameThatIsNotANameIsRefusedAndAskedAgain(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)

	res := mustSetupRun(t, l, claudeOnPath(), "not a name\nwork\n"+testToken+"\n")

	if !strings.Contains(res.stdout, "not a name") && !strings.Contains(res.stderr, "not a name") {
		t.Errorf("owl setup does not say what was wrong with the name:\n%s%s", res.stdout, res.stderr)
	}
	out := mustOwl(t, l, "account", "list").stdout
	if !strings.Contains(out, "work") {
		t.Errorf("owl account list does not show work:\n%s", out)
	}
}

func TestS10RunningSetupAgainWhenEverythingIsDoneChangesNothing(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	mustSetupRun(t, l, claudeOnPath(), "work\n"+testToken+"\n")
	first := mustOwl(t, l, "account", "list").stdout

	res := mustSetupRun(t, l, claudeOnPath(), "")

	if strings.Contains(res.stdout, "?") {
		t.Errorf("the second owl setup asked something:\n%s", res.stdout)
	}
	if got := mustOwl(t, l, "account", "list").stdout; got != first {
		t.Errorf("the accounts changed:\nbefore\n%s\nafter\n%s", first, got)
	}
}

func TestS11SetupEndsBySayingWhatComesNext(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)

	res := mustSetupRun(t, l, claudeOnPath(), "")

	if !strings.Contains(res.stdout, "owl project add") {
		t.Errorf("owl setup does not say what comes next:\n%s", res.stdout)
	}
}

func TestS12SetupWithNoInputEndsRatherThanHanging(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, owlBin, "setup")
	cmd.Env = l.withoutEnv("PATH").withEnv("PATH=" + claudeNowhere()).env
	cmd.Stdin = strings.NewReader("")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()

	if ctx.Err() != nil {
		t.Fatal("owl setup is still waiting for input that will never come")
	}
	if err == nil {
		t.Fatalf("owl setup with no input exited 0\nstdout:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "answer") {
		t.Errorf("stderr does not say it needed an answer:\n%s", errb.String())
	}
	if _, err := os.Stat(daemonConfig(l)); err == nil {
		if got := readFile(t, daemonConfig(l)); strings.Contains(got, "claudePath") {
			t.Errorf("owl setup wrote claudePath although it was never answered:\n%s", got)
		}
	}
}
