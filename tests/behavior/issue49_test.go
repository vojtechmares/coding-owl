package behavior_test

// Behavior tests for issue #49. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-49.md. The scenarios drive the built owl binary against
// a daemon whose stub agent says one line more than Owl will read, and then
// more than a pipe holds, so a Run that stops reading is one that never ends.

import (
	"strconv"
	"strings"
	"testing"
)

// overLong is one byte more than the 8 MiB Owl reads a line up to.
const overLong = 8<<20 + 1

// pipeful is more than the 64 KiB a pipe buffers, so an Agent that writes it
// into a pipe nobody reads is blocked for good.
const pipeful = 64<<10 + 1

// overLongScript is what the Agent of S1 says: a normal line, an over-long
// one, and more than a pipe holds after it.
var overLongScript = []string{
	agentScript[0],
	"#fill " + strconv.Itoa(overLong),
	"#fill " + strconv.Itoa(pipeful),
	agentScript[2],
}

// overLongRun runs the Job of S1 to its end and returns the layout it ran in,
// the run id and what owl jobs show printed once it had ended.
func overLongRun(t *testing.T) (l *layout, run, out string) {
	t.Helper()
	l, _ = agentLayout(t, overLongScript, 0)
	daemonUp(t, l)
	runnableJob(t, l, "work")

	run, job := startRun(t, l)
	out = finished(t, l, job)
	return l, run, out
}

func TestS1RunEndsAfterAnOverLongLineFollowedByMoreThanAPipeHolds(t *testing.T) {
	_, _, out := overLongRun(t)

	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	runs := runRows(t, out)
	if len(runs) != 1 {
		t.Fatalf("owl jobs show reports %d runs, want 1:\n%s", len(runs), out)
	}
	if runs[0].outcome != "failed" {
		t.Errorf("run outcome = %q, want failed:\n%s", runs[0].outcome, out)
	}
	if runs[0].ended == "(none)" {
		t.Errorf("run has not ended:\n%s", out)
	}
}

func TestS2OverLongLineIsTheRunsRecordedReason(t *testing.T) {
	_, _, out := overLongRun(t)

	reason := line(t, out, "reason")
	if !strings.Contains(reason, "reading the agent's output") {
		t.Errorf("reason = %q, does not say the agent's output could not be read", reason)
	}
	if !strings.Contains(reason, "too long") {
		t.Errorf("reason = %q, does not name the line as too long", reason)
	}
}

func TestS3OutputBeforeTheOverLongLineIsStillInTheLog(t *testing.T) {
	l, run, _ := overLongRun(t)

	out := mustOwl(t, l, "logs", run).stdout
	if !strings.Contains(out, agentScript[0]) {
		t.Errorf("owl logs does not print the line before the over-long one:\n%s", out)
	}
}
