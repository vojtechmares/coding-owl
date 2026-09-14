package behavior_test

// Behavior tests for issue #48. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-48.md. The scenarios here drive the built owl binary
// against a daemon whose Project's setup command sleeps, so that a Job can be
// seen while its Run is being set up. The store and run package scenarios of
// the same sheet live in their own packages' tests.

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// settingUp brings up a daemon with one Job in a Project whose setup command
// sleeps, runs owl start in the background, and returns once the Job is being
// set up. started receives what owl start printed when it returns.
func settingUp(t *testing.T) (l *layout, job string, started <-chan result) {
	t.Helper()
	l, _ = agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nsetup:\n  - sleep 3\n", "configure owl")
	addProject(t, l, r)
	addJob(t, l, r.dir, "work", "--no-plan")
	job = jobID(t, queueList(t, l), "work")

	done := make(chan result, 1)
	go func() {
		// Run directly rather than through the harness, which fails the test
		// from the goroutine it would otherwise be running in.
		cmd := exec.Command(owlBin, "start")
		cmd.Env = l.env
		out, err := cmd.Output()
		var exitErr *exec.ExitError
		res := result{stdout: string(out)}
		if errors.As(err, &exitErr) {
			res.code, res.stderr = exitErr.ExitCode(), string(exitErr.Stderr)
		} else if err != nil {
			res.code, res.stderr = -1, err.Error()
		}
		done <- res
	}()
	waitFor(t, "the job to be set up", func() bool { return jobState(t, l, job) == "active" })
	return l, job, done
}

// awaitStart waits for the background owl start of settingUp to return, and
// fails when it did not start the Run it was there to start.
func awaitStart(t *testing.T, started <-chan result) {
	t.Helper()
	select {
	case res := <-started:
		if res.code != 0 || startedRE.FindStringSubmatch(res.stdout) == nil {
			t.Fatalf("owl start exited %d without reporting a run:\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("owl start did not return")
	}
}

// refusedAsActive checks that a queue command was refused because the Job is
// being run rather than waiting.
func refusedAsActive(t *testing.T, what string, res result) {
	t.Helper()
	if res.code == 0 {
		t.Errorf("%s exited 0 for a job being set up:\n%s", what, res.stdout)
	}
	if !strings.Contains(res.stderr, "not pending") || !strings.Contains(res.stderr, "active") {
		t.Errorf("%s stderr = %q, does not say the job is not pending, it is active", what, res.stderr)
	}
}

func TestS6JobBeingSetUpCannotBeCancelled(t *testing.T) {
	l, job, started := settingUp(t)

	res := runOwl(t, l, "queue", "remove", job)

	refusedAsActive(t, "owl queue remove", res)
	awaitStart(t, started)
	out := finished(t, l, job)
	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want the run to have gone on to review", got)
	}
}

func TestS7JobBeingSetUpCannotBeReordered(t *testing.T) {
	l, job, started := settingUp(t)

	res := runOwl(t, l, "queue", "reorder", job, "1")

	refusedAsActive(t, "owl queue reorder", res)
	awaitStart(t, started)
}
