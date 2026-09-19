package behavior_test

// Behavior tests for issue #133. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-133.md. They drive the built owl binary against a
// daemon whose PATH puts the stub agent of issue #5 where Claude Code would
// be, and queue Jobs that wait for other Jobs.

import (
	"regexp"
	"strings"
	"testing"
)

// queuedRE reads the id out of what owl add says, so a scenario can name the
// Job it has just queued without going back to the queue for it.
var queuedRE = regexp.MustCompile(`queued job (\d+)`)

// queued is the id of the Job owl add reported queueing.
func queued(t *testing.T, res result) string {
	t.Helper()
	m := queuedRE.FindStringSubmatch(res.stdout)
	if m == nil {
		t.Fatalf("owl add did not report the job it queued:\n%s", res.stdout)
	}
	return m[1]
}

// waitingOver is each Job owl status says was passed over, and why. It is
// caps.passedOver for a layout that has no caps harness around it: the REASON
// column alone, so a reason that never names a project cannot look as though
// it did.
func waitingOver(t *testing.T, l *layout) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, row := range sectionOf(mustOwl(t, l, "status").stdout, "passed over:") {
		cells := columns.Split(strings.TrimSpace(row), 3)
		if len(cells) != 3 {
			t.Fatalf("a passed-over row is not job, project and reason: %q", row)
		}
		out[cells[0]] = cells[2]
	}
	return out
}

// dependent is two Projects with a Job each, the second waiting for the first,
// against a daemon that starts nothing by itself. It returns the ids.
func dependent(t *testing.T, l *layout) (blocking, waiting string) {
	t.Helper()
	api := project(t, l, "api")
	blocking = queued(t, addJob(t, l, api.dir, "the api change", "--no-plan"))
	web := project(t, l, "web")
	waiting = queued(t, addJob(t, l, web.dir, "the client change", "--no-plan", "--blocked-by", blocking))
	return blocking, waiting
}

func TestS1BlockedByQueuesAJobBehindAnotherByItsID(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	first := queued(t, addJob(t, l, r.dir, "the api change", "--no-plan"))

	res := addJob(t, l, r.dir, "the client change", "--no-plan", "--blocked-by", first)

	// The whole phrase, and to the end of the line: "job 1" alone would be
	// satisfied by "job 10" in a queue that had got that far.
	if !strings.Contains(res.stdout, "waiting for job "+first+"\n") {
		t.Errorf("owl add says %q, want it to name the job the new one waits for", res.stdout)
	}
	second := queued(t, res)
	out := mustOwl(t, l, "jobs", "show", second).stdout
	if got := line(t, out, "waiting for"); got != "job "+first {
		t.Errorf("waiting for = %q, want job %s:\n%s", got, first, out)
	}
}

func TestS2BlockedByAJobThatIsNotThereIsRefused(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "the api change", "--no-plan")
	before := summarise(queueList(t, l))

	res := runOwlIn(t, l, r.dir, "add", "the client change", "--no-plan", "--blocked-by", "999")

	if res.code == 0 {
		t.Fatalf("owl add exited 0 naming a job that is not there:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "999") {
		t.Errorf("owl add says %q, want it to name the job 999 that is not there", res.stderr)
	}
	if !strings.Contains(res.stderr, "no job") {
		t.Errorf("owl add says %q, want it to say there is no such job", res.stderr)
	}
	if after := summarise(queueList(t, l)); after != before {
		t.Errorf("the queue holds:\n%s\nwant what it held:\n%s", after, before)
	}
}

func TestS3BlockedBySomethingThatIsNotAJobIDIsRefused(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")

	for _, bad := range []string{"0", "-1", "abc"} {
		res := runOwlIn(t, l, r.dir, "add", "the client change", "--no-plan", "--blocked-by", bad)

		if res.code == 0 {
			t.Errorf("owl add exited 0 with --blocked-by %s:\n%s", bad, res.stdout)
			continue
		}
		want := "job ids count from one"
		if bad == "abc" {
			// Not a number at all, so the flag itself refuses it before the
			// daemon is asked anything.
			want = "blocked-by"
		}
		if !strings.Contains(res.stderr, want) {
			t.Errorf("owl add --blocked-by %s says %q, want it to carry %q", bad, res.stderr, want)
		}
		if rows := queueList(t, l); len(rows) != 0 {
			t.Errorf("owl add --blocked-by %s queued something: %v", bad, rows)
		}
	}
}

func TestS4TheSchedulerPassesOverAJobWhoseDependencyIsNotDone(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	blocking, waiting := dependent(t, c.l)

	c.away(t)

	// Only the Job that waits for nothing is going, though the dependent Job's
	// own Project is free and nothing else holds it.
	c.waitRunning(t, 1)
	if got := columnOf(c.status(t), "runs in progress:", 1); len(got) != 1 || got[0] != blocking {
		t.Fatalf("the job in flight is %v, want the one nothing waits for (%s):\n%s", got, blocking, c.status(t))
	}
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the dependent one: %v", len(over), over)
	}
	if _, ok := over[waiting]; !ok {
		t.Errorf("owl status passed over %v, want job %s", over, waiting)
	}
}

func TestS5StatusSaysWhichJobIsWaitedForAndItsState(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	blocking, waiting := dependent(t, c.l)

	c.away(t)
	c.waitRunning(t, 1)

	why := c.passedOver(t)[waiting]
	if !strings.Contains(why, "job "+blocking) {
		t.Errorf("job %s was passed over for %q, want it to name job %s", waiting, why, blocking)
	}
	// The blocking Job's Run is in flight, so that is the state to report.
	if !strings.Contains(why, "active") {
		t.Errorf("job %s was passed over for %q, want it to name the state job %s is in", waiting, why, blocking)
	}
}

func TestS6APassedOverDependentJobKeepsItsQueuePosition(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	blocking, waiting := dependent(t, c.l)
	// A third Job, in a Project of its own, queued after the dependent one: it
	// can run, so it shows that being passed over is not being moved.
	cli := project(t, c.l, "cli")
	behind := queued(t, addJob(t, c.l, cli.dir, "the cli change", "--no-plan"))
	before := allPositions(t, c.l)
	if before[waiting] == "" {
		t.Fatalf("job %s was not in the queue to begin with: %v", waiting, before)
	}

	c.away(t)
	c.waitRunning(t, 2)

	if got := jobState(t, c.l, waiting); got != "pending" {
		t.Errorf("job %s is %s, want it still pending: being passed over is not leaving the queue", waiting, got)
	}
	after := allPositions(t, c.l)
	if after[waiting] != before[waiting] {
		t.Errorf("job %s moved from position %s to %s", waiting, before[waiting], after[waiting])
	}
	// And it is still ahead of what was queued after it, which ran while it
	// waited (ADR-0025).
	if after[waiting] >= after[behind] {
		t.Errorf("job %s is at position %s and job %s, queued after it, is at %s",
			waiting, after[waiting], behind, after[behind])
	}
	if after[blocking] != before[blocking] {
		t.Errorf("the blocking job moved from position %s to %s", before[blocking], after[blocking])
	}
}

// allPositions is each Job's place in the queue, by id, whatever its state: a
// Job being run keeps its position (ADR-0025), and a scenario about positions
// has to be able to see it.
func allPositions(t *testing.T, l *layout) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, r := range queueList(t, l, "--all") {
		out[r.id] = r.position
	}
	return out
}

func TestS7ReviewIsNotDoneAndAcceptingClearsTheWait(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	blocking, waiting := dependent(t, l)

	// The blocking Job's Run ends and it waits for a decision.
	run, job := startRun(t, l)
	if job != blocking {
		t.Fatalf("owl start started job %s, want the one at the head of the queue (%s)", job, blocking)
	}
	waitRun(t, l, blocking, run)
	if got := jobState(t, l, blocking); got != "review" {
		t.Fatalf("job %s is %s, want it in review:\n%s", blocking, got,
			mustOwl(t, l, "jobs", "show", blocking).stdout)
	}

	// Finished is not done: the dependent Job still waits.
	why := waitingOver(t, l)[waiting]
	if !strings.Contains(why, "job "+blocking) || !strings.Contains(why, "review") {
		t.Errorf("job %s was passed over for %q, want it to say it waits for job %s in review",
			waiting, why, blocking)
	}

	mustOwl(t, l, "jobs", "accept", blocking)

	_, started := startRun(t, l)
	if started != waiting {
		t.Errorf("owl start started job %s, want the dependent job %s now that %s is done",
			started, waiting, blocking)
	}
}

func TestS8StartRefusesAndNamesTheJobBeingWaitedFor(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	blocking, _ := dependent(t, l)

	// The blocking Job leaves the queue for a decision, so the dependent Job
	// is the only thing left to choose between.
	run, _ := startRun(t, l)
	waitRun(t, l, blocking, run)

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start exited 0 with only a waiting job in the queue:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "waits for job "+blocking) {
		t.Errorf("owl start says %q, want it to say the job waits for job %s", res.stderr, blocking)
	}
}

func TestS9ADependencyNothingWillClearIsSaidOutLoud(t *testing.T) {
	c := capsLayout(t, "maxParallelRuns: 4\n")
	blocking, waiting := dependent(t, c.l)
	// Cancelled is a state a Job never leaves, so nothing about this will
	// change until a person does something.
	mustOwl(t, c.l, "queue", "remove", blocking)

	c.away(t)

	// Said on the line about nothing running, which is where a reason Owl
	// expects to clear itself is never reported (ADR-0011): a person looking
	// at an idle machine is owed the reason it is idle.
	waitFor(t, "owl status to say why nothing is running", func() bool {
		return strings.Contains(maybeLine(c.status(t), "nothing is running"), "waits for job "+blocking)
	})
	if got := maybeLine(c.status(t), "nothing is running"); !strings.Contains(got, "cancelled") {
		t.Errorf("owl status says %q, want it to name the state job %s is in", got, blocking)
	}
	// And it is in the passed-over list too, because that is where a Job's own
	// reason lives.
	over := c.passedOver(t)
	if len(over) != 1 {
		t.Fatalf("owl status passed over %d jobs, want the waiting one: %v", len(over), over)
	}
	if why := over[waiting]; !strings.Contains(why, "job "+blocking) {
		t.Errorf("job %s was passed over for %q, want it to name job %s", waiting, why, blocking)
	}
}

func TestS10TheAgentIsToldWhatItsJobWasQueuedBehind(t *testing.T) {
	l, s := committingLayout(t)
	daemonUp(t, l)
	blocking, waiting := dependent(t, l)

	run, _ := startRun(t, l)
	waitRun(t, l, blocking, run)
	mustOwl(t, l, "jobs", "accept", blocking)
	run, started := startRun(t, l)
	if started != waiting {
		t.Fatalf("owl start started job %s, want the dependent job %s", started, waiting)
	}
	waitRun(t, l, waiting, run)

	prompt := lastArg(t, s.invoked(t))
	for _, want := range []string{
		// Its own work first.
		"the client change",
		// What it was queued behind: the Job, its state, and its prompt. The
		// state is asserted in the phrase that carries it - the word "done"
		// alone appears in the execution prompt's own boilerplate, so it
		// would pass for a note that dropped the state entirely.
		"job " + blocking + ", which is done",
		"the api change",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the agent's prompt does not carry %q:\n%s", want, prompt)
		}
	}
	// And the other Job's prompt is fenced off from Owl's own words, so that a
	// prompt somebody else wrote cannot be read as instructions to this Agent.
	// The fence is named rather than matched as "a line of dashes": the quoted
	// handoff has one of those too, and a check that took either would pass
	// for a blocking prompt that was spliced in unfenced.
	const fence = "----- queued behind -----"
	_, quoted, ok := strings.Cut(prompt, fence+"\n")
	if !ok {
		t.Fatalf("the blocking job's prompt is spliced into the agent's prompt rather than fenced:\n%s", prompt)
	}
	inside, _, _ := strings.Cut(quoted, "\n"+fence)
	if strings.TrimSpace(inside) != "the api change" {
		t.Errorf("the fences hold %q, want the blocking job's prompt:\n%s", inside, prompt)
	}
}

func TestS11AJobWithNoDependencyIsUntouched(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")

	out, job := finishedJob(t, l)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want a job with no dependency to run as it always did:\n%s", got, out)
	}
	if strings.Contains(out, "waiting for") {
		t.Errorf("owl jobs show %s says something about waiting for another job:\n%s", job, out)
	}
}
