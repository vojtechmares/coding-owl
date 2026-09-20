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

// prompts are every question owl setup can put, by the words it puts them in.
// A scenario that says nothing was asked is checked against these rather than
// against a question mark, which none of them happens to carry.
var setupPrompts = []string{"Write it now", "Path to claude", "Name for this account", "Paste the token"}

// questionsIn is the first question an output holds, and empty for an output
// that asked nothing.
func questionsIn(out string) string {
	for _, prompt := range setupPrompts {
		if strings.Contains(out, prompt) {
			return prompt
		}
	}
	return ""
}

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

	// Each of the three reported as the thing it is, not merely named
	// somewhere in the output: the closing lines mention an account too.
	for _, want := range []string{
		"daemon: running",
		"claude: " + filepath.Join(fakeClaudeDir, "claude"),
		"account: work",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("owl setup does not report %q:\n%s", want, res.stdout)
		}
	}
	if asked := questionsIn(res.stdout); asked != "" {
		t.Errorf("owl setup asked %q although nothing was missing:\n%s", asked, res.stdout)
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
	// And names what would start one. Which command that is depends on the
	// machine, and this owl is in a temporary directory rather than a
	// Homebrew cellar, so it is the launch agent on macOS and running the
	// daemon yourself anywhere else.
	starts := "owl daemon run"
	if runtime.GOOS == "darwin" {
		starts = "owl daemon install"
	}
	if !strings.Contains(whole, starts) {
		t.Errorf("owl setup does not name %s as what starts a daemon:\n%s", starts, whole)
	}
	// The claude check needs no daemon, so it still happened - and found the
	// one on the PATH it was given, rather than saying it found none.
	if !strings.Contains(whole, "claude: "+filepath.Join(fakeClaudeDir, "claude")) {
		t.Errorf("owl setup does not report the claude it would find:\n%s", whole)
	}
	if !strings.Contains(whole, "`owl setup` again") {
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

	// Each refused path named in the refusal of it. Counting the words of one
	// refusal would not do: a single one already carries the reason twice,
	// its own and the one it wraps.
	for _, refused := range []string{l.root, missing} {
		if !strings.Contains(res.stdout, refused+" is not") {
			t.Errorf("owl setup does not say what was wrong with %s:\n%s", refused, res.stdout)
		}
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

	// The name echoed back, and what is wrong with it: a name refused without
	// a reason is a user typing it again.
	for _, want := range []string{"not a name", "is not one Owl can use"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("owl setup does not say %q about the name it refused:\n%s", want, res.stdout)
		}
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

	if asked := questionsIn(res.stdout); asked != "" {
		t.Errorf("the second owl setup asked %q:\n%s", asked, res.stdout)
	}
	// And reports all three, rather than saying only that it is done.
	for _, want := range []string{
		"daemon: running",
		"claude: " + filepath.Join(fakeClaudeDir, "claude"),
		"account: work",
	} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("the second owl setup does not report %q:\n%s", want, res.stdout)
		}
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

func TestS13AQuestionSkippedLeavesTheCommandUnfinished(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)

	// An empty line is the answer that skips the question, and the input does
	// not run out: what ends the command is the thing left undone.
	res := setupRun(t, l, "", claudeNowhere(), "\n")

	if res.code == 0 {
		t.Fatalf("owl setup exited 0 although claude is still nowhere it will be found\nstdout:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout, "everything Owl needs") {
		t.Errorf("owl setup says the machine is ready although claude was skipped:\n%s", res.stdout)
	}
	whole := res.stdout + res.stderr
	if !strings.Contains(whole, "claude") {
		t.Errorf("owl setup does not name claude as what is still missing:\n%s", whole)
	}
	if got := readFile(t, daemonConfig(l)); strings.Contains(got, "claudePath") {
		t.Errorf("owl setup wrote claudePath although the question was skipped:\n%s", got)
	}
}

func TestS14AMachineWithNoDaemonConfigurationGetsOne(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	// The daemon is up and has read its file already, so taking it away now
	// leaves owl setup with the first-time user's case: nothing to edit.
	if err := os.Remove(daemonConfig(l)); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(fakeClaudeDir, "claude")

	mustSetupRun(t, l, claudeNowhere(), claude+"\n")

	got := readFile(t, daemonConfig(l))
	for _, want := range []string{"apiVersion: codingowl.dev/v1", "claudePath: " + claude} {
		if !strings.Contains(got, want) {
			t.Errorf("the configuration owl setup wrote does not carry %q:\n%s", want, got)
		}
	}
	// Read back by the same parser a daemon reads it with: a file that would
	// be refused at startup is not a file setup may write.
	again := mustSetupRun(t, l, claudeNowhere(), "")
	if !strings.Contains(again.stdout, "claude: "+claude) {
		t.Errorf("owl setup does not report the claude its own file names:\n%s", again.stdout)
	}
}
