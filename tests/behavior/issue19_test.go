package behavior_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fastIdle is what every scenario here puts in global configuration: the
// daemon looks at the machine often enough that a scenario can watch it change
// its mind. The policy itself is the default unless a scenario says otherwise.
const fastIdle = "apiVersion: codingowl.dev/v1\nidle:\n  interval: 100ms\n"

// machine is the stub machine, as a scenario steps away from it and comes back
// to it.
type machine struct{ path string }

// watching is a layout whose daemon reads the machine through the stub, first
// on its PATH in place of the system's own tools, with that global
// configuration. The machine starts in use, so nothing runs until a scenario
// says the user has gone.
func watching(t *testing.T, l *layout, config string) (*layout, *machine) {
	t.Helper()
	m := &machine{path: filepath.Join(l.root, "machine")}
	m.inUse(t)
	if config == "" {
		config = fastIdle
	}
	globalConfig(t, l, config)
	out := l.withoutEnv("PATH").withEnv(
		"PATH="+fakeMachineDir+string(os.PathListSeparator)+
			fakeClaudeDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"OWL_FAKE_MACHINE="+m.path,
	)
	return out, m
}

// says writes what the machine reports from now on.
func (m *machine) says(t *testing.T, what string) {
	t.Helper()
	if err := os.WriteFile(m.path, []byte(what+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// idleFor is a machine nobody has touched for that long.
func (m *machine) idleFor(t *testing.T, d time.Duration, onPower bool) {
	t.Helper()
	power := "battery"
	if onPower {
		power = "ac"
	}
	m.says(t, strconv.Itoa(int(d.Seconds()))+" "+power)
}

// inUse is a machine somebody is at: input a moment ago, on AC power, so that
// what refuses it is the input and not the power.
func (m *machine) inUse(t *testing.T) {
	t.Helper()
	m.idleFor(t, 30*time.Second, true)
}

// away is a machine nobody has touched for long enough under the default
// policy, on AC power.
func (m *machine) away(t *testing.T) {
	t.Helper()
	m.idleFor(t, 11*time.Minute, true)
}

// unreadable is a machine whose state nothing can get at.
func (m *machine) unreadable(t *testing.T) {
	t.Helper()
	m.says(t, "unreadable")
}

// maybeLine is a key line of some output, empty when there is none: a line
// that is not there yet is what a scenario is waiting for rather than a
// failure.
func maybeLine(out, key string) string {
	for _, ln := range strings.Split(out, "\n") {
		if after, ok := strings.CutPrefix(ln, key+":"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// looked gives the daemon time to look at the machine several times, for the
// scenarios that assert nothing happened.
func looked() { sleep(700 * time.Millisecond) }

// waitStarted waits for a Run to appear for that Job without anybody having
// asked for one, and returns its id.
func waitStarted(t *testing.T, l *layout, job string) string {
	t.Helper()
	var run string
	waitFor(t, "a run to start on its own", func() bool {
		rows := runRows(t, mustOwl(t, l, "jobs", "show", job).stdout)
		if len(rows) == 0 {
			return false
		}
		run = rows[0].id
		return true
	})
	return run
}

// queuedJob registers a Project and queues one Job in it, without starting
// anything.
func queuedJob(t *testing.T, l *layout) (*repo, string) {
	t.Helper()
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")
	return r, "1"
}

// running reports whether any Run has been recorded for that Job.
func running(t *testing.T, l *layout, job string) bool {
	t.Helper()
	return len(runRows(t, mustOwl(t, l, "jobs", "show", job).stdout)) > 0
}

func TestS1IdleAMachineInUseStartsNothing(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.idleFor(t, time.Minute, true)

	looked()

	if running(t, l, job) {
		t.Error("a run started while somebody was at the machine")
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state"); got != "pending" {
		t.Errorf("state = %q, want pending", got)
	}
}

func TestS2IdleAMachineOnBatteryStartsNothing(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.idleFor(t, time.Hour, false)

	looked()

	if running(t, l, job) {
		t.Error("a run started on battery")
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state"); got != "pending" {
		t.Errorf("state = %q, want pending", got)
	}
}

func TestS3IdleAnIdleMachineStartsARunWithNobodyAsking(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)
	// checkedProject queues the Job; nothing asks for it to be run.
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	job := "1"

	m.away(t)

	run := waitStarted(t, l, job)
	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out as if somebody had asked", row.outcome)
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS4IdleThePolicyIsWhatTheConfigurationSays(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "apiVersion: codingowl.dev/v1\nidle:\n"+
		"  interval: 100ms\n  after: 1s\n  requirePower: false\n")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	job := "1"

	// Two seconds without input, on battery: idle under this policy and under
	// no other.
	m.idleFor(t, 2*time.Second, false)

	run := waitStarted(t, l, job)
	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out", row.outcome)
	}
}

func TestS5IdleStartWorksWhateverTheMachineIsDoing(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.inUse(t)

	run, _ := startRun(t, l)
	b.started(t)

	if job != "1" {
		t.Fatalf("the queued job is %q", job)
	}
	// Still running a moment later: a Run somebody asked for is not the
	// machine's to freeze.
	looked()
	b.growing(t)
	if row := runRowOf(t, l, job, run); row.ended != "(none)" {
		t.Errorf("run ended %q, want one still going", row.ended)
	}
	b.stub.let(t)
}

func TestS6IdleComingBackToTheMachineFreezesTheRun(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.away(t)
	run := waitStarted(t, l, job)
	b.started(t)

	m.inUse(t)

	waitFor(t, "the run to be reported as paused", func() bool {
		return runRowOf(t, l, job, run).outcome == "paused"
	})
	if !b.still(t) {
		t.Error("the agent's child is still beating after the machine came back into use")
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state"); got != "active" {
		t.Errorf("state = %q, want the job still the one in progress", got)
	}
	b.stub.let(t)
}

func TestS7IdleGoingAwayAgainContinuesTheSameRun(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.away(t)
	run := waitStarted(t, l, job)
	b.started(t)
	m.inUse(t)
	waitFor(t, "the run to be reported as paused", func() bool {
		return runRowOf(t, l, job, run).outcome == "paused"
	})

	m.away(t)

	b.growing(t)
	waitFor(t, "the run to be reported as going again", func() bool {
		return runRowOf(t, l, job, run).outcome != "paused"
	})
	b.stub.let(t)
	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the same run carried on", row.outcome)
	}
	if rows := runRows(t, mustOwl(t, l, "jobs", "show", job).stdout); len(rows) != 1 {
		t.Errorf("the job took %d runs, want the one that was continued", len(rows))
	}
}

func TestS8IdleTheGraceWindowPassingEndsTheRun(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, fastIdle+"graceWindow: 1s\n")
	b.l = l
	daemonUp(t, l)
	r, job := queuedJob(t, l)
	m.away(t)
	run := waitStarted(t, l, job)
	b.started(t)

	m.inUse(t)

	row := waitRun(t, l, job, run)
	if row.outcome != "interrupted" {
		t.Errorf("run outcome = %q, want interrupted", row.outcome)
	}
	waitFor(t, "the job to be queued again", func() bool {
		return line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state") == "pending"
	})
	out := mustOwl(t, l, "jobs", "show", job).stdout
	branch := line(t, out, "branch")
	if branch == "(none)" || branch == "" {
		t.Errorf("the job kept no branch:\n%s", out)
	}
	if !strings.Contains(r.git("branch", "--list", branch), branch) {
		t.Errorf("branch %q is gone, so the next run cannot carry on in place", branch)
	}
	worktree := line(t, out, "worktree")
	if st, err := os.Stat(worktree); err != nil || !st.IsDir() {
		t.Errorf("worktree %q is gone: %v", worktree, err)
	}
}

func TestS9IdlePauseIsNotUndoneByTheMachineGoingIdle(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.inUse(t)
	run, _ := startRun(t, l)
	b.started(t)
	mustOwl(t, l, "pause")

	m.away(t)

	looked()
	if !b.still(t) {
		t.Error("the agent's child is beating again: the machine undid what the user asked for")
	}
	if got := runRowOf(t, l, job, run).outcome; got != "paused" {
		t.Errorf("run state = %q, want it still paused", got)
	}

	mustOwl(t, l, "resume")

	b.growing(t)

	// And the other way round: a Run the machine froze, which the user then
	// asked to pause as well, is theirs now. `owl pause` stops a Run whatever
	// the machine is doing, so the machine leaving cannot undo it.
	m.inUse(t)
	waitFor(t, "the machine to freeze the run", func() bool {
		return runRowOf(t, l, job, run).outcome == "paused"
	})
	if res := runOwl(t, l, "pause"); res.code == 0 {
		t.Errorf("owl pause reported success for a run that was already frozen:\n%s", res.stdout)
	}

	m.away(t)

	looked()
	if !b.still(t) {
		t.Error("the agent's child is beating again: the machine undid what the user asked for")
	}
	if got := runRowOf(t, l, job, run).outcome; got != "paused" {
		t.Errorf("run state = %q, want it still paused", got)
	}
	b.stub.let(t)
}

func TestS10IdleStatusReportsTheMachine(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)

	m.idleFor(t, 12*time.Minute, true)

	waitFor(t, "status to report an idle machine", func() bool {
		return strings.Contains(line(t, mustOwl(t, l, "status").stdout, "machine"), "idle")
	})
	said := line(t, mustOwl(t, l, "status").stdout, "machine")
	for _, want := range []string{"12m", "AC power"} {
		if !strings.Contains(said, want) {
			t.Errorf("status says %q, want it to carry %q", said, want)
		}
	}

	m.idleFor(t, 5*time.Second, false)

	waitFor(t, "status to report a machine in use", func() bool {
		return strings.Contains(line(t, mustOwl(t, l, "status").stdout, "machine"), "in use")
	})
	said = line(t, mustOwl(t, l, "status").stdout, "machine")
	for _, want := range []string{"5s", "battery"} {
		if !strings.Contains(said, want) {
			t.Errorf("status says %q, want it to carry %q", said, want)
		}
	}
}

func TestS11IdleStatusSaysWhyNothingIsRunning(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)
	r, job := queuedJob(t, l)
	m.inUse(t)

	looked()

	said := line(t, mustOwl(t, l, "status").stdout, "nothing is running")
	if !strings.Contains(said, "machine is in use") {
		t.Errorf("status says %q, want it to blame the machine", said)
	}
	if running(t, l, job) {
		t.Fatal("a run started, so there is nothing to explain")
	}

	mustOwl(t, l, "queue", "remove", job)
	m.away(t)

	waitFor(t, "status to stop blaming the machine", func() bool {
		return strings.Contains(line(t, mustOwl(t, l, "status").stdout, "nothing is running"), "queued")
	})

	// A Job that cannot run on a machine Owl may work on: what refused it is
	// what a person needs, and the machine is not to blame for it.
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\naccount: nobody\n",
		"name an account nobody has")
	addJob(t, l, r.dir, "work", "--no-plan")

	waitFor(t, "status to say what refused the job", func() bool {
		said := maybeLine(mustOwl(t, l, "status").stdout, "nothing is running")
		return strings.Contains(said, "nobody") && !strings.Contains(said, "machine")
	})
}

func TestS12IdleAMachineOwlCannotReadStartsNothing(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.unreadable(t)

	looked()

	if running(t, l, job) {
		t.Error("a run started on a machine nobody could read")
	}
	said := line(t, mustOwl(t, l, "status").stdout, "machine")
	if !strings.Contains(said, "could not be read") {
		t.Errorf("status says %q, want it to say the machine could not be read", said)
	}

	// A Run already in flight is left alone: not knowing is not the same as
	// knowing somebody is back.
	m.away(t)
	run := waitStarted(t, l, job)
	b.started(t)
	m.unreadable(t)
	looked()

	b.growing(t)
	if got := runRowOf(t, l, job, run).outcome; got == "paused" {
		t.Error("the run was frozen on a reading nobody got")
	}
	b.stub.let(t)
}

func TestS13IdleAPolicyChangedWhileRunningIsTheOneThatHolds(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)
	checkedProject(t, l, "apiVersion: codingowl.dev/v1\n")
	job := "1"
	// Idle for two seconds on battery, which the default policy refuses twice
	// over.
	m.idleFor(t, 2*time.Second, false)
	looked()
	if running(t, l, job) {
		t.Fatal("a run started under the default policy")
	}

	globalConfig(t, l, "apiVersion: codingowl.dev/v1\nidle:\n"+
		"  interval: 100ms\n  after: 1s\n  requirePower: false\n")

	run := waitStarted(t, l, job)
	if row := waitRun(t, l, job, run); row.outcome != "succeeded" {
		t.Errorf("run outcome = %q, want the job carried out under the new policy", row.outcome)
	}
}

func TestS14IdleTheNextJobStartsWhenTheOneBeforeItEnds(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	r, first := queuedJob(t, l)
	addJob(t, l, r.dir, "and then this one", "--no-plan")
	second := "2"
	m.away(t)
	firstRun := waitStarted(t, l, first)
	b.started(t)

	// A Run in progress is work happening. Nothing is being held back, and
	// saying otherwise would be reporting a refusal that refused nothing.
	if said := maybeLine(mustOwl(t, l, "status").stdout, "nothing is running"); said != "" {
		t.Errorf("status says %q while a run is in progress", said)
	}
	// Long enough that a wait growing on every look would have grown past the
	// moment the Run ends.
	sleep(4 * time.Second)
	b.stub.let(t)
	waitRun(t, l, first, firstRun)
	ended := time.Now()

	// The machine never stopped being idle, so the queue moves on at the next
	// look rather than after a wait nothing earned.
	deadline := ended.Add(10 * time.Second)
	for time.Now().Before(deadline) && !running(t, l, second) {
		sleep(50 * time.Millisecond)
	}
	if !running(t, l, second) {
		t.Fatalf("the next job had not started 10s after the run before it ended:\n%s",
			mustOwl(t, l, "status").stdout)
	}
	if waited := time.Since(ended); waited > 1500*time.Millisecond {
		t.Errorf("the next job started %s after the one before it ended, as though something had refused it",
			waited.Truncate(time.Millisecond))
	}
	b.stub.let(t)
}

func TestS15IdleARunStartingAsTheMachineReturnsIsFrozen(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	// Setup that takes a moment, so the machine can change its mind while the
	// Run is still being got ready.
	r := checkedProject(t, l, "apiVersion: codingowl.dev/v1\nsetup:\n  - sleep 2\n")
	job := "1"
	_ = r

	m.away(t)
	sleep(300 * time.Millisecond)
	m.inUse(t)

	run := waitStarted(t, l, job)
	waitFor(t, "the run to be reported as paused", func() bool {
		return runRowOf(t, l, job, run).outcome == "paused"
	})
	if !b.still(t) {
		t.Error("the agent's child is still beating on a machine somebody is at")
	}
	b.stub.let(t)
}

func TestS16IdleFreezingDoesNotDependOnReadingTheConfiguration(t *testing.T) {
	b := beatingLayout(t, "")
	l, m := watching(t, b.l, "")
	b.l = l
	daemonUp(t, l)
	_, job := queuedJob(t, l)
	m.away(t)
	run := waitStarted(t, l, job)
	b.started(t)

	// The file stops parsing while the Run is going, which is exactly when
	// giving the machine back matters.
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\ngraceWindow: soon\n")
	m.inUse(t)

	// Nothing can be asked of the daemon while its configuration does not
	// parse, so what says the Run was frozen is the Agent's own child going
	// quiet - which is what being frozen is.
	waitFor(t, "the agent's child to stop beating", func() bool { return b.still(t) })
	_ = run
	// A person is told, because a person can fix it.
	res := runOwl(t, l, "pause")
	if res.code == 0 {
		t.Errorf("owl pause exited 0 with a configuration that does not parse:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "graceWindow") {
		t.Errorf("stderr does not name the setting:\n%s", res.stderr)
	}
	b.stub.let(t)
}

func TestS17IdleTheAppShowsWhetherTheMachineIsIdle(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	l, m := watching(t, l, "")
	daemonUp(t, l)
	m.idleFor(t, 20*time.Minute, true)
	app, _ := desktopApp(t, l)

	var idle bool
	var since string
	waitFor(t, "the app to be told the machine is idle", func() bool {
		o, err := app.Overview()
		if err != nil {
			t.Fatalf("Overview: %v", err)
		}
		idle, since = o.Machine.Idle, o.Machine.Since.String()
		return idle
	})
	if !strings.Contains(since, "20m") {
		t.Errorf("the app was told the machine has been idle %q, want what the machine says", since)
	}
	o, err := app.Overview()
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if !o.Machine.OnPower {
		t.Error("the app was not told the machine is on AC power")
	}

	// The window shows it, rather than the app merely knowing it.
	// What is rendered, rather than what is imported: a view that reads the
	// machine and shows none of it would satisfy the words alone.
	src := filepath.Join(repoDir, "cmd", "owl-desktop", "frontend", "src")
	body := readFile(t, filepath.Join(src, "views", "Overview.tsx"))
	for _, want := range []string{`title="Machine"`, `label="Idle"`, `label="Last input"`, `label="Power"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the overview view does not render %s", want)
		}
	}
}
