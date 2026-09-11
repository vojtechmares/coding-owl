package behavior_test

// Behavior tests for issue #17. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-17.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// exercise the agent Verifier a Project can ask for after its own checks.

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// verdictPath is where Owl asks the verifying Agent to leave its verdict,
// inside the Job's worktree.
const verdictPath = ".coding-owl/VERDICT.md"

// verifiedConfig is a Project that asks for an agent verifier after one
// passing check.
const verifiedConfig = `apiVersion: codingowl.dev/v1
checks:
  - name: build
    run: "true"
verification:
  agent: true
`

// verifying is a layout whose stub agent leaves that verdict when it is asked
// to judge the work, and whose execution Agent commits a file of its own.
func verifying(t *testing.T, verdict string) (*layout, *stub) {
	t.Helper()
	l, s := writingLayout(t, map[string]string{"work.txt": "the agent's own work\n"}, true, agentScript)
	return withVerdict(t, l, verdict), s
}

// withVerdict tells the stub agent what to write when it is asked to judge the
// work. An empty verdict is an Agent that leaves none.
func withVerdict(t *testing.T, l *layout, verdict string) *layout {
	t.Helper()
	if verdict == "" {
		return l
	}
	spec, err := json.Marshal(map[string]string{verdictPath: verdict})
	if err != nil {
		t.Fatal(err)
	}
	return l.withEnv("OWL_FAKE_CLAUDE_VERDICT=" + string(spec))
}

// verifierName is what the agent Verifier is called in a report.
const verifierName = "agent verifier"

// lastArg is the prompt an invocation was given, which the Driver puts last.
func lastArg(t *testing.T, inv invocation) string {
	t.Helper()
	if len(inv.Argv) == 0 {
		t.Fatal("the stub agent recorded an invocation with no arguments")
	}
	return inv.Argv[len(inv.Argv)-1]
}

// workInvocation is the stub agent's invocation that carried the Job out,
// which is the one that was not asked to judge anything.
func workInvocation(t *testing.T, s *stub) invocation {
	t.Helper()
	for _, inv := range s.invocations(t) {
		if !strings.Contains(strings.Join(inv.Argv, " "), verdictPath) {
			return inv
		}
	}
	t.Fatalf("the stub agent was only ever asked to judge the work: %v", s.invocations(t))
	return invocation{}
}

// verifyInvocation is the stub agent's invocation that was asked to judge the
// work.
func verifyInvocation(t *testing.T, s *stub) invocation {
	t.Helper()
	for _, inv := range s.invocations(t) {
		if strings.Contains(strings.Join(inv.Argv, " "), verdictPath) {
			return inv
		}
	}
	t.Fatalf("the stub agent was never asked to judge the work: %v", s.invocations(t))
	return invocation{}
}

func TestS1AProjectThatDoesNotAskForTheAgentVerifierDoesNotGetIt(t *testing.T) {
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
	if _, ok := rows[verifierName]; ok {
		t.Errorf("a project that asked for no agent verifier got one:\n%s", out)
	}
	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS2TheAgentVerifierIsASecondSeparateSession(t *testing.T) {
	l, s := verifying(t, "verdict: pass\n\nNothing to add.\n")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	if got := len(s.invocations(t)); got != 2 {
		t.Fatalf("the agent was invoked %d times, want twice: once to work and once to judge it", got)
	}
	worked, judged := workInvocation(t, s), verifyInvocation(t, s)
	// The stub records the directory it was started in with its symlinks
	// resolved, which is what /var is on a mac.
	worktree, err := filepath.EvalSymlinks(line(t, out, "worktree"))
	if err != nil {
		t.Fatal(err)
	}
	if judged.Dir != worktree {
		t.Errorf("the verifying agent ran in %s, want the job's worktree %s", judged.Dir, worktree)
	}
	if judged.Env["CLAUDE_CONFIG_DIR"] != worked.Env["CLAUDE_CONFIG_DIR"] {
		t.Errorf("the verifying agent ran as %q, want the account the job runs on %q",
			judged.Env["CLAUDE_CONFIG_DIR"], worked.Env["CLAUDE_CONFIG_DIR"])
	}
	// Fresh is the whole point: nothing carries the session that produced the
	// work into the one that judges it (ADR-0013).
	for _, flag := range []string{"--resume", "--continue", "--session-id", "-c", "-r"} {
		if judged.has(flag) {
			t.Errorf("the verifying agent was invoked with %s: %v", flag, judged.Argv)
		}
	}
}

func TestS3TheVerifyingAgentIsGivenThePlanAndTheDiff(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nAdd the file and stop.\n")
	l = withVerdict(t, l, "verdict: pass\n")
	l = l.withEnv(`OWL_FAKE_CLAUDE_WRITE={"` + handoffPath + `":"# Handoff\n\nAdd the file and stop.\n","work.txt":"a line the agent added\n"}`)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", verifiedConfig, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	_, job := finishedJob(t, l)
	run, _ := startRun(t, l)
	waitRun(t, l, job, run)

	prompt := lastArg(t, verifyInvocation(t, s))
	// The plan is quoted as the plan: the handoff it came from is also on the
	// branch, so a prompt that carries the sentence anywhere carries it twice
	// over and says nothing about what the verifying agent was told it was.
	planned := quoted(t, prompt, planFence)
	if !strings.Contains(planned, "Add the file and stop.") {
		t.Errorf("the verifying agent was not given the plan:\n%s", planned)
	}
	// The plan, not the diff quoted under the plan's name: the handoff the
	// plan came from is committed on the branch, so it is in the diff too.
	for _, notWant := range []string{"diff --git", "work.txt"} {
		if strings.Contains(planned, notWant) {
			t.Errorf("what the verifying agent was told is the plan carries %q:\n%s", notWant, planned)
		}
	}
	diff := quoted(t, prompt, diffFence)
	for _, want := range []string{"work.txt", "a line the agent added"} {
		if !strings.Contains(diff, want) {
			t.Errorf("the diff the verifying agent was given does not carry %q:\n%s", want, diff)
		}
	}
	// Where to write the verdict is Owl's own instruction, so it is outside
	// everything the prompt quotes.
	asked, _, _ := strings.Cut(prompt, planFence)
	rest := prompt[strings.LastIndex(prompt, diffFence)+len(diffFence):]
	if strings.Contains(asked, verdictPath) || !strings.Contains(rest, verdictPath) {
		t.Errorf("the prompt does not say where to write the verdict outside what it quotes:\n%s", prompt)
	}
}

// planFence and diffFence are the lines the prompt sets quoted material
// apart with, so a scenario can ask what the verifying agent was told a piece
// of text was.
const (
	planFence = "----- plan -----"
	diffFence = "----- diff -----"
)

// quoted is what a prompt set inside one fence.
func quoted(t *testing.T, prompt, fence string) string {
	t.Helper()
	_, rest, ok := strings.Cut(prompt, fence)
	if !ok {
		t.Fatalf("the prompt quotes nothing inside %q:\n%s", fence, prompt)
	}
	body, _, ok := strings.Cut(rest, fence)
	if !ok {
		t.Fatalf("the prompt never closes %q:\n%s", fence, prompt)
	}
	return body
}

func TestS4AVerdictThatPassesLeavesTheJobInReview(t *testing.T) {
	l, _ := verifying(t, "verdict: pass\n\nThe change does what the plan said.\n")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
	rows := checks(t, out)
	wantCheck(t, rows, "build", "passed")
	row := wantCheck(t, rows, verifierName, "passed")
	if !strings.Contains(row.output, "The change does what the plan said.") {
		t.Errorf("what the verifying agent wrote was not reported:\n%s", out)
	}
}

func TestS5AVerdictThatRefusesTheWorkBlocksTheJob(t *testing.T) {
	l, _ := verifying(t, "verdict: fail\n\n1. The error from os.Rename is dropped.\n2. Nothing covers the empty input.\n")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	if got := line(t, out, "reason"); !strings.Contains(got, verifierName) {
		t.Errorf("reason = %q, want it to name the agent verifier", got)
	}
	row := wantCheck(t, checks(t, out), verifierName, "failed")
	for _, want := range []string{"os.Rename", "the empty input"} {
		if !strings.Contains(row.output, want) {
			t.Errorf("the findings do not carry %q:\n%s", want, out)
		}
	}
}

func TestS6AnAgentThatLeavesNoVerdictBlocksTheJob(t *testing.T) {
	l, _ := verifying(t, "")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	row := wantCheck(t, checks(t, out), verifierName, "failed")
	if !strings.Contains(row.why, "verdict") {
		t.Errorf("the agent verifier says %q, want it to say no verdict was left", row.why)
	}
}

func TestS7TheAgentVerifierLeavesNothingBehind(t *testing.T) {
	l, _ := verifying(t, "verdict: pass\n\nThe change does what the plan said.\n")
	daemonUp(t, l)
	r := checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	worktree := line(t, out, "worktree")
	if got := gitIn(t, r, worktree, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("the worktree is not clean after the agent verifier ran:\n%s", got)
	}
	if got := readFile(t, filepath.Join(worktree, verdictPath)); got != "" {
		t.Errorf("the verdict file is still in the worktree:\n%s", got)
	}
}

func TestS8ACheckThatFailedStillBlocksAJobTheVerifierPassed(t *testing.T) {
	l, _ := verifying(t, "verdict: pass\n\nLooks right to me.\n")
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: test
    run: echo the tests are unhappy; exit 1
verification:
  agent: true
`)

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	rows := checks(t, out)
	wantCheck(t, rows, "test", "failed")
	wantCheck(t, rows, verifierName, "passed")
}

func TestS9AnAgentThatWillNotFinishIsStopped(t *testing.T) {
	l, _ := verifying(t, "verdict: pass\n")
	l = l.withEnv("OWL_FAKE_CLAUDE_VERDICT_SLEEP=60s")
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: build
    run: "true"
verification:
  agent: true
  timeout: 1s
`)

	started := time.Now()
	out, _ := finishedJob(t, l)

	if took := time.Since(started); took > 30*time.Second {
		t.Errorf("the job took %s, want the verifier's own timeout to end it", took)
	}
	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked:\n%s", got, out)
	}
	row := wantCheck(t, checks(t, out), verifierName, "failed")
	if !strings.Contains(row.why, "stopped") && !strings.Contains(row.why, "timed out") {
		t.Errorf("the agent verifier says %q, want it to say it was stopped", row.why)
	}
}

func TestS10APlanningRunIsNotJudged(t *testing.T) {
	l, s := planningLayout(t, "# Handoff\n\nstep one done\n")
	l = withVerdict(t, l, "verdict: fail\n\nnobody should have asked me\n")
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", verifiedConfig, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	out, _ := finishedJob(t, l)

	if got := len(s.invocations(t)); got != 1 {
		t.Errorf("the agent was invoked %d times for a planning run, want once", got)
	}
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job back in the queue to be carried out", got)
	}
	if _, ok := checks(t, out)[verifierName]; ok {
		t.Errorf("a planning run was reviewed:\n%s", out)
	}
}

func TestS11TheDesktopAppShowsTheFindingsBesideTheChecks(t *testing.T) {
	l, _ := verifying(t, "verdict: fail\n\n1. The error from os.Rename is dropped.\n")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)
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
	findings, ok := byName[verifierName]
	if !ok {
		t.Fatalf("the detail does not carry the agent verifier: %+v", d.Checks)
	}
	if passed[verifierName] {
		t.Error("the app reports a failed review as passed")
	}
	if !strings.Contains(findings, "os.Rename") {
		t.Errorf("the app does not carry the findings: %q", findings)
	}
}

func TestS12TheVerifyingAgentsContractIsShown(t *testing.T) {
	l, _ := verifying(t, "verdict: pass\n\nThe change does what the plan said.\n")
	daemonUp(t, l)
	checkedProject(t, l, verifiedConfig)

	out, _ := finishedJob(t, l)

	// The verifying agent is one Owl starts with a contract of its own, and
	// nothing Owl puts in front of an Agent is hidden (ADR-0017).
	shown := section(t, out, "verifier system prompt:")
	if !strings.Contains(shown, "reviewing somebody else's work") {
		t.Errorf("owl jobs show does not print the verifying agent's own contract:\n%s", out)
	}
	if !strings.Contains(section(t, out, "system prompt:"), "running unattended") {
		t.Errorf("owl jobs show no longer prints the agent's own contract:\n%s", out)
	}

	// A Project that asks for no review has none to show.
	other, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, other)
	checkedProject(t, other, "apiVersion: codingowl.dev/v1\n")
	plain, _ := finishedJob(t, other)
	if strings.Contains(plain, "verifier system prompt:") {
		t.Errorf("a project that asked for no agent verifier is shown one's contract:\n%s", plain)
	}
}
