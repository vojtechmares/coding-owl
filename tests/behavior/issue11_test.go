package behavior_test

// Behavior tests for issue #11. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-11.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be. The
// stub starts a child that appends to a heartbeat file, so a scenario can see
// whether what the Agent started is running - which is what tells a frozen
// process group apart from a frozen Agent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// beating is a layout whose Agent starts a heartbeat child and then waits, so
// a Run stays in progress for as long as a scenario needs it.
type beating struct {
	l     *layout
	stub  *stub
	beats string
}

func beatingLayout(t *testing.T, config string) *beating {
	t.Helper()
	// The Agent commits, writes a handoff, starts its child and then waits.
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := agentLayout(t, script, 0)
	beats := filepath.Join(l.root, "heartbeat")
	l = l.withEnv(
		"OWL_FAKE_CLAUDE_CHILD="+beats,
		"OWL_FAKE_CLAUDE_WRITE="+`{"work.txt":"the agent's work\n","`+handoffPath+`":"# Handoff\n\nhalf way through the work\n"}`,
		"OWL_FAKE_CLAUDE_COMMIT=1",
	)
	if config != "" {
		globalConfig(t, l, config)
	}
	return &beating{l: l, stub: s, beats: beats}
}

// beats counts the heartbeats written so far.
func (b *beating) count(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(b.beats)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "beat\n")
}

// beating waits until the child has written something, so a scenario freezes a
// group that is demonstrably running.
func (b *beating) started(t *testing.T) {
	t.Helper()
	waitFor(t, "the agent's child to start beating", func() bool { return b.count(t) > 0 })
}

// still reports whether the heartbeat stopped: two reads a moment apart with
// nothing in between.
func (b *beating) still(t *testing.T) bool {
	t.Helper()
	before := b.count(t)
	sleep(200 * time.Millisecond)
	return b.count(t) == before
}

// growing waits for the heartbeat to grow again, which is what a released
// process group does.
func (b *beating) growing(t *testing.T) {
	t.Helper()
	before := b.count(t)
	waitFor(t, "the agent's child to beat again", func() bool { return b.count(t) > before })
}

// sleep is a pause the test itself takes, where waiting for a condition would
// be waiting for nothing to happen.
func sleep(d time.Duration) { time.Sleep(d) }

// frozen starts a Run and freezes it, returning the run and job ids.
func (b *beating) frozen(t *testing.T) (run, job string) {
	t.Helper()
	r := project(t, b.l, "api")
	addJob(t, b.l, r.dir, "work", "--no-plan")
	run, job = startRun(t, b.l)
	b.started(t)

	res := mustOwl(t, b.l, "pause")
	if !strings.Contains(res.stdout, run) {
		t.Fatalf("owl pause does not name the run it froze:\n%s", res.stdout)
	}
	return run, job
}

func TestS1PauseFreezesTheWholeProcessGroup(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	run, job := b.frozen(t)

	if !b.still(t) {
		t.Errorf("the agent's child is still beating after owl pause")
	}
	// Still frozen a moment later: a group that was continued by accident
	// would have started again by now.
	if !b.still(t) {
		t.Errorf("the agent's child started beating again on its own")
	}
	row := runRowOf(t, b.l, job, run)
	if row.outcome != "running" && row.outcome != "paused" {
		t.Errorf("run outcome = %q, want a run still in progress", row.outcome)
	}
	b.stub.let(t)
	mustOwl(t, b.l, "resume")
}

func TestS2ResumeContinuesTheSameRun(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	run, job := b.frozen(t)
	if !b.still(t) {
		t.Fatalf("the agent's child is still beating after owl pause")
	}

	res := mustOwl(t, b.l, "resume")

	if !strings.Contains(res.stdout, run) {
		t.Errorf("owl resume does not name the run it continued:\n%s", res.stdout)
	}
	b.growing(t)
	b.stub.let(t)
	row := waitRun(t, b.l, job, run)
	if row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want succeeded: it was continued, not ended", row.outcome)
	}
	if got := line(t, mustOwl(t, b.l, "jobs", "show", job).stdout, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS3StatusReportsAFrozenRun(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	b.frozen(t)

	row := runningRow(t, mustOwl(t, b.l, "status").stdout)

	if !contains(row, "paused") {
		t.Errorf("owl status does not report the run as paused: %v", row)
	}
	mustOwl(t, b.l, "resume")
	row = runningRow(t, mustOwl(t, b.l, "status").stdout)
	if contains(row, "paused") {
		t.Errorf("owl status still reports the run as paused after resume: %v", row)
	}
	b.stub.let(t)
}

func TestS4PauseWithNothingRunningSaysSo(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)

	res := runOwl(t, b.l, "pause")

	if res.code == 0 {
		t.Fatalf("owl pause exited 0 with nothing running\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "no run in progress") {
		t.Errorf("stderr does not say there is nothing to pause:\n%s", res.stderr)
	}
}

func TestS5ResumeWithNothingFrozenSaysSo(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	r := project(t, b.l, "api")
	addJob(t, b.l, r.dir, "work", "--no-plan")
	run, job := startRun(t, b.l)
	b.started(t)

	res := runOwl(t, b.l, "resume")

	if res.code == 0 {
		t.Fatalf("owl resume exited 0 for a run that was never paused\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "not paused") {
		t.Errorf("stderr does not say the run is not paused:\n%s", res.stderr)
	}
	b.stub.let(t)
	waitRun(t, b.l, job, run)
}

func TestS6PausingAFrozenRunSaysSo(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	run, job := b.frozen(t)

	res := runOwl(t, b.l, "pause")

	if res.code == 0 {
		t.Fatalf("owl pause exited 0 for a run that was already frozen\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "already paused") {
		t.Errorf("stderr does not say the run is already paused:\n%s", res.stderr)
	}
	if !b.still(t) {
		t.Errorf("the refused pause let the run go again")
	}
	mustOwl(t, b.l, "resume")
	b.growing(t)
	b.stub.let(t)
	waitRun(t, b.l, job, run)
}

func TestS7GraceWindowEndsAFrozenRun(t *testing.T) {
	b := beatingLayout(t, "apiVersion: codingowl.dev/v1\ngraceWindow: 1s\n")
	daemonUp(t, b.l)
	run, job := b.frozen(t)

	row := waitRun(t, b.l, job, run)

	if row.outcome != "interrupted" {
		t.Errorf("run outcome = %q, want interrupted", row.outcome)
	}
	if got := line(t, mustOwl(t, b.l, "jobs", "show", job).stdout, "state"); got != "pending" {
		t.Errorf("state = %q, want pending: an interrupted job goes back in the queue", got)
	}
	if !b.still(t) {
		t.Errorf("the agent's child is still beating after the grace window ended the run")
	}
}

func TestS8TerminatedRunKeepsItsWork(t *testing.T) {
	b := beatingLayout(t, "apiVersion: codingowl.dev/v1\ngraceWindow: 1s\n")
	daemonUp(t, b.l)
	r := project(t, b.l, "api")
	addJob(t, b.l, r.dir, "work", "--no-plan")
	run, job := startRun(t, b.l)
	b.started(t)
	out := mustOwl(t, b.l, "jobs", "show", job).stdout
	worktree, branch := line(t, out, "worktree"), line(t, out, "branch")
	mustOwl(t, b.l, "pause")

	waitRun(t, b.l, job, run)

	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the job's worktree is gone: %v", err)
	}
	if !hasBranch(t, r, branch) {
		t.Errorf("the job's branch %s is gone", branch)
	}
	if got := r.git("show", branch+":work.txt"); got != "the agent's work\n" {
		t.Errorf("the work the agent committed before it was frozen is gone: %q", got)
	}
}

func TestS9NextRunContinuesFromTheHandoff(t *testing.T) {
	b := beatingLayout(t, "apiVersion: codingowl.dev/v1\ngraceWindow: 1s\n")
	daemonUp(t, b.l)
	run, job := b.frozen(t)
	waitRun(t, b.l, job, run)
	before := mustOwl(t, b.l, "jobs", "show", job).stdout

	again, sameJob := startRun(t, b.l)

	if sameJob != job {
		t.Fatalf("owl start began job %s, want the interrupted job %s", sameJob, job)
	}
	if again == run {
		t.Errorf("the new run has the id of the one that was interrupted, %s", again)
	}
	// The Agent records how it was called before it does anything else, but
	// owl start returns as soon as it has started.
	waitFor(t, "the second agent to record how it was called", func() bool {
		return len(b.stub.invocations(t)) >= 2
	})
	asked := strings.Join(b.stub.invoked(t).Argv, " ")
	if !strings.Contains(asked, "half way through the work") {
		t.Errorf("the agent was not given what the handoff said:\n%s", asked)
	}
	after := mustOwl(t, b.l, "jobs", "show", job).stdout
	for _, key := range []string{"branch", "worktree"} {
		if line(t, before, key) != line(t, after, key) {
			t.Errorf("the job's %s changed between runs: %q then %q", key, line(t, before, key), line(t, after, key))
		}
	}
	mustOwl(t, b.l, "pause")
}

func TestS10GraceWindowIsConfigurable(t *testing.T) {
	b := beatingLayout(t, "apiVersion: codingowl.dev/v1\ngraceWindow: 3s\n")
	daemonUp(t, b.l)
	run, job := b.frozen(t)

	sleep(time.Second)

	if row := runRowOf(t, b.l, job, run); row.outcome != "running" && row.outcome != "paused" {
		t.Errorf("run outcome = %q one second into a three second window, want it still in progress", row.outcome)
	}
	if row := waitRun(t, b.l, job, run); row.outcome != "interrupted" {
		t.Errorf("run outcome = %q once the window passed, want interrupted", row.outcome)
	}
}

func TestS11AGraceWindowThatIsNotADurationIsRefused(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	r := project(t, b.l, "api")
	addJob(t, b.l, r.dir, "work", "--no-plan")
	run, job := startRun(t, b.l)
	b.started(t)
	// Written once the Run is going: the daemon reads its configuration when
	// it needs it, so this is what owl pause finds.
	globalConfig(t, b.l, "apiVersion: codingowl.dev/v1\ngraceWindow: soon\n")

	res := runOwl(t, b.l, "pause")

	if res.code == 0 {
		t.Fatalf("owl pause accepted a grace window that is not a duration\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"config.yaml", "graceWindow"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not name %q:\n%s", want, res.stderr)
		}
	}
	// Still running, not frozen: the refusal came before anything was
	// signalled.
	b.growing(t)

	globalConfig(t, b.l, "apiVersion: codingowl.dev/v1\n")
	b.stub.let(t)
	if row := waitRun(t, b.l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want succeeded: the refused pause left it alone", row.outcome)
	}
}

func TestS12ResumingInsideTheWindowKeepsTheRun(t *testing.T) {
	b := beatingLayout(t, "apiVersion: codingowl.dev/v1\ngraceWindow: 30s\n")
	daemonUp(t, b.l)
	run, job := b.frozen(t)

	res := mustOwl(t, b.l, "resume")

	if !strings.Contains(res.stdout, run) {
		t.Errorf("owl resume continued a different run:\n%s", res.stdout)
	}
	b.stub.let(t)
	if row := waitRun(t, b.l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want succeeded: the window did not end it", row.outcome)
	}
}

func TestS13DaemonRestartEndsTheRunsThatWereGoing(t *testing.T) {
	b := beatingLayout(t, "")
	d := daemonUp(t, b.l)
	r := project(t, b.l, "api")
	addJob(t, b.l, r.dir, "work", "--no-plan")
	run, job := startRun(t, b.l)
	b.started(t)

	// Killed outright rather than asked to stop: nothing records how that Run
	// ended, so what the next daemon does about it is the whole point.
	if err := d.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	d.exit(t, 10*time.Second)
	waitForLog(t, startDaemon(t, b.l), "daemon listening")

	row := runRowOf(t, b.l, job, run)
	if row.outcome != "interrupted" {
		t.Errorf("run outcome = %q after a restart, want interrupted", row.outcome)
	}
	if got := line(t, mustOwl(t, b.l, "jobs", "show", job).stdout, "state"); got != "pending" {
		t.Errorf("state = %q after a restart, want pending", got)
	}
	again, sameJob := startRun(t, b.l)
	if sameJob != job || again == run {
		t.Errorf("owl start began run %s of job %s, want a new run of job %s", again, sameJob, job)
	}
	mustOwl(t, b.l, "pause")
}

func TestS14StoppingTheDaemonWhileFrozenDoesNotHang(t *testing.T) {
	b := beatingLayout(t, "")
	d := daemonUp(t, b.l)
	run, job := b.frozen(t)

	stopDaemon(t, d)

	if !b.still(t) {
		t.Errorf("the agent's child is still beating after the daemon stopped")
	}
	daemonUp(t, b.l)
	if row := runRowOf(t, b.l, job, run); row.outcome != "interrupted" {
		t.Errorf("run outcome = %q, want interrupted", row.outcome)
	}
	if got := line(t, mustOwl(t, b.l, "jobs", "show", job).stdout, "state"); got != "pending" {
		t.Errorf("state = %q, want pending", got)
	}
}

func TestS15PauseAndResumeLeaveTheDaemonAlone(t *testing.T) {
	b := beatingLayout(t, "")
	daemonUp(t, b.l)
	run, job := b.frozen(t)

	if res := runOwl(t, b.l, "daemon", "status"); res.code != 0 {
		t.Fatalf("owl daemon status exited %d while a run was frozen\nstderr:\n%s", res.code, res.stderr)
	}
	mustOwl(t, b.l, "resume")
	if res := runOwl(t, b.l, "daemon", "status"); res.code != 0 {
		t.Fatalf("owl daemon status exited %d after a resume\nstderr:\n%s", res.code, res.stderr)
	}
	b.stub.let(t)
	waitRun(t, b.l, job, run)
}

// runRowOf is the row owl jobs show reports for one Run, whether or not it has
// ended.
func runRowOf(t *testing.T, l *layout, job, run string) runRow {
	t.Helper()
	out := mustOwl(t, l, "jobs", "show", job).stdout
	for _, row := range runRows(t, out) {
		if row.id == run {
			return row
		}
	}
	t.Fatalf("job %s has no run %s:\n%s", job, run, out)
	return runRow{}
}

// contains reports whether any field of a row is s.
func contains(row []string, s string) bool {
	for _, f := range row {
		if f == s {
			return true
		}
	}
	return false
}
