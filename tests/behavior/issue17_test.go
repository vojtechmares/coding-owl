package behavior_test

// Behavior tests for issue #17. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-17.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// exercise the review a Project can ask for after its own checks.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reviewPath is where Owl asks a reviewer to leave its verdict, inside the
// Job's worktree.
const reviewPath = ".coding-owl/REVIEW.md"

// reviewedConfig is a Project that asks for a review after one passing check.
const reviewedConfig = `apiVersion: codingowl.dev/v1
checks:
  - name: build
    run: "true"
review:
  agent: true
`

// reviewing is a layout whose stub agent leaves that verdict when it is asked
// for a review, and whose execution Agent commits a file of its own.
func reviewing(t *testing.T, verdict string) (*layout, *stub) {
	t.Helper()
	l, s := writingLayout(t, map[string]string{"work.txt": "the agent's own work\n"}, true, agentScript)
	return withVerdict(t, l, verdict), s
}

// withVerdict tells the stub agent what to write when it is asked for a
// review. An empty verdict is a reviewer that leaves none.
func withVerdict(t *testing.T, l *layout, verdict string) *layout {
	t.Helper()
	if verdict == "" {
		return l
	}
	spec, err := json.Marshal(map[string]string{reviewPath: verdict})
	if err != nil {
		t.Fatal(err)
	}
	return l.withEnv("OWL_FAKE_CLAUDE_REVIEW=" + string(spec))
}

// reviewName is what the review is called in a report.
const reviewName = "agent review"

// lastArg is the prompt an invocation was given, which the Driver puts last.
func lastArg(t *testing.T, inv invocation) string {
	t.Helper()
	if len(inv.Argv) == 0 {
		t.Fatal("the stub agent recorded an invocation with no arguments")
	}
	return inv.Argv[len(inv.Argv)-1]
}

// reviewInvocation is the stub agent's invocation that was asked for a review.
func reviewInvocation(t *testing.T, s *stub) invocation {
	t.Helper()
	for _, inv := range s.invocations(t) {
		if strings.Contains(strings.Join(inv.Argv, " "), reviewPath) {
			return inv
		}
	}
	t.Fatalf("the stub agent was never asked for a review: %v", s.invocations(t))
	return invocation{}
}

func TestS1ReviewAProjectThatDoesNotAskForOneDoesNotGetOne(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: build
    run: "true"
`)

	out, _ := finishedJob(t, l)

	if got := len(s.invocations(t)); got != 1 {
		t.Errorf("the agent was invoked %d times, want once", got)
	}
	rows := checks(t, out)
	wantCheck(t, rows, "build", "passed")
	if _, ok := rows[reviewName]; ok {
		t.Errorf("a project that asked for no review got one:\n%s", out)
	}
	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS2ReviewIsASecondSeparateSession(t *testing.T) {
	l, s := reviewing(t, "verdict: pass\n\nNothing to add.\n")
	daemonUp(t, l)
	checkedProject(t, l, reviewedConfig)

	out, _ := finishedJob(t, l)

	if got := len(s.invocations(t)); got != 2 {
		t.Fatalf("the agent was invoked %d times, want twice: once to work and once to review", got)
	}
	worked, reviewed := s.invocations(t)[0], reviewInvocation(t, s)
	// The stub records the directory it was started in with its symlinks
	// resolved, which is what /var is on a mac.
	worktree, err := filepath.EvalSymlinks(line(t, out, "worktree"))
	if err != nil {
		t.Fatal(err)
	}
	if reviewed.Dir != worktree {
		t.Errorf("the reviewer ran in %s, want the job's worktree %s", reviewed.Dir, worktree)
	}
	if reviewed.Env["CLAUDE_CONFIG_DIR"] != worked.Env["CLAUDE_CONFIG_DIR"] {
		t.Errorf("the reviewer ran as %q, want the account the job runs on %q",
			reviewed.Env["CLAUDE_CONFIG_DIR"], worked.Env["CLAUDE_CONFIG_DIR"])
	}
	// Fresh is the whole point: nothing carries the session that produced the
	// work into the one that reviews it (ADR-0013).
	for _, flag := range []string{"--resume", "--continue", "--session-id", "-c", "-r"} {
		if reviewed.has(flag) {
			t.Errorf("the reviewer was invoked with %s: %v", flag, reviewed.Argv)
		}
	}
}

func TestS3ReviewIsGivenThePlanAndTheDiff(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nAdd the file and stop.\n")
	l = withVerdict(t, l, "verdict: pass\n")
	l = l.withEnv(`OWL_FAKE_CLAUDE_WRITE={"` + handoffPath + `":"# Handoff\n\nAdd the file and stop.\n","work.txt":"a line the agent added\n"}`)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", reviewedConfig, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	_, job := finishedJob(t, l)
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	prompt := lastArg(t, reviewInvocation(t, s))
	for _, want := range []string{"Add the file and stop.", "work.txt", "a line the agent added", reviewPath} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the reviewer's prompt does not carry %q:\n%s", want, prompt)
		}
	}
}

func TestS4ReviewThatPassesLeavesTheJobInReview(t *testing.T) {
	l, _ := reviewing(t, "verdict: pass\n\nThe change does what the plan said.\n")
	daemonUp(t, l)
	checkedProject(t, l, reviewedConfig)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
	rows := checks(t, out)
	wantCheck(t, rows, "build", "passed")
	row := wantCheck(t, rows, reviewName, "passed")
	if !strings.Contains(row.output, "The change does what the plan said.") {
		t.Errorf("what the reviewer wrote was not reported:\n%s", out)
	}
}

func TestS5ReviewThatFailsBlocksTheJobWithItsFindings(t *testing.T) {
	l, _ := reviewing(t, "verdict: fail\n\n1. The error from os.Rename is dropped.\n2. Nothing covers the empty input.\n")
	daemonUp(t, l)
	checkedProject(t, l, reviewedConfig)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	if got := line(t, out, "reason"); !strings.Contains(got, reviewName) {
		t.Errorf("reason = %q, want it to name the review", got)
	}
	row := wantCheck(t, checks(t, out), reviewName, "failed")
	for _, want := range []string{"os.Rename", "the empty input"} {
		if !strings.Contains(row.output, want) {
			t.Errorf("the reviewer's findings do not carry %q:\n%s", want, out)
		}
	}
}

func TestS6ReviewerThatLeavesNoVerdictBlocksTheJob(t *testing.T) {
	l, _ := reviewing(t, "")
	daemonUp(t, l)
	checkedProject(t, l, reviewedConfig)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	row := wantCheck(t, checks(t, out), reviewName, "failed")
	if !strings.Contains(row.why, "verdict") {
		t.Errorf("the review says %q, want it to say no verdict was left", row.why)
	}
}

func TestS7ReviewLeavesNothingBehindInTheWorktree(t *testing.T) {
	l, _ := reviewing(t, "verdict: pass\n\nThe change does what the plan said.\n")
	daemonUp(t, l)
	r := checkedProject(t, l, reviewedConfig)

	out, _ := finishedJob(t, l)

	worktree := line(t, out, "worktree")
	if got := gitIn(t, r, worktree, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("the worktree is not clean after a review:\n%s", got)
	}
	if got := readFile(t, filepath.Join(worktree, reviewPath)); got != "" {
		t.Errorf("the verdict file is still in the worktree:\n%s", got)
	}
}

func TestS8ACheckThatFailedStillBlocksAReviewedJob(t *testing.T) {
	l, _ := reviewing(t, "verdict: pass\n\nLooks right to me.\n")
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: test
    run: echo the tests are unhappy; exit 1
review:
  agent: true
`)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	rows := checks(t, out)
	wantCheck(t, rows, "test", "failed")
	wantCheck(t, rows, reviewName, "passed")
}

func TestS9ReviewerThatWillNotFinishIsStopped(t *testing.T) {
	l, _ := reviewing(t, "verdict: pass\n")
	l = l.withEnv("OWL_FAKE_CLAUDE_REVIEW_SLEEP=60s")
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: build
    run: "true"
review:
  agent: true
  timeout: 1s
`)

	started := time.Now()
	out, _ := finishedJob(t, l)

	if took := time.Since(started); took > 30*time.Second {
		t.Errorf("the job took %s, want the review's own timeout to end it", took)
	}
	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	row := wantCheck(t, checks(t, out), reviewName, "failed")
	if !strings.Contains(row.why, "stopped") && !strings.Contains(row.why, "timed out") {
		t.Errorf("the review says %q, want it to say it was stopped", row.why)
	}
}

func TestS10APlanningRunIsNotReviewed(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nstep one done\n")
	l = withVerdict(t, l, "verdict: fail\n\nnobody should have asked me\n")
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", reviewedConfig, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	out, _ := finishedJob(t, l)

	if got := len(s.invocations(t)); got != 1 {
		t.Errorf("the agent was invoked %d times for a planning run, want once", got)
	}
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job back in the queue to be carried out", got)
	}
	if _, ok := checks(t, out)[reviewName]; ok {
		t.Errorf("a planning run was reviewed:\n%s", out)
	}
}

func TestS11TheDesktopAppShowsTheReviewBesideTheChecks(t *testing.T) {
	l, _ := reviewing(t, "verdict: fail\n\n1. The error from os.Rename is dropped.\n")
	daemonUp(t, l)
	checkedProject(t, l, reviewedConfig)
	out, job := finishedJob(t, l)
	app, _ := desktopApp(t, l)

	d, err := app.Job(id(t, job))

	if err != nil {
		t.Fatalf("the app could not read the job: %v\n%s", err, out)
	}
	byName := map[string]string{}
	passed := map[string]bool{}
	for _, c := range d.Checks {
		byName[c.Name] = c.Output
		passed[c.Name] = c.Passed
	}
	if _, ok := byName["build"]; !ok {
		t.Errorf("the detail does not carry the project's own check: %+v", d.Checks)
	}
	findings, ok := byName[reviewName]
	if !ok {
		t.Fatalf("the detail does not carry the review: %+v", d.Checks)
	}
	if passed[reviewName] {
		t.Error("the app reports a failed review as passed")
	}
	if !strings.Contains(findings, "os.Rename") {
		t.Errorf("the app does not carry the reviewer's findings: %q", findings)
	}
}
