package behavior_test

// Behavior tests for issue #50. TestS5 maps to scenario S5 in
// tests/behavior/issue-50.md; S1 to S4 live with the host executor and the
// shell runner, whose own APIs they exercise. This one drives the built owl
// binary against a daemon whose Agent, and whose Agent's child, ignore being
// asked to stop.

import (
	"syscall"
	"testing"
	"time"
)

// gracePeriod is the five seconds an Agent has to act on SIGTERM before it is
// killed (ADR-0034), and killMargin is what the scenario allows on top of it
// for the kill to land and be seen.
const (
	gracePeriod = 5 * time.Second
	killMargin  = 3 * time.Second
)

func TestS5StoppingTheDaemonKillsWhatAnAgentStartedEvenWhenNeitherWillStop(t *testing.T) {
	b := beatingLayout(t, "")
	// An Agent that catches being asked to stop and carries on, and whose
	// child does the same.
	b.l = b.l.withEnv("OWL_FAKE_CLAUDE_IGNORE_TERM=1")
	d := daemonUp(t, b.l)
	r := project(t, b.l, "api")
	addJob(t, b.l, r.dir, "work", "--no-plan")
	startRun(t, b.l)
	b.started(t)
	agent, child := b.stub.invoked(t).PID, b.childPID(t)
	t.Cleanup(func() {
		_ = syscall.Kill(agent, syscall.SIGKILL)
		_ = syscall.Kill(child, syscall.SIGKILL)
	})

	signalled := time.Now()
	if err := d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	if code := d.exit(t, gracePeriod+killMargin+5*time.Second); code != 0 {
		t.Fatalf("daemon exited %d:\n%s", code, d.out())
	}
	deadline := signalled.Add(gracePeriod + killMargin)
	for time.Now().Before(deadline) && (processAlive(agent) || processAlive(child)) {
		time.Sleep(20 * time.Millisecond)
	}
	if processAlive(agent) {
		t.Errorf("the agent that ignores SIGTERM is still alive %s after the daemon was told to stop", time.Since(signalled))
	}
	if processAlive(child) {
		t.Errorf("the child the agent started is still alive %s after the daemon was told to stop", time.Since(signalled))
	}
}

// processAlive reports whether a process with that pid can still be signalled.
func processAlive(pid int) bool { return syscall.Kill(pid, syscall.Signal(0)) == nil }
