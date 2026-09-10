package behavior_test

// Behavior tests for issue #12. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-12.md. A Job needs two Runs for a rebase to be possible
// at all, so these scenarios use a planned Job: its planning Run ends, the Job
// returns to the queue, the base branch moves, and the execution Run is what
// has to rebase.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rebasing is a Job whose planning Run has ended, so its branch exists and its
// next Run is the one that rebases.
type rebasing struct {
	l    *layout
	stub *stub
	repo *repo
	job  string
}

// agentWork is what the stub agent commits in the first Run, and what a rebase
// has to replay.
const agentWork = "the agent's work\n"

func rebasingJob(t *testing.T, files map[string]string) *rebasing {
	t.Helper()
	return rebasingJobOn(t, files, nil)
}

// rebasingJobOn is rebasingJob with files committed to the base branch before
// the Job's branch is cut from it, so that a later change to them is a change
// to something the two sides share.
func rebasingJobOn(t *testing.T, files, base map[string]string) *rebasing {
	t.Helper()
	all := map[string]string{handoffPath: "# Handoff\n\nstep one done\n"}
	for path, body := range files {
		all[path] = body
	}
	l, s := writingLayout(t, all, true, agentScript)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	for path, body := range base {
		r.commit(path, body, "before the job")
	}
	addProject(t, l, r)
	addJob(t, l, r.dir, "work")
	_, job := finishedJob(t, l)
	if got := jobState(t, l, job); got != "pending" {
		t.Fatalf("state after the planning run = %q, want the job pending again", got)
	}
	return &rebasing{l: l, stub: s, repo: r, job: job}
}

// worktree is where the Job's branch is checked out.
func (rb *rebasing) worktree(t *testing.T) string {
	t.Helper()
	return line(t, mustOwl(t, rb.l, "jobs", "show", rb.job).stdout, "worktree")
}

func (rb *rebasing) branch(t *testing.T) string {
	t.Helper()
	return line(t, mustOwl(t, rb.l, "jobs", "show", rb.job).stdout, "branch")
}

// moveBase adds a commit to the Project's base branch, which is what a rebase
// has to bring in.
func (rb *rebasing) moveBase(t *testing.T, path, body string) {
	t.Helper()
	rb.repo.commit(path, body, "move the base")
}

// gitInWorktree runs git in the Job's worktree.
func (rb *rebasing) gitInWorktree(t *testing.T, args ...string) string {
	t.Helper()
	return gitIn(t, rb.repo, rb.worktree(t), args...)
}

func TestS1RebaseBringsTheMovedBaseIntoTheWorktree(t *testing.T) {
	rb := rebasingJob(t, nil)
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	worktree := rb.worktree(t)

	res := mustOwl(t, rb.l, "start")

	if !strings.Contains(res.stdout, "started run") {
		t.Fatalf("owl start did not start a run:\n%s", res.stdout)
	}
	if body, err := os.ReadFile(filepath.Join(worktree, "from-base.txt")); err != nil {
		t.Errorf("the base branch's new file is not in the worktree: %v", err)
	} else if string(body) != "added while the job was waiting\n" {
		t.Errorf("the base branch's new file reads %q", body)
	}
	// What a rebase leaves behind: the base is an ancestor, and the branch is
	// linear on top of it rather than joined to it by a merge.
	base := strings.TrimSpace(rb.repo.git("rev-parse", "main"))
	if out := rb.gitInWorktree(t, "merge-base", "--is-ancestor", base, "HEAD"); out != "" {
		t.Errorf("the base is not an ancestor of the job's branch: %s", out)
	}
	if merges := strings.TrimSpace(rb.gitInWorktree(t, "rev-list", "--count", "--merges", base+"..HEAD")); merges != "0" {
		t.Errorf("the job's branch carries %s merges above the base, want none", merges)
	}
	rb.stub.let(t)
}

func TestS2RebaseReplaysWhatTheAgentCommitted(t *testing.T) {
	rb := rebasingJob(t, map[string]string{"work.txt": agentWork})
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	worktree := rb.worktree(t)

	mustOwl(t, rb.l, "start")

	if body, err := os.ReadFile(filepath.Join(worktree, "work.txt")); err != nil {
		t.Errorf("what the agent committed is gone: %v", err)
	} else if string(body) != agentWork {
		t.Errorf("what the agent committed reads %q, want %q", body, agentWork)
	}
	if got := rb.repo.git("show", rb.branch(t)+":work.txt"); got != agentWork {
		t.Errorf("the job's branch no longer carries the agent's commit: %q", got)
	}
	rb.stub.let(t)
}

func TestS3VerificationRunsAgainstTheRebasedState(t *testing.T) {
	rb := rebasingJob(t, nil)
	// A check that only passes when the base branch's new file is there, and
	// the file, in the same commit.
	rb.repo.write("from-base.txt", "added while the job was waiting\n")
	rb.repo.write(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nchecks:\n  - name: rebased\n    run: test -f from-base.txt\n")
	rb.repo.git("add", "--", "from-base.txt", ".coding-owl.yaml")
	rb.repo.git("commit", "-m", "move the base")

	out, _ := finishedJob(t, rb.l)

	if !strings.Contains(out, "rebased: passed") {
		t.Errorf("the check against the rebased state did not pass:\n%s", out)
	}
	if got := line(t, out, "state"); got != "review" {
		t.Errorf("state = %q, want review", got)
	}
}

// conflicting sets up a Job whose Agent changed a file and a base branch that
// changed the same lines of it.
func conflicting(t *testing.T, files ...string) *rebasing {
	t.Helper()
	agent := map[string]string{}
	for _, path := range files {
		agent[path] = "what the agent wrote\n"
	}
	rb := rebasingJob(t, agent)
	for _, path := range files {
		rb.repo.write(path, "what the base says\n")
		rb.repo.git("add", "--", path)
	}
	rb.repo.git("commit", "-m", "move the base")
	return rb
}

func TestS4ConflictBlocksTheJobNamingThePaths(t *testing.T) {
	rb := conflicting(t, "work.txt")

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job whose rebase conflicts\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "work.txt") {
		t.Errorf("stderr does not name the conflicting file:\n%s", res.stderr)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "work.txt") {
		t.Errorf("the reason does not name the conflicting file: %q", reason)
	}
	// The attempt started no Run: nothing was carried out.
	if rows := runRows(t, out); len(rows) != 1 {
		t.Errorf("the job has %d runs, want only the planning run", len(rows))
	}
}

func TestS5ConflictLeavesNoRebaseInProgress(t *testing.T) {
	rb := conflicting(t, "work.txt")
	worktree := rb.worktree(t)
	before := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "HEAD"))

	runOwl(t, rb.l, "start")

	status := gitIn(t, rb.repo, worktree, "status")
	if strings.Contains(status, "rebase in progress") {
		t.Errorf("the worktree is still mid-rebase:\n%s", status)
	}
	if got := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want the commit the agent left, %s", got, before)
	}
	if got := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != rb.branch(t) {
		t.Errorf("the worktree is on %q, want the job's own branch", got)
	}
}

func TestS6EveryConflictingPathIsNamed(t *testing.T) {
	rb := conflicting(t, "one.txt", "two.txt")

	runOwl(t, rb.l, "start")

	reason := line(t, mustOwl(t, rb.l, "jobs", "show", rb.job).stdout, "reason")
	for _, want := range []string{"one.txt", "two.txt"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not name %q: %q", want, reason)
		}
	}
}

func TestS7TheFirstRunHasNothingToRebase(t *testing.T) {
	l, s := writingLayout(t, map[string]string{"work.txt": agentWork}, true, agentScript)
	daemonUp(t, l)
	r := runnableJob(t, l, "work")
	base := strings.TrimSpace(r.git("rev-parse", "main"))

	out, job := finishedJob(t, l)

	if got := line(t, out, "state"); got != "review" {
		t.Fatalf("state = %q, want a job that ran cleanly:\n%s", got, out)
	}
	branch := line(t, out, "branch")
	if got := r.git("show", branch+":work.txt"); got != agentWork {
		t.Errorf("the agent's commit is not on the branch: %q", got)
	}
	if out := r.git("merge-base", "--is-ancestor", base, branch); out != "" {
		t.Errorf("the branch is not cut from the base: %s", out)
	}
	_ = s
	_ = job
}

func TestS8OnlyTheJobsBranchIsRebased(t *testing.T) {
	rb := rebasingJob(t, map[string]string{"work.txt": agentWork})
	// A branch of somebody else's, and an uncommitted file in the Project.
	rb.repo.git("branch", "someone-else")
	elsewhere := strings.TrimSpace(rb.repo.git("rev-parse", "someone-else"))
	rb.repo.write("mine.txt", "not committed\n")
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	head := strings.TrimSpace(rb.repo.git("rev-parse", "HEAD"))

	mustOwl(t, rb.l, "start")

	if got := strings.TrimSpace(rb.repo.git("rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Errorf("the project's checkout is on %q, want main", got)
	}
	if got := strings.TrimSpace(rb.repo.git("rev-parse", "HEAD")); got != head {
		t.Errorf("the project's checkout moved to %s, want %s", got, head)
	}
	if got := strings.TrimSpace(rb.repo.git("rev-parse", "someone-else")); got != elsewhere {
		t.Errorf("somebody else's branch moved to %s, want %s", got, elsewhere)
	}
	if body, err := os.ReadFile(filepath.Join(rb.repo.dir, "mine.txt")); err != nil || string(body) != "not committed\n" {
		t.Errorf("the project's uncommitted file reads %q, %v", body, err)
	}
	rb.stub.let(t)
}

func TestS9RebasingNeverPushesAnything(t *testing.T) {
	rb := rebasingJob(t, nil)
	bare := filepath.Join(rb.l.root, "origin.git")
	rb.repo.git("init", "--bare", bare)
	rb.repo.git("remote", "add", "origin", bare)
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")

	mustOwl(t, rb.l, "start")

	if refs := strings.TrimSpace(gitIn(t, rb.repo, bare, "for-each-ref")); refs != "" {
		t.Errorf("the remote holds refs, so something pushed:\n%s", refs)
	}
	rb.stub.let(t)
}

func TestS10TheBaseBranchIsFetchedBeforeTheRebase(t *testing.T) {
	rb := rebasingJob(t, nil)
	// A remote the Project tracks, which somebody else has moved on.
	bare := filepath.Join(rb.l.root, "origin.git")
	rb.repo.git("init", "--bare", bare)
	rb.repo.git("remote", "add", "origin", bare)
	rb.repo.git("push", "--quiet", "origin", "main")
	other := filepath.Join(rb.l.root, "other")
	// --branch: a bare repository initialised here has its HEAD on whatever
	// git's default is, so a plain clone would start unborn somewhere else.
	rb.repo.git("clone", "--quiet", "--branch", "main", bare, other)
	gitIn(t, rb.repo, other, "-c", "user.name=Someone", "-c", "user.email=someone@example.com",
		"commit", "--allow-empty", "-m", "pushed by somebody else")
	gitIn(t, rb.repo, other, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	pushed := strings.TrimSpace(gitIn(t, rb.repo, other, "rev-parse", "HEAD"))
	local := strings.TrimSpace(rb.repo.git("rev-parse", "main"))

	mustOwl(t, rb.l, "start")

	if got := strings.TrimSpace(rb.repo.git("rev-parse", "refs/remotes/origin/main")); got != pushed {
		t.Errorf("the remote-tracking branch is at %s, want the commit that was pushed, %s", got, pushed)
	}
	if got := strings.TrimSpace(rb.repo.git("rev-parse", "main")); got != local {
		t.Errorf("owl moved the project's own base branch to %s, want it left at %s", got, local)
	}
	rb.stub.let(t)
}

func TestS11AProjectWithNoRemoteRebasesWithoutComplaint(t *testing.T) {
	rb := rebasingJob(t, nil)
	if remotes := strings.TrimSpace(rb.repo.git("remote")); remotes != "" {
		t.Fatalf("the project has remotes: %q", remotes)
	}
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")

	res := mustOwl(t, rb.l, "start")

	if !strings.Contains(res.stdout, "started run") {
		t.Errorf("owl start did not start a run:\n%s", res.stdout)
	}
	rb.stub.let(t)
}

func TestS12ARemoteThatCannotBeReachedDoesNotStopTheRun(t *testing.T) {
	rb := rebasingJob(t, nil)
	rb.repo.git("remote", "add", "origin", filepath.Join(rb.l.root, "not-there.git"))
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	worktree := rb.worktree(t)

	res := mustOwl(t, rb.l, "start")

	if !strings.Contains(res.stdout, "started run") {
		t.Fatalf("owl start did not start a run:\n%s", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(worktree, "from-base.txt")); err != nil {
		t.Errorf("the job was not rebased onto the local base branch: %v", err)
	}
	rb.stub.let(t)
}

func TestS13AWorktreeLeftMidRebaseIsReported(t *testing.T) {
	rb := conflicting(t, "work.txt")
	worktree := rb.worktree(t)
	// A rebase somebody started and never finished, by hand.
	if out := gitInAllowingFailure(t, rb.repo, worktree, "rebase", "main"); !strings.Contains(out, "CONFLICT") {
		t.Fatalf("the rebase by hand did not conflict:\n%s", out)
	}

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start took over a worktree that was mid-rebase\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "already in progress") {
		t.Errorf("the reason does not say a rebase is already in progress: %q", reason)
	}
	if status := gitIn(t, rb.repo, worktree, "status"); !strings.Contains(status, "rebase in progress") {
		t.Errorf("owl finished somebody else's rebase:\n%s", status)
	}
}

// gitInAllowingFailure runs git in a directory and returns what it said,
// whether or not it succeeded: a rebase that conflicts exits non-zero, and
// that is what the scenario is arranging.
func gitInAllowingFailure(t *testing.T, r *repo, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = r.env
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestS14AJobBlockedByAConflictKeepsItsWork(t *testing.T) {
	rb := conflicting(t, "work.txt")
	worktree, branch := rb.worktree(t), rb.branch(t)

	runOwl(t, rb.l, "start")

	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the job's worktree is gone: %v", err)
	}
	if !hasBranch(t, rb.repo, branch) {
		t.Errorf("the job's branch %s is gone", branch)
	}
	if got := rb.repo.git("show", branch+":work.txt"); got != "what the agent wrote\n" {
		t.Errorf("the agent's commit is not on the branch: %q", got)
	}
}

func TestS15AWorktreeNotOnTheJobsBranchIsNotRebased(t *testing.T) {
	rb := rebasingJob(t, nil)
	worktree := rb.worktree(t)
	// An Agent that moved its worktree onto somebody else's branch.
	rb.repo.git("branch", "someone-else")
	elsewhere := strings.TrimSpace(rb.repo.git("rev-parse", "someone-else"))
	gitIn(t, rb.repo, worktree, "checkout", "-q", "someone-else")
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start rebased a worktree that is on somebody else's branch\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "someone-else") {
		t.Errorf("the reason does not say what the worktree is on: %q", reason)
	}
	if got := strings.TrimSpace(rb.repo.git("rev-parse", "someone-else")); got != elsewhere {
		t.Errorf("somebody else's branch moved to %s, want %s", got, elsewhere)
	}
}

func TestS16AConflictInWhatNobodyCommittedKeepsIt(t *testing.T) {
	rb := rebasingJobOn(t,
		map[string]string{"other.txt": agentWork},
		map[string]string{"both.txt": "shared\n"})
	worktree := rb.worktree(t)
	// Changed in the worktree and never committed, in the lines the base is
	// about to change.
	if err := os.WriteFile(filepath.Join(worktree, "both.txt"), []byte("shared\nfrom the agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rb.moveBase(t, "both.txt", "shared\nfrom the base\n")

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job whose uncommitted changes conflict\nstdout:\n%s", res.stdout)
	}
	reason := line(t, mustOwl(t, rb.l, "jobs", "show", rb.job).stdout, "reason")
	for _, want := range []string{"both.txt", "stash"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not mention %q: %q", want, reason)
		}
	}
	if list := rb.repo.git("stash", "list"); strings.TrimSpace(list) == "" {
		t.Errorf("what nobody committed is not in the stash")
	}
}

func TestS17ARebaseThatCannotBeCarriedOutBlocksTheJob(t *testing.T) {
	rb := rebasingJob(t, map[string]string{"work.txt": agentWork})
	worktree := rb.worktree(t)
	before := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "HEAD"))
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	// Signing that cannot work, which is what a daemon with no terminal gets
	// from an ordinary developer's configuration. Set after the fixture's own
	// commits, which would not be able to sign either.
	rb.repo.git("config", "commit.gpgsign", "true")
	rb.repo.git("config", "gpg.program", filepath.Join(rb.l.root, "no-such-gpg"))

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job whose rebase could not be carried out\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "could not be carried out") {
		t.Errorf("the reason does not say the rebase could not be carried out: %q", reason)
	}
	if status := gitIn(t, rb.repo, worktree, "status"); strings.Contains(status, "rebase in progress") {
		t.Errorf("the worktree is still mid-rebase:\n%s", status)
	}
	if got := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where it was, %s", got, before)
	}
}
