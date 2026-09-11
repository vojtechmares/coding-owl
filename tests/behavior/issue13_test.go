package behavior_test

// Behavior tests for issue #13. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-13.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be.

import (
	"strconv"
	"strings"
	"testing"
)

// attempts is what owl jobs show reports a Job has left.
func attempts(t *testing.T, l *layout, job string) int {
	t.Helper()
	got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "attempts")
	n, err := strconv.Atoi(strings.Fields(got)[0])
	if err != nil {
		t.Fatalf("owl jobs show reports attempts %q: %v", got, err)
	}
	return n
}

func TestS1TTLDefaultsToTenAttempts(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")

	addJob(t, l, r.dir, "work")

	if got := attempts(t, l, "1"); got != 10 {
		t.Errorf("a job is queued with %d attempts, want ten", got)
	}
}

func TestS2TTLIsSettableWhenTheJobIsQueued(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")

	addJob(t, l, r.dir, "work", "--ttl", "3")

	if got := attempts(t, l, "1"); got != 3 {
		t.Errorf("a job queued with --ttl 3 has %d attempts, want three", got)
	}
}

func TestS3TTLThatIsNotANumberOfAttemptsIsRefused(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")

	for _, c := range []struct {
		ttl string
		// says is what the message has to tell the user beyond naming the flag
		// and the value it would not take: what a ttl is instead. A value that
		// is not a number at all never reaches that check, because the flag
		// itself refuses it in its own words.
		says string
	}{
		{ttl: "0", says: "count from one"},
		{ttl: "-1", says: "count from one"},
		{ttl: "many"},
	} {
		res := runOwlIn(t, l, r.dir, "add", "work", "--ttl", c.ttl)

		if res.code == 0 {
			t.Errorf("owl add --ttl %s exited 0\nstdout:\n%s", c.ttl, res.stdout)
		}
		// Saying something is not saying what was wrong: a flag that does not
		// exist would say something too.
		said := strings.TrimSpace(res.stderr)
		for _, want := range []string{"ttl", c.ttl, c.says} {
			if !strings.Contains(said, want) {
				t.Errorf("owl add --ttl %s said %q, which does not name %q", c.ttl, said, want)
			}
		}
	}
	if rows := queueList(t, l); len(rows) != 0 {
		t.Errorf("the queue holds %d jobs, want none: every add was refused", len(rows))
	}
}

func TestS4ARunThatSucceededSpendsAnAttempt(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "3")

	_, job := finishedJob(t, l)

	if got := attempts(t, l, job); got != 2 {
		t.Errorf("the job has %d attempts left after one run, want two", got)
	}
}

func TestS5ARunThatFailedSpendsAnAttemptToo(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 3)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "3")

	out, job := finishedJob(t, l)

	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked", got)
	}
	if got := attempts(t, l, job); got != 2 {
		t.Errorf("the job has %d attempts left after a failed run, want two", got)
	}
}

func TestS6ARunThatWasInterruptedSpendsAnAttempt(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, _ := agentLayout(t, script, 0)
	d := daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "3")
	_, job := startRun(t, l)

	stopDaemon(t, d)
	daemonUp(t, l)

	if got := jobState(t, l, job); got != "pending" {
		t.Fatalf("state = %q, want pending", got)
	}
	if got := attempts(t, l, job); got != 2 {
		t.Errorf("the job has %d attempts left after an interrupted run, want two", got)
	}
}

// spent runs a planned Job with one attempt, which its planning Run uses up.
func spent(t *testing.T, l *layout, r *repo) string {
	t.Helper()
	addJob(t, l, r.dir, "work", "--ttl", "1")
	run, job := startRun(t, l)
	waitRun(t, l, job, run)
	return job
}

func TestS7AJobOutOfAttemptsIsExhausted(t *testing.T) {
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := project(t, l, "api")

	job := spent(t, l, r)

	out := mustOwl(t, l, "jobs", "show", job).stdout
	if got := line(t, out, "state"); got != "exhausted" {
		t.Errorf("state = %q, want exhausted", got)
	}
	if got := attempts(t, l, job); got != 0 {
		t.Errorf("the job has %d attempts left, want none", got)
	}
}

func TestS8AnExhaustedJobIsNeverScheduled(t *testing.T) {
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := project(t, l, "api")
	job := spent(t, l, r)
	addJob(t, l, r.dir, "the next one", "--no-plan")

	_, started := startRun(t, l)

	if started == job {
		t.Errorf("owl start ran the exhausted job %s", job)
	}
	if got := jobState(t, l, job); got != "exhausted" {
		t.Errorf("the exhausted job is %q", got)
	}
}

func TestS9ExtendReturnsAnExhaustedJobToTheQueue(t *testing.T) {
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := project(t, l, "api")
	job := spent(t, l, r)

	res := mustOwl(t, l, "jobs", "extend", job)

	if !strings.Contains(res.stdout, "10") {
		t.Errorf("owl jobs extend does not say how many attempts the job has:\n%s", res.stdout)
	}
	if got := jobState(t, l, job); got != "pending" {
		t.Errorf("state = %q, want pending", got)
	}
	if got := attempts(t, l, job); got != 10 {
		t.Errorf("the job has %d attempts, want ten", got)
	}
	if _, started := startRun(t, l); started != job {
		t.Errorf("owl start ran job %s, want the extended job %s", started, job)
	}
}

func TestS10ExtendAddsToWhatIsLeft(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "2")

	mustOwl(t, l, "jobs", "extend", "1", "--ttl", "5")

	if got := attempts(t, l, "1"); got != 7 {
		t.Errorf("the job has %d attempts, want the five added to the two it had", got)
	}
	if got := jobState(t, l, "1"); got != "pending" {
		t.Errorf("state = %q, want the job left where it was", got)
	}
}

func TestS11ExtendRefusesANumberThatIsNotAttempts(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "4")

	for _, ttl := range []string{"0", "-3"} {
		res := runOwl(t, l, "jobs", "extend", "1", "--ttl", ttl)

		if res.code == 0 {
			t.Errorf("owl jobs extend --ttl %s exited 0\nstdout:\n%s", ttl, res.stdout)
		}
		if got := attempts(t, l, "1"); got != 4 {
			t.Errorf("the job has %d attempts after a refused extend, want the four it had", got)
		}
	}
}

func TestS12ExtendRefusesAJobThatIsNotThere(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)

	res := runOwl(t, l, "jobs", "extend", "999")

	if res.code == 0 {
		t.Fatalf("owl jobs extend 999 exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "999") {
		t.Errorf("stderr does not name 999:\n%s", res.stderr)
	}
}

func TestS13StatusTellsExhaustedFromBlocked(t *testing.T) {
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := project(t, l, "api")
	job := spent(t, l, r)
	// And a Job a check refuses, which is a different thing.
	bad := newRepo(t, l, "web")
	bad.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nchecks:\n  - name: guard\n    run: exit 1\n", "configure owl")
	addProject(t, l, bad)
	addJob(t, l, bad.dir, "block me", "--no-plan")
	out, blocked := finishedJob(t, l)
	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked", got)
	}

	status := mustOwl(t, l, "status").stdout

	counts := statusCounts(t, status)
	if counts["exhausted"] != "1" || counts["blocked"] != "1" {
		t.Errorf("owl status counts %q exhausted and %q blocked, want one of each:\n%s",
			counts["exhausted"], counts["blocked"], status)
	}
	_, exhausted, ok := strings.Cut(status, "out of attempts:")
	if !ok {
		t.Fatalf("owl status does not list what is out of attempts:\n%s", status)
	}
	for _, want := range []string{job, "api", "work"} {
		if !strings.Contains(exhausted, want) {
			t.Errorf("the exhausted section does not name %q:\n%s", want, status)
		}
	}
	if strings.Contains(exhausted, blocked) {
		t.Errorf("the blocked job is listed as out of attempts:\n%s", status)
	}
}

func TestS14AnExhaustedJobKeepsItsPlace(t *testing.T) {
	l, _ := planningLayout(t, "# Handoff\n\nstep one done\n")
	daemonUp(t, l)
	r := project(t, l, "api")
	job := spent(t, l, r)
	addJob(t, l, r.dir, "the next one", "--no-plan")

	mustOwl(t, l, "jobs", "extend", job)

	rows := queueList(t, l)
	if len(rows) != 2 {
		t.Fatalf("the queue holds %d jobs, want two:\n%+v", len(rows), rows)
	}
	if rows[0].id != job {
		t.Errorf("the queue starts with job %s, want the extended job %s", rows[0].id, job)
	}
}

func TestS15AJobWithAttemptsLeftIsNotExhausted(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "3")

	out, _ := finishedJob(t, l)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

func TestS16WhatIsLeftSurvivesARestart(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	d := daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan", "--ttl", "4")
	_, job := finishedJob(t, l)
	before := attempts(t, l, job)
	if before != 3 {
		t.Fatalf("the job has %d attempts after one run of the four it was given, want three", before)
	}

	stopDaemon(t, d)
	daemonUp(t, l)

	if got := attempts(t, l, job); got != before {
		t.Errorf("the job has %d attempts after a restart, want the %d it had", got, before)
	}
}
