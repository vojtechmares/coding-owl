package behavior_test

// Behavior tests for issue #101. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-101.md. They drive the built owl binary against a daemon
// whose stub agent is killed by a signal rather than exiting, and check that a
// Run says so - and that the Runs Owl ends itself, whose Agents die of a signal
// too, are still interruptions rather than failures.

import (
	"os"
	"strings"
	"testing"
)

// lastWords is what a scenario's stub agent writes to standard error before
// it ends. It names no signal and no status, so a reason that names one got it
// from Owl rather than from the Agent.
const lastWords = "out of memory half way through the test suite"

// killedJob runs one Job whose stub agent kills itself with the named signal,
// and returns what owl jobs show printed once the Run had finished.
func killedJob(t *testing.T, signal string) string {
	t.Helper()
	l, _ := agentLayout(t, agentScript, 0)
	l = l.withEnv("OWL_FAKE_CLAUDE_SIGNAL="+signal, "OWL_FAKE_CLAUDE_STDERR="+lastWords)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	return finished(t, l, job)
}

// onlyRun is the one Run owl jobs show reports, failing the scenario when
// there is not exactly one.
func onlyRun(t *testing.T, out string) runRow {
	t.Helper()
	rows := runRows(t, out)
	if len(rows) != 1 {
		t.Fatalf("owl jobs show reports %d runs, want 1:\n%s", len(rows), out)
	}
	return rows[0]
}

// namesNoSignal fails the scenario when a reason names any signal: a Run Owl
// ended itself is not explained by what its Agent finally died of.
func namesNoSignal(t *testing.T, reason string) {
	t.Helper()
	if strings.Contains(reason, "SIG") {
		t.Errorf("reason = %q, names a signal for a run Owl ended itself", reason)
	}
}

func TestS1KilledAgentFailsItsRunAndBlocksItsJob(t *testing.T) {
	out := killedJob(t, "SIGKILL")

	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if got := onlyRun(t, out).outcome; got != "failed" {
		t.Errorf("run outcome = %q, want failed", got)
	}
}

func TestS2KilledAgentsReasonNamesTheSignalAndNoStatus(t *testing.T) {
	out := killedJob(t, "SIGKILL")

	reason := line(t, out, "reason")
	signal := strings.Index(reason, "SIGKILL")
	if signal < 0 {
		t.Fatalf("reason = %q, does not name SIGKILL", reason)
	}
	for _, claim := range []string{"exited with status", "-1"} {
		if strings.Contains(reason, claim) {
			t.Errorf("reason = %q, claims %q for an agent that never exited", reason, claim)
		}
	}
	if said := strings.Index(reason, lastWords); said < signal {
		t.Errorf("reason = %q, want what the agent wrote to standard error after the signal's name", reason)
	}
}

func TestS3KilledAgentHasNoExitStatusRecorded(t *testing.T) {
	out := killedJob(t, "SIGKILL")

	if got := onlyRun(t, out).exit; got != "(none)" {
		t.Errorf("EXIT = %q, want (none) for an agent that never exited", got)
	}
}

func TestS4ASignalOwlDidNotSendIsAFailureEvenOneOwlSends(t *testing.T) {
	out := killedJob(t, "SIGTERM")

	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	row := onlyRun(t, out)
	if row.outcome != "failed" || row.exit != "(none)" {
		t.Errorf("run = %+v, want it failed with no exit status", row)
	}
	reason := line(t, out, "reason")
	if !strings.Contains(reason, "SIGTERM") || strings.Contains(reason, "SIGKILL") {
		t.Errorf("reason = %q, want it to name SIGTERM, the signal the agent was killed by", reason)
	}
}

func TestS5ARunTheDaemonStoppingEndsIsStillInterrupted(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := agentLayout(t, script, 0)
	d := daemonUp(t, l)
	runnableJob(t, l, "work")
	run, job := startRun(t, l)
	// The Agent is running, so what stopping the daemon ends is an Agent Owl
	// has to kill rather than a Run that never got as far as one.
	waitFor(t, "the agent to start", func() bool {
		_, err := os.Stat(s.argv)
		return err == nil
	})

	stopDaemon(t, d)
	daemonUp(t, l)

	row := runRowOf(t, l, job, run)
	if row.outcome != "interrupted" || row.exit != "(none)" {
		t.Errorf("run = %+v, want it interrupted with no exit status", row)
	}
	out := mustOwl(t, l, "jobs", "show", job).stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want pending", got)
	}
	reason := line(t, out, "reason")
	if !strings.Contains(reason, "daemon stopped") {
		t.Errorf("reason = %q, want it to say the daemon stopped", reason)
	}
	namesNoSignal(t, reason)
}

func TestS6ARunTheGraceWindowEndsIsStillInterruptedWhenItsAgentIsKilled(t *testing.T) {
	b := beatingLayout(t, "apiVersion: codingowl.dev/v1\ngraceWindow: 1s\n")
	// An Agent that carries on when it is asked to stop, so what ends it is
	// the SIGKILL Owl sends after the window (ADR-0034).
	b.l = b.l.withEnv("OWL_FAKE_CLAUDE_IGNORE_TERM=1")
	d := daemonUp(t, b.l)
	run, job := b.frozen(t)

	row := waitRun(t, b.l, job, run)

	waitForLog(t, d, "an agent did not stop when it was asked, and is being killed")
	if row.outcome != "interrupted" || row.exit != "(none)" {
		t.Errorf("run = %+v, want it interrupted with no exit status", row)
	}
	out := mustOwl(t, b.l, "jobs", "show", job).stdout
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want pending", got)
	}
	reason := line(t, out, "reason")
	if !strings.Contains(reason, "grace window passed") {
		t.Errorf("reason = %q, want it to say the grace window passed", reason)
	}
	namesNoSignal(t, reason)
}

func TestS7AnAgentThatExitsNonZeroIsReportedAsBefore(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 3)
	l = l.withEnv("OWL_FAKE_CLAUDE_STDERR=" + lastWords)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	_, job := startRun(t, l)
	out := finished(t, l, job)

	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	row := onlyRun(t, out)
	if row.outcome != "failed" || row.exit != "3" {
		t.Errorf("run = %+v, want it failed with exit status 3", row)
	}
	if reason, want := line(t, out, "reason"), "the agent exited with status 3: "+lastWords; reason != want {
		t.Errorf("reason = %q, want %q", reason, want)
	}
}
