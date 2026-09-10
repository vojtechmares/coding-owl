package behavior_test

// Behavior tests for issue #7. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-7.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// configure Verification checks on the Project's base branch.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// checkRow is one check as owl jobs show reports it.
type checkRow struct {
	name   string
	result string
	why    string
	output string
}

// checkedProject registers a Project whose base branch carries this
// configuration, and queues one Job that runs in a single Run against it.
func checkedProject(t *testing.T, l *layout, config string, flags ...string) *repo {
	t.Helper()
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", config, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work", append([]string{"--no-plan"}, flags...)...)
	return r
}

// checks parses the checks section of owl jobs show, keyed by check name.
func checks(t *testing.T, out string) map[string]checkRow {
	t.Helper()
	_, rest, ok := strings.Cut(out, "checks:\n")
	if !ok {
		return nil
	}
	rows := map[string]checkRow{}
	var current string
	for _, ln := range strings.Split(rest, "\n") {
		switch {
		case strings.HasPrefix(ln, "- "):
			name, verdict, ok := strings.Cut(strings.TrimPrefix(ln, "- "), ": ")
			if !ok {
				t.Fatalf("cannot read the check row %q in:\n%s", ln, out)
			}
			result, why, _ := strings.Cut(verdict, " (")
			rows[name] = checkRow{name: name, result: result, why: strings.TrimSuffix(why, ")")}
			current = name
		case strings.HasPrefix(ln, "    "):
			row := rows[current]
			row.output += strings.TrimPrefix(ln, "    ") + "\n"
			rows[current] = row
		case strings.TrimSpace(ln) == "":
			// A blank line may be part of a check's output; the section ends
			// at the next heading.
		default:
			return rows
		}
	}
	return rows
}

// wantCheck fails unless a check was reported with that result.
func wantCheck(t *testing.T, rows map[string]checkRow, name, result string) checkRow {
	t.Helper()
	row, ok := rows[name]
	if !ok {
		t.Fatalf("no check named %q was reported; got %v", name, rows)
	}
	if row.result != result {
		t.Errorf("check %s = %q (%s), want %s", name, row.result, row.why, result)
	}
	return row
}

// finishedJob runs the Job at the head of the queue to the end of one Run and
// returns what owl jobs show says about it.
func finishedJob(t *testing.T, l *layout) (out, job string) {
	t.Helper()
	run, job := startRun(t, l)
	waitRun(t, l, job, run)
	return mustOwl(t, l, "jobs", "show", job).stdout, job
}

// threeChecks is the configuration S1, S6, S11 and S15 share: a failing check,
// a passing one, and another failing one, so that a later check plainly runs
// after an earlier one has failed.
const threeChecks = `apiVersion: codingowl.dev/v1
checks:
  - name: first
    run: echo first is unhappy; exit 1
  - name: second
    run: "true"
  - name: third
    run: echo third is unhappy; exit 1
`

func TestS1VerifyEveryCheckRunsAfterOneHasFailed(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, threeChecks)

	out, _ := finishedJob(t, l)

	rows := checks(t, out)
	if len(rows) != 3 {
		t.Fatalf("owl jobs show reports %d checks, want 3:\n%s", len(rows), out)
	}
	wantCheck(t, rows, "first", "failed")
	wantCheck(t, rows, "second", "passed")
	wantCheck(t, rows, "third", "failed")
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
}

func TestS2VerifyAllChecksPassingLandsTheJobInReview(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: build
    run: "true"
  - name: test
    run: exit 0
`)

	out, _ := finishedJob(t, l)

	rows := checks(t, out)
	wantCheck(t, rows, "build", "passed")
	wantCheck(t, rows, "test", "passed")
	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS3VerifyAProjectWithNoChecksIsLeftAsItWas(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
	if rows := checks(t, out); len(rows) != 0 {
		t.Errorf("owl jobs show reports checks for a project that configures none: %v", rows)
	}
}

func TestS4VerifyEmptyOutputFailsACheckThatPrints(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: fmt
    run: echo untidy.go
    expect: empty_output
  - name: quiet
    run: "true"
    expect: empty_output
`)

	out, _ := finishedJob(t, l)

	rows := checks(t, out)
	fmtRow := wantCheck(t, rows, "fmt", "failed")
	if !strings.Contains(fmtRow.why, "output") {
		t.Errorf("why fmt failed = %q, does not mention the output it printed", fmtRow.why)
	}
	if !strings.Contains(fmtRow.output, "untidy.go") {
		t.Errorf("fmt's output = %q, want what it printed", fmtRow.output)
	}
	wantCheck(t, rows, "quiet", "passed")
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
}

func TestS5VerifyAHungCheckIsKilledAtItsTimeout(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: hang
    run: sleep 60
    timeout: 1s
`)

	started := time.Now()
	out, _ := finishedJob(t, l)

	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Errorf("the run took %s, so the check was not killed at its timeout", elapsed)
	}
	row := wantCheck(t, checks(t, out), "hang", "failed")
	if !strings.Contains(row.why, "timeout") && !strings.Contains(row.why, "timed out") {
		t.Errorf("why hang failed = %q, does not name the timeout", row.why)
	}
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
}

func TestS6VerifyABlockedJobShowsEveryFailingCheckAndItsOutput(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, threeChecks)

	out, _ := finishedJob(t, l)

	rows := checks(t, out)
	first := wantCheck(t, rows, "first", "failed")
	third := wantCheck(t, rows, "third", "failed")
	if !strings.Contains(first.output, "first is unhappy") {
		t.Errorf("first's output = %q, want what it printed", first.output)
	}
	if !strings.Contains(third.output, "third is unhappy") {
		t.Errorf("third's output = %q, want what it printed", third.output)
	}
}

func TestS7VerifySetupRunsInTheWorktreeBeforeTheAgent(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
setup:
  - touch prepared.txt
`)

	out, _ := finishedJob(t, l)

	if entries := s.invoked(t).Entries; !slices.Contains(entries, "prepared.txt") {
		t.Errorf("the agent's working directory held %v, want the file setup made", entries)
	}
	worktree := line(t, out, "worktree")
	if _, err := os.Stat(filepath.Join(worktree, "prepared.txt")); err != nil {
		t.Errorf("the job's worktree does not hold what setup made: %v", err)
	}
}

func TestS8VerifyASetupFailureBlocksTheJobBeforeAnyRun(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
setup:
  - echo cannot prepare; exit 1
`)

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start exited 0 with a failing setup command\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "cannot prepare") && !strings.Contains(res.stderr, "exit 1") {
		t.Errorf("stderr does not name the setup command that failed:\n%s", res.stderr)
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if got := line(t, out, "runs"); got != "none" {
		t.Errorf("runs = %q, want none: setup failed before any run", got)
	}
	if _, err := os.Stat(s.argv); err == nil {
		t.Error("the stub agent was invoked even though setup failed")
	}
}

func TestS9VerifyChecksComeFromTheBaseBranchNotTheWorktree(t *testing.T) {
	// The Agent rewrites the configuration in its own worktree to have no
	// checks at all; the Run is judged by what the base branch says.
	l, _ := writingLayout(t,
		map[string]string{".coding-owl.yaml": "apiVersion: codingowl.dev/v1\n"}, true, agentScript)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: guard
    run: exit 1
`)

	out, _ := finishedJob(t, l)

	wantCheck(t, checks(t, out), "guard", "failed")
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
}

func TestS10VerifyChecksRunAfterExecutionNotAfterPlanning(t *testing.T) {
	l, _ := writingLayout(t, map[string]string{handoffPath: planText}, true, agentScript)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", `apiVersion: codingowl.dev/v1
checks:
  - name: guard
    run: exit 1
`, "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	planned, _ := finishedJob(t, l)

	if got := line(t, planned, "state"); got != "pending" {
		t.Errorf("state after planning = %q, want pending", got)
	}
	if rows := checks(t, planned); len(rows) != 0 {
		t.Errorf("checks ran after a planning run: %v", rows)
	}

	executed, _ := finishedJob(t, l)

	wantCheck(t, checks(t, executed), "guard", "failed")
	if got := line(t, executed, "state"); got != "blocked" {
		t.Errorf("state after execution = %q, want blocked", got)
	}
}

func TestS11VerifyAFailedCheckKeepsTheAgentsWork(t *testing.T) {
	l, _ := writingLayout(t, map[string]string{"work.txt": "the agent's work\n"}, true, agentScript)
	daemonUp(t, l)
	r := checkedProject(t, l, threeChecks)

	out, _ := finishedJob(t, l)

	branch := line(t, out, "branch")
	if got := r.git("show", branch+":work.txt"); got != "the agent's work\n" {
		t.Errorf("the agent's commit is not on %s: %q", branch, got)
	}
	rows := runRows(t, out)
	if len(rows) != 1 || rows[0].outcome != "succeeded" {
		t.Fatalf("runs = %+v, want one succeeded run: the agent did its part", rows)
	}
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want the job blocked by verification", got)
	}
}

func TestS12VerifyAnUnreadableCheckIsRefusedBeforeTheAgentStarts(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: nameless
`)

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start exited 0 with an unreadable check\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, ".coding-owl.yaml") {
		t.Errorf("stderr does not name the configuration file:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr, "nameless") && !strings.Contains(res.stderr, "run") {
		t.Errorf("stderr does not say what is wrong with the check:\n%s", res.stderr)
	}
	if _, err := os.Stat(s.argv); err == nil {
		t.Error("the stub agent was invoked despite the unreadable check")
	}
	wantQueue(t, l, "1|api|pending|work")
}

func TestS13VerifyACheckWithoutATimeoutStillEnds(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: quick
    run: "true"
`)

	out, _ := finishedJob(t, l)

	wantCheck(t, checks(t, out), "quick", "passed")
	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS14VerifyChecksRunInTheJobsWorktree(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := checkedProject(t, l, `apiVersion: codingowl.dev/v1
setup:
  - touch prepared.txt
checks:
  - name: where
    run: test -f prepared.txt
`)

	out, _ := finishedJob(t, l)

	wantCheck(t, checks(t, out), "where", "passed")
	if _, err := os.Stat(filepath.Join(r.dir, "prepared.txt")); err == nil {
		t.Error("the project's own checkout holds what setup made in the worktree")
	}
}

func TestS15VerifyTheReasonNamesTheChecksThatRefusedTheJob(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	checkedProject(t, l, threeChecks)

	out, _ := finishedJob(t, l)

	reason := line(t, out, "reason")
	if !strings.Contains(reason, "first") || !strings.Contains(reason, "third") {
		t.Errorf("reason = %q, does not name the checks that refused the job", reason)
	}
}
