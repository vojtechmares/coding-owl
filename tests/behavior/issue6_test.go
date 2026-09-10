package behavior_test

// Behavior tests for issue #6. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-6.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// script that stub to leave a handoff behind the way a planning Agent does.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// handoffPath is where a Job's handoff lives on its branch (ADR-0026).
const handoffPath = ".coding-owl/HANDOFF.md"

// planText and stepTwo are what a scripted planning or execution Agent writes
// into the handoff.
const (
	planText = "step one: read the tests\n"
	stepTwo  = "step two: fix the parser\n"
)

// writingLayout returns a layout whose stub agent writes these files into its
// working directory before it emits its script, committing them when asked.
func writingLayout(t *testing.T, files map[string]string, commit bool, script []string) (*layout, *stub) {
	t.Helper()
	l, s := agentLayout(t, script, 0)
	spec, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"OWL_FAKE_CLAUDE_WRITE=" + string(spec)}
	if commit {
		env = append(env, "OWL_FAKE_CLAUDE_COMMIT=1")
	}
	return l.withEnv(env...), s
}

// planningLayout is writingLayout for an Agent that writes and commits the
// handoff, which is what a planning Run is expected to do.
func planningLayout(t *testing.T, plan string) (*layout, *stub) {
	t.Helper()
	return writingLayout(t, map[string]string{handoffPath: plan}, true, agentScript)
}

// waitRun waits for one Run of a Job to end and returns its row.
func waitRun(t *testing.T, l *layout, job, run string) runRow {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		out := mustOwl(t, l, "jobs", "show", job).stdout
		for _, row := range runRows(t, out) {
			if row.id == run && row.outcome != "running" {
				return row
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s of job %s had not ended after 20s:\n%s", run, job,
		mustOwl(t, l, "jobs", "show", job).stdout)
	return runRow{}
}

// phase runs one phase of the Job at the head of the queue and returns the row
// of the Run it was.
func phase(t *testing.T, l *layout) (runRow, string) {
	t.Helper()
	run, job := startRun(t, l)
	return waitRun(t, l, job, run), job
}

// jobState is the state owl jobs show reports for a Job.
func jobState(t *testing.T, l *layout, job string) string {
	t.Helper()
	return line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state")
}

// prompt is the text the Agent was given, which is the last of its arguments.
func prompt(t *testing.T, inv invocation) string {
	t.Helper()
	if len(inv.Argv) == 0 {
		t.Fatal("the stub agent recorded no arguments")
	}
	return inv.Argv[len(inv.Argv)-1]
}

// globalConfig writes the daemon's own configuration file (ADR-0014).
func globalConfig(t *testing.T, l *layout, body string) {
	t.Helper()
	dir := filepath.Join(l.config, "coding-owl")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// committedHandoff is the handoff as it stands on the Job's branch.
func committedHandoff(t *testing.T, r *repo, job, branch string) string {
	t.Helper()
	return r.git("show", branch+":"+handoffPath)
}

func TestS1PlanRunsBeforeExecute(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	runnableJob(t, l, "fix the flaky test")

	planned, job := phase(t, l)
	between := jobState(t, l, job)
	executed, _ := phase(t, l)

	if planned.phase != "plan" || executed.phase != "execute" {
		t.Errorf("phases are %q then %q, want plan then execute", planned.phase, executed.phase)
	}
	if between != "pending" {
		t.Errorf("between the runs the job was %q, want pending", between)
	}
	if got := jobState(t, l, job); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
	if rows := runRows(t, mustOwl(t, l, "jobs", "show", job).stdout); len(rows) != 2 {
		t.Errorf("owl jobs show reports %d runs, want 2", len(rows))
	}
}

func TestS2PlanNoPlanGoesStraightToExecution(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")

	executed, job := phase(t, l)

	if executed.phase != "execute" {
		t.Errorf("phase = %q, want execute", executed.phase)
	}
	if got := jobState(t, l, job); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
	if rows := runRows(t, mustOwl(t, l, "jobs", "show", job).stdout); len(rows) != 1 {
		t.Errorf("owl jobs show reports %d runs, want 1", len(rows))
	}
}

func TestS3PlanBothFlagsAreRefused(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	r := project(t, l, "api")

	res := runOwlIn(t, l, r.dir, "add", "work", "--plan", "--no-plan")

	if res.code == 0 {
		t.Fatalf("owl add with both flags exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "--plan") || !strings.Contains(res.stderr, "--no-plan") {
		t.Errorf("stderr does not name both flags:\n%s", res.stderr)
	}
	wantEmptyQueue(t, l)
}

func TestS4PlanIsCommittedAsTheHandoffAndPrinted(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	r := runnableJob(t, l, "work")

	_, job := phase(t, l)

	branch := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "branch")
	if got := committedHandoff(t, r, job, branch); got != planText {
		t.Errorf("the handoff on %s = %q, want %q", branch, got, planText)
	}
	out := mustOwl(t, l, "jobs", "show", job).stdout
	_, plan, ok := strings.Cut(out, "plan:\n")
	if !ok {
		t.Fatalf("owl jobs show prints no plan section:\n%s", out)
	}
	if !strings.Contains(plan, strings.TrimSpace(planText)) {
		t.Errorf("the plan section does not hold the plan:\n%s", out)
	}
}

func TestS5PlanOwlCommitsAHandoffTheAgentLeftBehind(t *testing.T) {
	l, _ := writingLayout(t, map[string]string{handoffPath: planText}, false, agentScript)
	daemonUp(t, l)
	r := runnableJob(t, l, "work")

	_, job := phase(t, l)

	out := mustOwl(t, l, "jobs", "show", job).stdout
	branch := line(t, out, "branch")
	if got := committedHandoff(t, r, job, branch); got != planText {
		t.Errorf("the handoff on %s = %q, want the plan owl committed for it", branch, got)
	}
	worktree := line(t, out, "worktree")
	if status := gitIn(t, r, worktree, "status", "--porcelain"); strings.Contains(status, "HANDOFF.md") {
		t.Errorf("the worktree still reports the handoff as uncommitted:\n%s", status)
	}
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job pending again for its execution run", got)
	}
}

func TestS6PlanWithoutAHandoffBlocksTheJob(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := phase(t, l)

	out := mustOwl(t, l, "jobs", "show", job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, handoffPath) {
		t.Errorf("reason = %q, does not name the handoff", reason)
	}
	if res := mustOwl(t, l, "start"); !strings.Contains(res.stdout, "nothing pending") {
		t.Errorf("owl start ran something after a blocked planning run:\n%s", res.stdout)
	}
}

func TestS7PlanExecutionCarriesTheHandoffInAFreshInvocation(t *testing.T) {
	l, s := planningLayout(t, planText)
	daemonUp(t, l)
	runnableJob(t, l, "fix the flaky test")

	phase(t, l)
	phase(t, l)

	invs := s.invocations(t)
	if len(invs) != 2 {
		t.Fatalf("the agent was invoked %d times, want once per phase", len(invs))
	}
	execution := prompt(t, invs[1])
	for _, want := range []string{strings.TrimSpace(planText), handoffPath, "fix the flaky test"} {
		if !strings.Contains(execution, want) {
			t.Errorf("the execution prompt does not carry %q:\n%s", want, execution)
		}
	}
	for _, inv := range invs {
		for _, forbidden := range []string{"--resume", "--continue"} {
			if inv.has(forbidden) {
				t.Errorf("the agent was invoked with %s: %v", forbidden, inv.Argv)
			}
		}
	}
	if out := mustOwl(t, l, "jobs", "show", "1").stdout; strings.Contains(strings.ToLower(out), "session") {
		t.Errorf("owl jobs show reports a session, which nothing should keep:\n%s", out)
	}
}

func TestS8PlanASecondExecutionReadsTheUpdatedHandoff(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := writingLayout(t, map[string]string{handoffPath: stepTwo}, true, script)
	p := daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")
	_, job := startRun(t, l)

	stopDaemon(t, p)
	daemonUp(t, l)

	if got := jobState(t, l, job); got != "pending" {
		t.Fatalf("state after the interruption = %q, want the job pending again", got)
	}
	row, again := phase(t, l)
	if again != job {
		t.Fatalf("owl start ran job %s, want the interrupted job %s", again, job)
	}
	if row.phase != "execute" {
		t.Errorf("phase = %q, want execute", row.phase)
	}
	invs := s.invocations(t)
	if got := prompt(t, invs[len(invs)-1]); !strings.Contains(got, strings.TrimSpace(stepTwo)) {
		t.Errorf("the prompt does not carry the handoff the first run left:\n%s", got)
	}
}

func TestS9PlanAHandoffEditedByHandIsWhatTheNextRunReads(t *testing.T) {
	l, s := planningLayout(t, planText)
	daemonUp(t, l)
	r := runnableJob(t, l, "work")
	_, job := phase(t, l)
	worktree := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "worktree")

	const edited = "changed by the user\n"
	if err := os.WriteFile(filepath.Join(worktree, handoffPath), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, r, worktree, "add", "--", handoffPath)
	gitIn(t, r, worktree, "commit", "-m", "steer the job")

	phase(t, l)

	execution := prompt(t, s.invocations(t)[1])
	if !strings.Contains(execution, strings.TrimSpace(edited)) {
		t.Errorf("the execution prompt does not carry the edited handoff:\n%s", execution)
	}
	if strings.Contains(execution, strings.TrimSpace(planText)) {
		t.Errorf("the execution prompt still carries what the planning run wrote:\n%s", execution)
	}
}

func TestS10PlanModelAndEffortDefaultPerPhase(t *testing.T) {
	l, s := planningLayout(t, planText)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	phase(t, l)
	phase(t, l)

	invs := s.invocations(t)
	if len(invs) != 2 {
		t.Fatalf("the agent was invoked %d times, want once per phase", len(invs))
	}
	wantFlag(t, invs[0], "--model", "opus")
	wantFlag(t, invs[0], "--effort", "xhigh")
	wantFlag(t, invs[1], "--model", "opus")
	wantFlag(t, invs[1], "--effort", "high")
}

// wantFlag fails unless the invocation carries that flag with that value.
func wantFlag(t *testing.T, inv invocation, name, want string) {
	t.Helper()
	if got := inv.flag(t, name); got != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func TestS11PlanProjectConfigurationOverridesTheDefaults(t *testing.T) {
	l, s := planningLayout(t, planText)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml",
		"apiVersion: codingowl.dev/v1\nphases:\n  execute:\n    effort: medium\n", "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	phase(t, l)
	phase(t, l)

	execution := s.invocations(t)[1]
	wantFlag(t, execution, "--effort", "medium")
	wantFlag(t, execution, "--model", "opus")
}

// phasedConfig is the global and Project configuration S12 to S14 share.
func phasedConfig(t *testing.T, l *layout) *repo {
	t.Helper()
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\nphases:\n  plan:\n    model: sonnet\n  execute:\n    effort: low\n")
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml",
		"apiVersion: codingowl.dev/v1\nphases:\n  plan:\n    model: haiku\n", "configure owl")
	addProject(t, l, r)
	return r
}

func TestS12PlanGlobalThenProjectWin(t *testing.T) {
	l, s := planningLayout(t, planText)
	daemonUp(t, l)
	r := phasedConfig(t, l)
	addJob(t, l, r.dir, "work")

	phase(t, l)
	phase(t, l)

	invs := s.invocations(t)
	wantFlag(t, invs[0], "--model", "haiku")
	wantFlag(t, invs[1], "--effort", "low")
}

func TestS13PlanJobOverridesEverything(t *testing.T) {
	l, s := planningLayout(t, planText)
	daemonUp(t, l)
	r := phasedConfig(t, l)
	addJob(t, l, r.dir, "work", "--model", "sonnet", "--effort", "max")

	phase(t, l)
	phase(t, l)

	for _, inv := range s.invocations(t) {
		wantFlag(t, inv, "--model", "sonnet")
		wantFlag(t, inv, "--effort", "max")
	}
}

func TestS14PlanJobsShowPrintsTheEffectiveModelAndEffortAndItsSource(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	r := phasedConfig(t, l)
	addJob(t, l, r.dir, "work")

	out := mustOwl(t, l, "jobs", "show", "1").stdout

	rows := phaseRows(t, out)
	if len(rows) != 2 {
		t.Fatalf("owl jobs show reports %d phases, want plan and execute:\n%s", len(rows), out)
	}
	if rows[0] != "plan|haiku|project|xhigh|default" {
		t.Errorf("plan phase = %q, want haiku from the project and xhigh from the default", rows[0])
	}
	if rows[1] != "execute|opus|default|low|global" {
		t.Errorf("execute phase = %q, want opus from the default and low from the global file", rows[1])
	}
}

// phaseRows renders the phases section of owl jobs show as
// phase|model|source|effort|source rows.
func phaseRows(t *testing.T, out string) []string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "phases:\n")
	if !ok {
		t.Fatalf("owl jobs show prints no phases section:\n%s", out)
	}
	var rows []string
	for _, ln := range strings.Split(rest, "\n") {
		if strings.TrimSpace(ln) == "" {
			break
		}
		if strings.HasPrefix(ln, "PHASE") {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) != 5 {
			t.Fatalf("cannot read the phase row %q, want phase, model, its source, effort and its source:\n%s", ln, out)
		}
		rows = append(rows, strings.Join(fields, "|"))
	}
	return rows
}

func TestS15PlanRunsRecordTheirPhase(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	phase(t, l)
	phase(t, l)

	rows := runRows(t, mustOwl(t, l, "jobs", "show", "1").stdout)
	if len(rows) != 2 {
		t.Fatalf("owl jobs show reports %d runs, want 2", len(rows))
	}
	if rows[0].phase != "plan" || rows[1].phase != "execute" {
		t.Errorf("run phases are %q and %q, want plan and execute", rows[0].phase, rows[1].phase)
	}
}

func TestS16PlanABadModelIsRefusedBeforeAnAgentStarts(t *testing.T) {
	l, _ := planningLayout(t, planText)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml",
		"apiVersion: codingowl.dev/v1\nphases:\n  plan:\n    model: --oops\n", "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start exited 0 with an unusable model\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "--oops") {
		t.Errorf("stderr does not name the model it refused:\n%s", res.stderr)
	}
	out := mustOwl(t, l, "jobs", "show", "1").stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job still pending", got)
	}
	if got := line(t, out, "runs"); got != "none" {
		t.Errorf("runs = %q, want none", got)
	}
}
