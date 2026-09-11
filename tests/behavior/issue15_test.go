package behavior_test

// Behavior tests for issue #15. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-15.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// exercise the garbage collection task that keeps the worktrees honest.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// onDemand is the daemon configuration the scenarios about `owl gc` share: it
// reports a Job that has waited any time at all for a decision, so that
// nothing waits on a clock, and it collects only when asked, so that nothing
// collects behind the scenario's back.
const onDemand = `apiVersion: codingowl.dev/v1
garbageCollection:
  interval: 24h
  reviewAfter: 1ns
`

// unasked is onDemand for the scenarios about the collection nobody asks for:
// there the daemon collects on its own, often.
const unasked = `apiVersion: codingowl.dev/v1
garbageCollection:
  interval: 200ms
  reviewAfter: 1ns
`

// gcLayout is a layout whose stub agent commits a file and whose daemon
// collects only when asked. The Agent commits, so a Job in review has work on
// its branch and a worktree worth reclaiming.
func gcLayout(t *testing.T) (*layout, *stub) {
	t.Helper()
	l, s := committingLayout(t)
	globalConfig(t, l, onDemand)
	return l, s
}

// patientLayout is gcLayout with a daemon that leaves a Job waiting for a
// decision rather than reporting it, for the scenarios that are not about the
// threshold.
func patientLayout(t *testing.T) (*layout, *stub) {
	t.Helper()
	l, s := committingLayout(t)
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\ngarbageCollection:\n  interval: 24h\n  reviewAfter: 24h\n")
	return l, s
}

// unaskedLayout is gcLayout with a daemon that collects on its own, for the
// scenarios about the collection nobody asks for.
func unaskedLayout(t *testing.T) (*layout, *stub) {
	t.Helper()
	l, s := committingLayout(t)
	globalConfig(t, l, unasked)
	return l, s
}

// gcReport is what owl gc printed, as lines.
func gcReport(t *testing.T, l *layout) string {
	t.Helper()
	return mustOwl(t, l, "gc").stdout
}

// disposedWithItsWorktreeBack accepts or drops a Job and puts its worktree back
// where it was, so that garbage collection has something to reclaim: disposal
// takes the worktree itself, and what is being tested here is the reconciling
// of a worktree the daemon did not take.
func disposedWithItsWorktreeBack(t *testing.T, l *layout, verb string) (r *repo, job, worktree string) {
	t.Helper()
	r, job, worktree, branch := reviewed(t, l)
	mustOwl(t, l, "jobs", verb, job)
	// The worktree is put back on the Job's own branch, or on a branch of its
	// own where dropping took it.
	if verb == "drop" {
		branch = "owl/put-back-" + job
		r.git("worktree", "add", "-b", branch, worktree)
	} else {
		r.git("worktree", "add", worktree, branch)
	}
	return r, job, worktree
}

func TestS1GCReclaimsAWorktreeWhoseJobIsFinishedWith(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	_, _, worktree := disposedWithItsWorktreeBack(t, l, "accept")

	out := gcReport(t, l)

	if !strings.Contains(out, worktree) {
		t.Errorf("owl gc does not name the worktree it reclaimed:\n%s", out)
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree of an accepted job is still at %s", worktree)
	}
}

func TestS2GCReclaimsAWorktreeWhoseJobWasCancelled(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	_, _, worktree := disposedWithItsWorktreeBack(t, l, "drop")

	out := gcReport(t, l)

	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree of a cancelled job is still at %s", worktree)
	}
	if !strings.Contains(out, worktree) {
		t.Errorf("owl gc does not name the worktree it reclaimed:\n%s", out)
	}
}

func TestS3GCReclaimsAWorktreeBelongingToNoJob(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	stray := worktreeDir(l, "999")
	r.git("worktree", "add", "-b", "owl/stray", stray)

	out := gcReport(t, l)

	if _, err := os.Stat(stray); err == nil {
		t.Errorf("a worktree belonging to no job is still at %s", stray)
	}
	if !strings.Contains(out, stray) {
		t.Errorf("owl gc does not name the worktree it reclaimed:\n%s", out)
	}
}

func TestS4GCLeavesADirectoryThatIsNotAWorktree(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	project(t, l, "api")
	notAWorktree := worktreeDir(l, "notes")
	if err := os.MkdirAll(notAWorktree, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notAWorktree, "mine.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := gcReport(t, l)

	if _, err := os.Stat(notAWorktree); err != nil {
		t.Errorf("a directory that is not a worktree was taken: %v", err)
	}
	if strings.Contains(out, notAWorktree) {
		t.Errorf("owl gc reports a directory that is none of its business:\n%s", out)
	}
}

func TestS5GCReportsAWorktreeHoldingUncommittedChanges(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	_, _, worktree := disposedWithItsWorktreeBack(t, l, "accept")
	stray := filepath.Join(worktree, "not-committed.txt")
	if err := os.WriteFile(stray, []byte("unfinished\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := gcReport(t, l)

	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("garbage collection destroyed work nobody had committed: %v", err)
	}
	unfinished := section(t, out, "unfinished work:")
	if !strings.Contains(unfinished, worktree) {
		t.Errorf("owl gc does not report the worktree as unfinished work:\n%s", out)
	}
	if !strings.Contains(unfinished, "committed") {
		t.Errorf("owl gc does not say what is unfinished about it:\n%s", out)
	}
}

func TestS6GCLeavesAWorktreeWhoseJobIsStillGoing(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	r, job, worktree, _ := reviewed(t, l)
	addJob(t, l, r.dir, "the next one", "--no-plan")

	out := gcReport(t, l)

	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree of a job in review went: %v", err)
	}
	// A heading that is not there is the strongest possible form of "it was
	// not reclaimed", so this reads the section only when there is one.
	if strings.Contains(out, "reclaimed:") && strings.Contains(section(t, out, "reclaimed:"), worktree) {
		t.Errorf("owl gc reclaimed the worktree of job %s, which is still in review:\n%s", job, out)
	}
}

func TestS7GCPrunesStaleWorktreeEntriesPerProject(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	r, _, worktree, _ := reviewed(t, l)
	// The directory goes without git being told, which is what leaves the
	// administrative entry git keeps reporting.
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.git("worktree", "list"), worktree) {
		t.Fatalf("git does not report the worktree whose directory went:\n%s", r.git("worktree", "list"))
	}

	out := gcReport(t, l)

	if got := r.git("worktree", "list"); strings.Contains(got, worktree) {
		t.Errorf("git still reports a worktree nobody can use:\n%s", got)
	}
	if !strings.Contains(out, "api") {
		t.Errorf("owl gc does not say which project it pruned:\n%s", out)
	}
}

func TestS8GCAcceptsAJobWhoseBranchIsInTheBaseBranch(t *testing.T) {
	l, _ := patientLayout(t)
	daemonUp(t, l)
	r, job, worktree, branch := reviewed(t, l)
	r.git("merge", "--no-ff", "-m", "merge the job's work", branch)

	out := gcReport(t, l)

	if got := jobState(t, l, job); got != "done" {
		t.Errorf("state = %q, want a job whose branch is merged to be done", got)
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree of an accepted job is still at %s", worktree)
	}
	accepted := section(t, out, "accepted:")
	if !strings.Contains(accepted, job) {
		t.Errorf("owl gc does not say it accepted job %s:\n%s", job, out)
	}
	if !strings.Contains(accepted, branch) {
		t.Errorf("owl gc does not say why it accepted the job:\n%s", out)
	}
}

func TestS9GCLeavesAJobWhoseBranchIsNotMerged(t *testing.T) {
	l, _ := patientLayout(t)
	daemonUp(t, l)
	_, job, worktree, _ := reviewed(t, l)

	gcReport(t, l)

	if got := jobState(t, l, job); got != "review" {
		t.Errorf("state = %q, want a job whose branch is not merged to wait in review", got)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree of a job still in review went: %v", err)
	}
}

func TestS10GCReportsAJobWaitingTooLongForADecision(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	_, job, worktree, _ := reviewed(t, l)

	out := gcReport(t, l)

	unfinished := section(t, out, "unfinished work:")
	if !strings.Contains(unfinished, job) {
		t.Errorf("owl gc does not report job %s as waiting for a decision:\n%s", job, out)
	}
	if !strings.Contains(unfinished, "decision") {
		t.Errorf("owl gc does not say what is unfinished about it:\n%s", out)
	}
	if got := jobState(t, l, job); got != "review" {
		t.Errorf("state = %q, want the job left where it was", got)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("reporting a job took its worktree: %v", err)
	}
}

func TestS11AJobLeftActiveByADeadDaemonIsQueuedAgainNotReported(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, _ := agentLayout(t, script, 0)
	globalConfig(t, l, onDemand)
	p := daemonUp(t, l)
	runnableJob(t, l, "work")
	run, job := startRun(t, l)
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p.exit(t, 10*time.Second)
	waitForLog(t, startDaemon(t, l), "daemon listening")

	out := gcReport(t, l)

	// The daemon that starts up ends the Run and queues the Job again
	// (ADR-0011), so there is nothing for garbage collection to report: the
	// Job is not stuck, it is waiting its turn.
	if strings.Contains(out, "job "+job) || strings.Contains(out, "no daemon is running it") {
		t.Errorf("owl gc reports job %s, which the daemon has already queued again:\n%s", job, out)
	}
	if got := jobState(t, l, job); got != "pending" {
		t.Errorf("state = %q, want pending: the job goes back in the queue", got)
	}
	if got := runRowOf(t, l, job, run).outcome; got != "interrupted" {
		t.Errorf("run outcome = %q, want interrupted", got)
	}
}

func TestS12StatusListsUnfinishedWork(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, _ := writingLayout(t, map[string]string{"work.txt": "the agent's work\n"}, true, script)
	// Patient about a decision, so that a Job in review is what awaits one
	// rather than being unfinished work in its own right.
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\ngarbageCollection:\n  interval: 24h\n  reviewAfter: 24h\n")
	p := daemonUp(t, l)

	// A worktree holding changes nobody committed, which is S5's kind.
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")
	release(t, l)
	out, first := finishedJob(t, l)
	worktree := line(t, out, "worktree")
	mustOwl(t, l, "jobs", "accept", first)
	r.git("worktree", "add", worktree, line(t, out, "branch"))
	if err := os.WriteFile(filepath.Join(worktree, "not-committed.txt"), []byte("unfinished\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A Job merely awaiting a decision, which is not unfinished work.
	second := newRepo(t, l, "web")
	addProject(t, l, second)
	addJob(t, l, second.dir, "waiting", "--no-plan")
	release(t, l)
	waitingOut, waiting := finishedJob(t, l)
	if got := line(t, waitingOut, "state"); got != "review" {
		t.Fatalf("state = %q, want a job awaiting a decision:\n%s", got, waitingOut)
	}

	// A Job left active by a daemon that died, which is S11's kind: the next
	// daemon queues it again (ADR-0011), so it is not unfinished work. The
	// stub is held first, so the Run is certainly still in progress when the
	// daemon is killed rather than being a race against it finishing.
	hold(t, l)
	addJob(t, l, r.dir, "interrupted", "--no-plan")
	_, abandoned := startRun(t, l)
	if got := jobState(t, l, abandoned); got != "active" {
		t.Fatalf("state = %q, want a run still in progress to kill the daemon under", got)
	}
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	p.exit(t, 10*time.Second)
	waitForLog(t, startDaemon(t, l), "daemon listening")

	status := mustOwl(t, l, "status").stdout

	unfinished := section(t, status, "unfinished work:")
	for _, want := range []string{worktree, "committed"} {
		if !strings.Contains(unfinished, want) {
			t.Errorf("owl status does not report %q as unfinished work:\n%s", want, status)
		}
	}
	if strings.Contains(unfinished, "job "+abandoned) {
		t.Errorf("owl status reports job %s as unfinished work, but the daemon queued it again:\n%s", abandoned, status)
	}
	if got := jobState(t, l, abandoned); got != "pending" {
		t.Errorf("state = %q, want pending: what a dead daemon was carrying out is queued again", got)
	}
	// What is merely waiting for a person is its own list, and has been since
	// issue #8; unfinished work is what nothing is going to resolve on its own.
	awaiting := section(t, status, "awaiting a decision:")
	if !strings.Contains(awaiting, waiting) {
		t.Errorf("owl status does not list job %s as awaiting a decision:\n%s", waiting, status)
	}
	if strings.Contains(unfinished, "job "+waiting+" ") {
		t.Errorf("owl status reports a job merely awaiting a decision as unfinished work:\n%s", status)
	}
	if strings.Contains(section(t, status, "jobs:"), "unfinished") {
		t.Errorf("owl status counts unfinished work as a job state:\n%s", status)
	}
}

// release lets a stub agent waiting on a `#wait` line continue, and hold takes
// that permission back, so a scenario can hold exactly the Run it means to.
// The stub waits for the file to exist, so its absence is what holds a Run.
func release(t *testing.T, l *layout) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.root, "agent-release"), []byte("go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hold(t *testing.T, l *layout) {
	t.Helper()
	if err := os.Remove(filepath.Join(l.root, "agent-release")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestS13GCWithNothingToDoSaysSo(t *testing.T) {
	l, _ := patientLayout(t)
	daemonUp(t, l)
	project(t, l, "api")

	out := gcReport(t, l)

	if !strings.Contains(out, "nothing to do") {
		t.Errorf("owl gc with nothing to do says %q", strings.TrimSpace(out))
	}
}

func TestS14GCRunsWhenTheDaemonStarts(t *testing.T) {
	// A daemon that collects only when asked, so that what reclaims the
	// worktree is this scenario's restart and nothing else.
	l, _ := gcLayout(t)
	d := daemonUp(t, l)
	_, _, worktree := disposedWithItsWorktreeBack(t, l, "accept")
	stopDaemon(t, d)

	started := daemonUp(t, l)

	waitForGone(t, worktree)
	waitForLog(t, started, worktree)
}

func TestS15GCRunsAgainOnItsInterval(t *testing.T) {
	l, _ := unaskedLayout(t)
	daemonUp(t, l)

	// The worktree becomes reclaimable while the daemon is already running, so
	// only a later collection can take it.
	_, _, worktree := disposedWithItsWorktreeBack(t, l, "accept")

	waitForGone(t, worktree)
}

func TestS16GCNeverRunsAnAgent(t *testing.T) {
	l, s := unaskedLayout(t)
	d := daemonUp(t, l)
	// A Job that has run, so the stub has a record and the collections below
	// have a worktree to reclaim: this is the state S14 and S15 leave behind.
	r, job, worktree, branch := reviewed(t, l)
	mustOwl(t, l, "jobs", "accept", job)
	before := len(s.invocations(t))

	// The collection a daemon makes when it starts, and the one it makes on
	// its interval, both inside the window this counts across: the worktree is
	// put back after the restart, so only an unasked collection can take it.
	stopDaemon(t, d)
	waitForLog(t, startDaemon(t, l), "daemon listening")
	r.git("worktree", "add", worktree, branch)
	waitForGone(t, worktree)

	if after := len(s.invocations(t)); after != before {
		t.Errorf("the agent was invoked %d times, want the %d it was before the collections", after, before)
	}
	if before == 0 {
		t.Error("the stub agent recorded nothing at all, so this proves nothing about garbage collection")
	}
}

func TestS17GCReportsAStoppedDaemon(t *testing.T) {
	l := newLayout(t)

	res := runOwl(t, l, "gc")

	if res.code == 0 {
		t.Fatalf("owl gc with no daemon exited 0\nstdout:\n%s", res.stdout)
	}
	for _, want := range []string{"not running", l.socket()} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, res.stderr)
		}
	}
}

// section is the part of out under a heading, up to the blank line that ends
// it. A heading that is not there is a failure: a scenario asking for one
// wants what is under it.
func section(t *testing.T, out, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(out, heading)
	if !ok {
		t.Fatalf("no %q in:\n%s", heading, out)
	}
	var lines []string
	for _, ln := range strings.Split(strings.TrimPrefix(rest, "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			break
		}
		lines = append(lines, ln)
	}
	return strings.Join(lines, "\n")
}

// waitForGone waits for a path to stop being there, which is how a scenario
// waits for a collection it did not ask for.
func waitForGone(t *testing.T, path string) {
	t.Helper()
	waitFor(t, path+" to be reclaimed", func() bool {
		_, err := os.Stat(path)
		return err != nil
	})
}
