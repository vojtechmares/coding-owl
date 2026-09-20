package behavior_test

// Behavior tests for issue #119. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-119.md. They drive the built owl binary against a
// daemon whose configuration is edited while it runs, and read what the next
// garbage collection did about it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// impatient and patient are the two ends of the reviewAfter setting: one
// reports a Job that has waited any time at all for a decision, the other
// reports nothing for a day. The interval is a day in both, so that nothing
// collects behind the scenario's back.
const impatient = `apiVersion: codingowl.dev/v1
garbageCollection:
  interval: 24h
  reviewAfter: 1ns
`

const patient = `apiVersion: codingowl.dev/v1
garbageCollection:
  interval: 24h
  reviewAfter: 24h
`

// waitingFor reports whether a report names the Job as having waited for a
// decision. The heading is only there when something is unfinished, which is
// why this reads the whole report rather than the section.
func waitingFor(report, job string) bool {
	if !strings.Contains(report, "unfinished work:") {
		return false
	}
	return strings.Contains(report, "job "+job+" ") &&
		strings.Contains(report, "waiting for a decision")
}

// strayWorktree leaves a worktree belonging to no Job where garbage collection
// will find it, and returns where it is.
func strayWorktree(t *testing.T, l *layout, r *repo, id string) string {
	t.Helper()
	stray := worktreeDir(l, id)
	r.git("worktree", "add", "-b", "owl/stray-"+id, stray)
	return stray
}

func TestS1ALongerReviewAfterIsUsedByTheNextCollection(t *testing.T) {
	l, _ := committingLayout(t)
	globalConfig(t, l, impatient)
	daemonUp(t, l)
	_, job, _, _ := reviewed(t, l)
	if before := gcReport(t, l); !waitingFor(before, job) {
		t.Fatalf("owl gc does not report job %s as waiting, so there is nothing to stop reporting:\n%s", job, before)
	}

	globalConfig(t, l, patient)

	if after := gcReport(t, l); waitingFor(after, job) {
		t.Errorf("owl gc still reports job %s as waiting, although reviewAfter is now a day:\n%s", job, after)
	}
}

func TestS2AShorterReviewAfterIsUsedByTheNextCollection(t *testing.T) {
	l, _ := committingLayout(t)
	globalConfig(t, l, patient)
	d := daemonUp(t, l)
	_, job, _, _ := reviewed(t, l)
	if before := gcReport(t, l); waitingFor(before, job) {
		t.Fatalf("owl gc reports job %s as waiting although reviewAfter is a day:\n%s", job, before)
	}

	globalConfig(t, l, impatient)

	if after := gcReport(t, l); !waitingFor(after, job) {
		t.Errorf("owl gc does not report job %s as waiting, although reviewAfter is now an instant:\n%s", job, after)
	}
	// The same daemon throughout: what is being tested is that nothing had to
	// be restarted, and a daemon that started twice would say so twice.
	if got := strings.Count(d.out(), "daemon listening"); got != 1 {
		t.Errorf("the daemon logged %d starts, want the one it began with:\n%s", got, d.out())
	}
}

func TestS3StatusReadsTheSameSettingAtTheSameMoment(t *testing.T) {
	l, _ := committingLayout(t)
	globalConfig(t, l, patient)
	daemonUp(t, l)
	_, job, _, _ := reviewed(t, l)

	globalConfig(t, l, impatient)

	status := mustOwl(t, l, "status").stdout
	if !strings.Contains(status, "unfinished work:") {
		t.Fatalf("owl status reports no unfinished work at all:\n%s", status)
	}
	unfinished := section(t, status, "unfinished work:")
	if !strings.Contains(unfinished, "job "+job+" ") {
		t.Errorf("owl status does not list job %s as unfinished work:\n%s", job, status)
	}
}

func TestS4ALongerIntervalStopsTheFrequentCollections(t *testing.T) {
	l, _ := committingLayout(t)
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\ngarbageCollection:\n  interval: 200ms\n  reviewAfter: 1ns\n")
	daemonUp(t, l)
	r := project(t, l, "api")
	// It collects on its own to begin with, which is what the edit below has
	// to stop.
	waitForGone(t, strayWorktree(t, l, r, "901"))

	globalConfig(t, l, impatient)
	// Long enough for a collection that was already waiting out the old
	// interval to have happened, so what follows is only about the new one.
	time.Sleep(time.Second)

	stray := strayWorktree(t, l, r, "902")
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("a worktree was reclaimed although the daemon is now waiting a day between collections: %v", err)
	}
	// And asking still collects: the interval is when it happens by itself,
	// not whether it happens at all.
	if out := gcReport(t, l); !strings.Contains(out, stray) {
		t.Errorf("owl gc does not reclaim the worktree when asked:\n%s", out)
	}
}

func TestS5AConfigurationThatCannotBeReadLeavesTheDefaultsInPlace(t *testing.T) {
	l, _ := committingLayout(t)
	globalConfig(t, l, impatient)
	daemonUp(t, l)
	r, job, _, _ := reviewed(t, l)
	if before := gcReport(t, l); !waitingFor(before, job) {
		t.Fatalf("owl gc does not report job %s as waiting to begin with:\n%s", job, before)
	}
	stray := strayWorktree(t, l, r, "903")

	broken := filepath.Join(l.config, "coding-owl", "config.yaml")
	if err := os.WriteFile(broken, []byte("this: [is not, a configuration file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "gc")
	if res.code != 0 {
		t.Fatalf("owl gc exited %d with a configuration it cannot read\nstdout:\n%s\nstderr:\n%s",
			res.code, res.stdout, res.stderr)
	}
	if _, err := os.Stat(stray); err == nil {
		t.Errorf("garbage collection stopped reclaiming when the configuration became unreadable: %s is still there", stray)
	}
	if waitingFor(res.stdout, job) {
		t.Errorf("owl gc still reports job %s as waiting, although the file it read that from cannot be read:\n%s",
			job, res.stdout)
	}
}

func TestS6WhatIsSettledAtStartupIsStillSettledAtStartup(t *testing.T) {
	l, s := agentLayout(t, agentScript, 0)
	// A daemon whose PATH does not hold the tool, so the only thing that can
	// tell it where the tool is, is the claudePath it started with.
	l = l.withoutEnv("PATH").withEnv("PATH=" + fakeMachineDir + string(os.PathListSeparator) + os.Getenv("PATH"))
	globalConfig(t, l, fileStore+"claudePath: "+filepath.Join(fakeClaudeDir, "claude")+"\n")
	daemonUp(t, l)
	runnableJob(t, l, "work")

	globalConfig(t, l, fileStore+"claudePath: "+filepath.Join(l.root, "no-such-claude")+"\n")

	_, job := startRun(t, l)
	out := finished(t, l, job)

	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review: the run should use the tool the daemon started with\n%s", got, out)
	}
	if got := s.invoked(t).Argv[0]; got != filepath.Join(fakeClaudeDir, "claude") {
		t.Errorf("the agent was invoked as %q, want the path the daemon started with", got)
	}
}
