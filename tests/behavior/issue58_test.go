package behavior_test

// Behavior tests for issue #58. TestS3 maps to scenario S3 in
// tests/behavior/issue-58.md; S1 and S2 live with the run package, whose own
// API they exercise. This one drives the built owl binary against a daemon on
// a Project whose repository already holds the branch the first Job would be
// cut on.

import (
	"strings"
	"testing"
)

func TestS3OwlStartReportsTheBlockedJobAndTheNextStartMovesOn(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "first", "--no-plan")
	addJob(t, l, r.dir, "second", "--no-plan")
	rows := queueList(t, l)
	first, second := jobID(t, rows, "first"), jobID(t, rows, "second")
	// The branch the first Job would be cut on is already there.
	r.git("branch", "owl/job-"+first)

	res := runOwl(t, l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job whose branch already exists\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "owl/job-"+first) {
		t.Errorf("stderr does not name the branch:\n%s", res.stderr)
	}
	out := mustOwl(t, l, "jobs", "show", first).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "owl/job-"+first) {
		t.Errorf("the reason does not name the branch: %q", reason)
	}

	_, job := startRun(t, l)

	if job != second {
		t.Errorf("the next owl start started job %s, want the second job %s", job, second)
	}
}
