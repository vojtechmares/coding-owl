package behavior_test

// Behavior tests for issue #8. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-8.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// carry a Job through to a decision.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reviewed runs one Job to the end of its execution Run and returns the
// Project, the Job's id and where its worktree is. The Agent commits a file, so
// there is work to accept or drop.
func reviewed(t *testing.T, l *layout) (r *repo, job, worktree, branch string) {
	t.Helper()
	r = project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")
	out, job := finishedJob(t, l)
	if got := line(t, out, "state"); got != "review" {
		t.Fatalf("state = %q, want a job in review to dispose of:\n%s", got, out)
	}
	return r, job, line(t, out, "worktree"), line(t, out, "branch")
}

// committingLayout is a layout whose stub agent commits a file, so a Job in
// review has work on its branch.
func committingLayout(t *testing.T) (*layout, *stub) {
	t.Helper()
	return writingLayout(t, map[string]string{"work.txt": "the agent's work\n"}, true, agentScript)
}

// hasBranch reports whether the Project still carries that branch.
func hasBranch(t *testing.T, r *repo, branch string) bool {
	t.Helper()
	for _, ln := range strings.Split(r.git("branch", "--list", "--format=%(refname:short)"), "\n") {
		if strings.TrimSpace(ln) == branch {
			return true
		}
	}
	return false
}

// listsWorktreePath reports whether git still knows about a worktree there.
func listsWorktreePath(t *testing.T, r *repo, worktree string) bool {
	t.Helper()
	return listsWorktree(t, r, worktree)
}

func TestS1DisposeAcceptKeepsTheBranchAndRemovesTheWorktree(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, job, worktree, branch := reviewed(t, l)

	mustOwl(t, l, "jobs", "accept", job)

	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree %s is still on disk", worktree)
	}
	if listsWorktreePath(t, r, worktree) {
		t.Errorf("git still reports the worktree:\n%s", r.git("worktree", "list"))
	}
	if !hasBranch(t, r, branch) {
		t.Errorf("the branch %s is gone; accept keeps it", branch)
	}
	if got := r.git("show", branch+":work.txt"); got != "the agent's work\n" {
		t.Errorf("the branch no longer carries the agent's commit: %q", got)
	}
	out := mustOwl(t, l, "jobs", "show", job).stdout
	if got := line(t, out, "state"); got != "done" {
		t.Errorf("state = %q, want done", got)
	}
	if got := line(t, out, "worktree"); got != "(none)" {
		t.Errorf("worktree = %q, want none: it has been reclaimed", got)
	}
}

func TestS2DisposeDropRemovesTheWorktreeAndTheBranch(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, job, worktree, branch := reviewed(t, l)

	mustOwl(t, l, "jobs", "drop", job)

	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree %s is still on disk", worktree)
	}
	if listsWorktreePath(t, r, worktree) {
		t.Errorf("git still reports the worktree:\n%s", r.git("worktree", "list"))
	}
	if hasBranch(t, r, branch) {
		t.Errorf("the branch %s is still there; drop deletes it", branch)
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state"); got != "cancelled" {
		t.Errorf("state = %q, want cancelled", got)
	}
}

func TestS3DisposeAcceptRefusesAJobThatIsNotInReview(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")

	res := runOwl(t, l, "jobs", "accept", "1")

	if res.code == 0 {
		t.Fatalf("accepting a pending job exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "pending") {
		t.Errorf("stderr does not name the job's state:\n%s", res.stderr)
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", "1").stdout, "state"); got != "pending" {
		t.Errorf("state = %q, want the job untouched", got)
	}
}

func TestS4DisposeDropRefusesAJobThatIsNotInReview(t *testing.T) {
	l, _ := agentLayout(t, agentScript, 0)
	daemonUp(t, l)
	// A check that refuses the work leaves the Job blocked rather than in
	// review.
	checkedProject(t, l, `apiVersion: codingowl.dev/v1
checks:
  - name: guard
    run: exit 1
`)
	out, job := finishedJob(t, l)
	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want a blocked job", got)
	}
	worktree := line(t, out, "worktree")

	res := runOwl(t, l, "jobs", "drop", job)

	if res.code == 0 {
		t.Fatalf("dropping a blocked job exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "blocked") {
		t.Errorf("stderr does not name the job's state:\n%s", res.stderr)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree of a job that was not disposed of is gone: %v", err)
	}
}

func TestS5DisposeRefusesAnUnknownJob(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)

	for _, verb := range []string{"accept", "drop"} {
		res := runOwl(t, l, "jobs", verb, "999")
		if res.code == 0 {
			t.Errorf("owl jobs %s 999 exited 0\nstdout:\n%s", verb, res.stdout)
		}
		if !strings.Contains(res.stderr, "999") {
			t.Errorf("owl jobs %s stderr does not name 999:\n%s", verb, res.stderr)
		}
	}
}

func TestS6DisposeAcceptRefusesUncommittedWorkUnlessTold(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	_, job, worktree, _ := reviewed(t, l)
	stray := filepath.Join(worktree, "stray.txt")
	if err := os.WriteFile(stray, []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "jobs", "accept", job)

	if res.code == 0 {
		t.Fatalf("accept threw away uncommitted work without being told to\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "commit") {
		t.Errorf("stderr does not say the worktree has uncommitted changes:\n%s", res.stderr)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the uncommitted file is gone after a refused accept: %v", err)
	}

	mustOwl(t, l, "jobs", "accept", job, "--force")

	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree %s survived a forced accept", worktree)
	}
	if got := line(t, mustOwl(t, l, "jobs", "show", job).stdout, "state"); got != "done" {
		t.Errorf("state = %q, want done", got)
	}
}

func TestS7DisposeDropRefusesUncommittedWorkUnlessTold(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, job, worktree, branch := reviewed(t, l)
	if err := os.WriteFile(filepath.Join(worktree, "stray.txt"), []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "jobs", "drop", job)

	if res.code == 0 {
		t.Fatalf("drop threw away uncommitted work without being told to\nstdout:\n%s", res.stdout)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree is gone after a refused drop: %v", err)
	}
	if !hasBranch(t, r, branch) {
		t.Errorf("the branch is gone after a refused drop")
	}

	mustOwl(t, l, "jobs", "drop", job, "--force")

	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree %s survived a forced drop", worktree)
	}
	if hasBranch(t, r, branch) {
		t.Errorf("the branch %s survived a forced drop", branch)
	}
}

// statusCounts parses the states section of owl status.
func statusCounts(t *testing.T, out string) map[string]string {
	t.Helper()
	_, rest, ok := strings.Cut(out, "jobs:\n")
	if !ok {
		return nil
	}
	counts := map[string]string{}
	for _, ln := range strings.Split(rest, "\n") {
		if strings.TrimSpace(ln) == "" {
			break
		}
		fields := strings.Fields(ln)
		if len(fields) != 2 || fields[0] == "STATE" {
			continue
		}
		counts[fields[0]] = fields[1]
	}
	return counts
}

// threeStates leaves one Job in review, one blocked and one pending.
func threeStates(t *testing.T, l *layout) (reviewJob, blockedJob string) {
	t.Helper()
	// The first Project's checks pass, so its Job lands in review.
	good := project(t, l, "api")
	addJob(t, l, good.dir, "review me", "--no-plan")
	out, reviewJob := finishedJob(t, l)
	if got := line(t, out, "state"); got != "review" {
		t.Fatalf("state = %q, want review", got)
	}
	// The second Project's check refuses the work.
	bad := newRepo(t, l, "web")
	bad.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nchecks:\n  - name: guard\n    run: exit 1\n", "configure owl")
	addProject(t, l, bad)
	addJob(t, l, bad.dir, "block me", "--no-plan")
	out, blockedJob = finishedJob(t, l)
	if got := line(t, out, "state"); got != "blocked" {
		t.Fatalf("state = %q, want blocked", got)
	}
	// And one that has not run.
	addJob(t, l, good.dir, "wait for me", "--no-plan")
	return reviewJob, blockedJob
}

func TestS8DisposeStatusCountsTheJobsByState(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	threeStates(t, l)

	res := mustOwl(t, l, "status")

	counts := statusCounts(t, res.stdout)
	for _, state := range []string{"review", "blocked", "pending"} {
		if counts[state] != "1" {
			t.Errorf("owl status reports %q jobs in %s, want 1:\n%s", counts[state], state, res.stdout)
		}
	}
}

func TestS9DisposeStatusListsWhatAwaitsADecision(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	reviewJob, blockedJob := threeStates(t, l)

	out := mustOwl(t, l, "status").stdout

	awaiting, _, ok := strings.Cut(out, "blocked:")
	if !ok {
		t.Fatalf("owl status has no blocked section:\n%s", out)
	}
	_, awaiting, ok = strings.Cut(awaiting, "awaiting a decision:")
	if !ok {
		t.Fatalf("owl status does not say what awaits a decision:\n%s", out)
	}
	for _, want := range []string{reviewJob, "api", "review me"} {
		if !strings.Contains(awaiting, want) {
			t.Errorf("the awaiting section does not name %q:\n%s", want, out)
		}
	}
	_, blocked, _ := strings.Cut(out, "blocked:")
	for _, want := range []string{blockedJob, "guard"} {
		if !strings.Contains(blocked, want) {
			t.Errorf("the blocked section does not name %q:\n%s", want, out)
		}
	}
}

func TestS10DisposeStatusShowsARunInProgress(t *testing.T) {
	script := []string{agentScript[0], "#wait", agentScript[2]}
	l, s := agentLayout(t, script, 0)
	daemonUp(t, l)
	r := project(t, l, "api")
	addJob(t, l, r.dir, "work", "--no-plan")
	run, job := startRun(t, l)

	out := mustOwl(t, l, "status").stdout

	_, running, ok := strings.Cut(out, "runs in progress:")
	if !ok {
		t.Fatalf("owl status does not report the run in progress:\n%s", out)
	}
	for _, want := range []string{run, job, "execute"} {
		if !strings.Contains(running, want) {
			t.Errorf("the running section does not name %q:\n%s", want, out)
		}
	}
	s.let(t)
	waitRun(t, l, job, run)
}

func TestS11DisposeStatusWithNothingToReportSaysSo(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)

	res := mustOwl(t, l, "status")

	if !strings.Contains(res.stdout, "nothing") {
		t.Errorf("owl status does not say there is nothing to report:\n%s", res.stdout)
	}
}

func TestS12DisposeNeverPushesAnything(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, accepted, _, _ := reviewed(t, l)
	// A remote to push to, if anything were inclined to.
	bare := filepath.Join(l.root, "origin.git")
	r.git("init", "--bare", bare)
	r.git("remote", "add", "origin", bare)
	addJob(t, l, r.dir, "second", "--no-plan")
	_, dropped := finishedJob(t, l)

	mustOwl(t, l, "jobs", "accept", accepted)
	mustOwl(t, l, "jobs", "drop", dropped)

	refs := strings.TrimSpace(gitIn(t, r, bare, "for-each-ref"))
	if refs != "" {
		t.Errorf("the remote holds refs, so something pushed:\n%s", refs)
	}
	if got := strings.TrimSpace(r.git("rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Errorf("the project's checkout is on %q, want main", got)
	}
}

func TestS13DisposeCommandsReportAStoppedDaemon(t *testing.T) {
	l := newLayout(t)

	for _, args := range [][]string{{"jobs", "accept", "1"}, {"jobs", "drop", "1"}, {"status"}} {
		res := runOwl(t, l, args...)
		if res.code == 0 {
			t.Errorf("owl %v exited 0 with no daemon running:\n%s", args, res.stdout)
		}
		if !strings.Contains(res.stderr, "daemon not running") || !strings.Contains(res.stderr, l.socket()) {
			t.Errorf("owl %v stderr does not report a stopped daemon at %s:\n%s", args, l.socket(), res.stderr)
		}
	}
}

func TestS14DisposeAcceptedWorkSurvivesTheWorktree(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, job, worktree, branch := reviewed(t, l)

	mustOwl(t, l, "jobs", "accept", job)

	if got := r.git("show", branch+":work.txt"); got != "the agent's work\n" {
		t.Errorf("the accepted work is not on the branch: %q", got)
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree %s is still there", worktree)
	}
}
