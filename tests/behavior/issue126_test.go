package behavior_test

// Behavior tests for issue #126. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-126.md. They drive the built owl binary against a
// daemon started as a subprocess, writing the answers the command asks for
// onto its standard input ahead of time, which is how an interactive command
// is exercised without a terminal.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The two places owl project setup will write, relative to the Project and to
// the configuration home (ADR-0014 forms 1 and 4).
const inRepoConfig = ".coding-owl.yaml"

// setupAnswers are the answers to the two questions, in the order they are
// asked: which Account, then where the file goes. They are written as the
// numbers beside the choices.
const (
	firstChoice  = "1\n"
	secondChoice = "2\n"
)

// configHomeConfig is the fallback file for a Project of that name.
func configHomeConfig(l *layout, project string) string {
	return filepath.Join(l.config, "coding-owl", project, "config.yaml")
}

// setup runs owl project setup for a Project, answering it with input, and
// returns what it printed.
func setup(t *testing.T, l *layout, project, input string) result {
	t.Helper()
	return runOwlStdin(t, l, input, "project", "setup", project)
}

// mustSetup is setup for the scenarios where it has to succeed.
func mustSetup(t *testing.T, l *layout, project, input string) result {
	t.Helper()
	res := setup(t, l, project, input)
	if res.code != 0 {
		t.Fatalf("owl project setup %s exited %d\nstdout:\n%s\nstderr:\n%s",
			project, res.code, res.stdout, res.stderr)
	}
	return res
}

// configured is a daemon with one Account and one registered Project that has
// no configuration file anywhere, which is what every scenario here starts
// from. It returns the Project's repository.
func configured(t *testing.T, l *layout, accounts ...string) *repo {
	t.Helper()
	daemonUp(t, l)
	for _, name := range accounts {
		addAccount(t, l, name, testToken)
	}
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	return r
}

// settings reads a configuration file written by setup and returns its lines,
// without the blank ones, so a scenario can say what it holds and what it does
// not.
func settings(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var out []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, strings.TrimSpace(ln))
		}
	}
	return out
}

// wantSettings fails unless the file holds exactly the apiVersion and the
// Account, which is all setup writes.
func wantSettings(t *testing.T, path, account string) {
	t.Helper()
	got := settings(t, path)
	want := []string{"apiVersion: codingowl.dev/v1", "account: " + account}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s holds\n%s\nwant\n%s", path, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// shownConfig is what owl project show reports for a Project, by line.
func shownConfig(t *testing.T, l *layout, project, key string) string {
	t.Helper()
	return line(t, mustOwl(t, l, "project", "show", project).stdout, key)
}

func TestS1SetupWritesTheInRepoFileAndSaysItIsNotInForceYet(t *testing.T) {
	l := newLayout(t)
	r := configured(t, l, "work")

	res := mustSetup(t, l, "api", firstChoice+firstChoice)

	wantSettings(t, filepath.Join(r.dir, inRepoConfig), "work")
	if !strings.Contains(res.stdout, "commit") {
		t.Errorf("setup does not say the file has to be committed:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "base branch") {
		t.Errorf("setup does not say where the file has to be committed to:\n%s", res.stdout)
	}
	// The working tree is not where an in-repo file is read from (ADR-0014),
	// so nothing has changed for the daemon yet.
	if got := shownConfig(t, l, "api", "config"); got != "(none)" {
		t.Errorf("owl project show reports config %q before the file is committed, want (none)", got)
	}
}

func TestS2SetupWritesTheConfigHomeFallbackWhichIsInForceAtOnce(t *testing.T) {
	l := newLayout(t)
	r := configured(t, l, "work")

	mustSetup(t, l, "api", firstChoice+secondChoice)

	wantSettings(t, configHomeConfig(l, "api"), "work")
	if _, err := os.Stat(filepath.Join(r.dir, inRepoConfig)); !os.IsNotExist(err) {
		t.Errorf("setup wrote into the repository as well as the configuration home")
	}
	// Read from disk rather than from the base branch, so it counts already.
	if got := shownConfig(t, l, "api", "account"); got != "work" {
		t.Errorf("owl project show reports account %q, want work", got)
	}
}

func TestS3TheProjectCanBeNamedByAPathInsideIt(t *testing.T) {
	l := newLayout(t)
	r := configured(t, l, "work")
	deeper := filepath.Join(r.dir, "sub", "deeper")
	if err := os.MkdirAll(deeper, 0o755); err != nil {
		t.Fatal(err)
	}

	mustSetup(t, l, deeper, firstChoice+secondChoice)

	wantSettings(t, configHomeConfig(l, "api"), "work")
}

func TestS4AProjectThatIsAlreadyConfiguredIsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	r := newRepo(t, l, "api")
	r.commit(inRepoConfig, "apiVersion: codingowl.dev/v1\nbranchPrefix: root/\n", "configure owl")
	addProject(t, l, r)

	res := setup(t, l, "api", firstChoice+firstChoice)

	if res.code == 0 {
		t.Fatalf("setup on a configured project exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, inRepoConfig) {
		t.Errorf("stderr does not name the file already in force:\n%s", res.stderr)
	}
	if got := shownConfig(t, l, "api", "branch prefix"); got != "root/" {
		t.Errorf("the configuration in force changed to %q", got)
	}
}

func TestS5SetupWithNoAccountIsRefusedAndSaysWhatToDo(t *testing.T) {
	l := newLayout(t)
	r := configured(t, l)

	res := setup(t, l, "api", firstChoice+firstChoice)

	if res.code == 0 {
		t.Fatalf("setup with no account exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "owl account add") {
		t.Errorf("stderr does not name owl account add:\n%s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(r.dir, inRepoConfig)); !os.IsNotExist(err) {
		t.Errorf("setup wrote a file although it had no account to write")
	}
}

func TestS6SetupWithNoInputFailsRatherThanHanging(t *testing.T) {
	l := newLayout(t)
	r := configured(t, l, "work")

	// Driven directly rather than through the helper, because the point of
	// the scenario is that it ends at all: a command that waited forever
	// would hang the test rather than fail it.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, owlBin, "project", "setup", "api")
	cmd.Env = l.env
	cmd.Stdin = strings.NewReader("")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()

	if ctx.Err() != nil {
		t.Fatalf("owl project setup is still waiting for input that will never come")
	}
	if err == nil {
		t.Fatalf("setup with no input exited 0\nstdout:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "answer") {
		t.Errorf("stderr does not say it needed an answer:\n%s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(r.dir, inRepoConfig)); !os.IsNotExist(err) {
		t.Errorf("setup wrote a file although it was never answered")
	}
}

func TestS7AnAnswerThatIsNotOneOfTheChoicesIsAskedAgain(t *testing.T) {
	l := newLayout(t)
	r := configured(t, l, "work", "personal")

	res := mustSetup(t, l, "api", "9\n"+choiceOf(t, l, "work")+firstChoice)

	if !strings.Contains(res.stdout, "9") {
		t.Errorf("setup does not say what was wrong with the answer:\n%s", res.stdout)
	}
	wantSettings(t, filepath.Join(r.dir, inRepoConfig), "work")
}

// choiceOf is the number beside an Account in the list setup prints. The
// Accounts come back in an order the scenario does not fix, so the answer is
// read off the list rather than assumed.
func choiceOf(t *testing.T, l *layout, account string) string {
	t.Helper()
	out := mustOwl(t, l, "account", "list").stdout
	var n int
	for _, ln := range strings.Split(out, "\n") {
		fields := strings.Fields(ln)
		if len(fields) == 0 || strings.HasPrefix(ln, "NAME") {
			continue
		}
		n++
		if fields[0] == account {
			return strconv.Itoa(n) + "\n"
		}
	}
	t.Fatalf("no account %q in:\n%s", account, out)
	return ""
}

func TestS8AProjectThatIsNotRegisteredIsRefused(t *testing.T) {
	l := newLayout(t)
	configured(t, l, "work")
	elsewhere := outside(t, l)

	ghost := setup(t, l, "ghost", firstChoice+firstChoice)
	stray := setup(t, l, elsewhere, firstChoice+firstChoice)

	for what, res := range map[string]result{"ghost": ghost, elsewhere: stray} {
		if res.code == 0 {
			t.Errorf("setup for %s exited 0\nstdout:\n%s", what, res.stdout)
			continue
		}
		if !strings.Contains(res.stderr, what) && !namesPath(res.stderr, what) {
			t.Errorf("setup for %s does not say what it could not find:\n%s", what, res.stderr)
		}
	}
}

func TestS9ProjectAddSaysToRunSetupWhenThereIsNoConfiguration(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")

	res := mustOwl(t, l, "project", "add", r.dir)

	if !strings.Contains(res.stdout, "registered api at ") {
		t.Errorf("project add no longer says what it registered:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "owl project setup api") {
		t.Errorf("project add does not name owl project setup for the project it registered:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "configuration") {
		t.Errorf("project add does not say no configuration file was found:\n%s", res.stdout)
	}
}

func TestS10ProjectAddSaysNothingAboutSetupWhenAlreadyConfigured(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(inRepoConfig, "apiVersion: codingowl.dev/v1\naccount: work\n", "configure owl")

	res := mustOwl(t, l, "project", "add", r.dir)

	if strings.Contains(res.stdout, "project setup") {
		t.Errorf("project add tells a configured project to run setup:\n%s", res.stdout)
	}
}

func TestS11SetupPointsAtTheDocumentationForEverythingElse(t *testing.T) {
	l := newLayout(t)
	configured(t, l, "work")

	res := mustSetup(t, l, "api", firstChoice+secondChoice)

	if !strings.Contains(res.stdout, "codingowl.dev") && !strings.Contains(res.stdout, "docs/") {
		t.Errorf("setup does not say where the rest of the configuration is documented:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "agent") {
		t.Errorf("setup does not say the rest can be filled in by a coding agent:\n%s", res.stdout)
	}
}
