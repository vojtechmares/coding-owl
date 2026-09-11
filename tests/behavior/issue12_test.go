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
	"syscall"
	"testing"
	"time"
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

// rebasingJobHeld is rebasingJob whose next Agent waits before it does
// anything, so a scenario can look at a worktree only the rebase has touched.
func rebasingJobHeld(t *testing.T, files map[string]string) *rebasing {
	t.Helper()
	rb := rebasingJobOn(t, files, nil, "OWL_FAKE_CLAUDE_HOLD=1")
	// The planning Run was let go; the next one is not.
	if err := os.Remove(rb.stub.release); err != nil {
		t.Fatal(err)
	}
	return rb
}

// heldAgent waits until the Agent of the Run just started is holding, so what
// a scenario reads is the rebase's work and nothing else.
func (rb *rebasing) heldAgent(t *testing.T) {
	t.Helper()
	waitFor(t, "the agent to be holding", func() bool {
		return len(rb.stub.invocations(t)) >= 2
	})
}

// rebasingJobOn is rebasingJob with files committed to the base branch before
// the Job's branch is cut from it, so that a later change to them is a change
// to something the two sides share.
func rebasingJobOn(t *testing.T, files, base map[string]string, env ...string) *rebasing {
	t.Helper()
	all := map[string]string{handoffPath: "# Handoff\n\nstep one done\n"}
	for path, body := range files {
		all[path] = body
	}
	l, s := writingLayout(t, all, true, agentScript)
	l = l.withEnv(env...)
	if len(env) > 0 {
		// Held from the start, and let go for the planning Run below.
		s.let(t)
	}
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
	// Held before it does anything: the second Run's Agent writes the same
	// file with the same bytes, so a worktree it had touched would say nothing
	// about what the rebase replayed.
	rb := rebasingJobHeld(t, map[string]string{"work.txt": agentWork})
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	worktree := rb.worktree(t)

	mustOwl(t, rb.l, "start")
	rb.heldAgent(t)

	if body, err := os.ReadFile(filepath.Join(worktree, "work.txt")); err != nil {
		t.Errorf("what the agent committed is gone: %v", err)
	} else if string(body) != agentWork {
		t.Errorf("what the agent committed reads %q, want %q", body, agentWork)
	}
	if got := rb.repo.git("show", rb.branch(t)+":work.txt"); got != agentWork {
		t.Errorf("the job's branch no longer carries the agent's commit: %q", got)
	}
	// On top of the new base: a rebase that quietly did nothing would still
	// have left the agent's work where it was.
	base := strings.TrimSpace(rb.repo.git("rev-parse", "main"))
	if out := rb.gitInWorktree(t, "merge-base", "--is-ancestor", base, "HEAD"); out != "" {
		t.Errorf("the agent's commit is not on top of the new base: %s", out)
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

	if midRebase(t, rb.repo, worktree) {
		t.Errorf("the worktree is still mid-rebase:\n%s", gitIn(t, rb.repo, worktree, "status"))
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
	// A branch of somebody else's pointing at one of the Job's own commits,
	// which is the one a rebase could carry along with it, and an uncommitted
	// file in the Project.
	rb.repo.git("branch", "someone-else", rb.branch(t))
	elsewhere := strings.TrimSpace(rb.repo.git("rev-parse", "someone-else"))
	// The setting that tells git to carry other branches along with the
	// commits it rewrites, which an Agent can set in the repository its
	// worktree shares.
	rb.repo.git("config", "rebase.updateRefs", "true")
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
	if !midRebase(t, rb.repo, worktree) {
		t.Errorf("owl finished somebody else's rebase:\n%s", gitIn(t, rb.repo, worktree, "status"))
	}
}

// midRebase reports whether a worktree is in the middle of a rebase. git says
// so in several wordings depending on where it stopped, so this asks for the
// directory git keeps the rebase in rather than reading its prose.
func midRebase(t *testing.T, r *repo, worktree string) bool {
	t.Helper()
	dir := strings.TrimSpace(gitIn(t, r, worktree, "rev-parse", "--git-path", "rebase-merge"))
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(worktree, dir)
	}
	if _, err := os.Stat(dir); err == nil {
		return true
	}
	apply := strings.TrimSpace(gitIn(t, r, worktree, "rev-parse", "--git-path", "rebase-apply"))
	if !filepath.IsAbs(apply) {
		apply = filepath.Join(worktree, apply)
	}
	_, err := os.Stat(apply)
	return err == nil
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
	reason := line(t, out, "reason")
	for _, want := range []string{"someone-else", "not on the job's own branch"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not say %q: %q", want, reason)
		}
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

func TestS17ARebaseThatStopsPartWayBlocksTheJob(t *testing.T) {
	rb := rebasingJobOn(t,
		map[string]string{"work.txt": agentWork},
		map[string]string{".gitattributes": "marked.txt filter=owlstop\n"})
	// A filter that refuses content one of the branch's own commits carries
	// and a later one takes away, so git meets it while replaying and never
	// before: somebody else's program, named by configuration an Agent can
	// write.
	rb.stopsPartWay(t)
	worktree := rb.worktree(t)
	before := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "HEAD"))
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job whose rebase stopped part way\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "could not be carried out") {
		t.Errorf("the reason does not say the rebase could not be carried out: %q", reason)
	}
	if midRebase(t, rb.repo, worktree) {
		t.Errorf("the worktree is still mid-rebase:\n%s", gitIn(t, rb.repo, worktree, "status"))
	}
	if got := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where it was, %s", got, before)
	}
	if got := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != rb.branch(t) {
		t.Errorf("the worktree is on %q, want the job's own branch", got)
	}
}

// slowFilter makes the Project require a filter that takes seconds over the
// second thing git filters, so a rebase is under way for long enough to be
// interrupted, and records that it got there.
func (rb *rebasing) slowFilter(t *testing.T, marker string) {
	t.Helper()
	rb.stopsWhileReplaying(t, "touch "+marker+"; sleep 3")
}

func TestS19AClientThatGoesAwayLeavesNoRebaseInProgress(t *testing.T) {
	rb := rebasingJobOn(t,
		map[string]string{"work.txt": agentWork},
		map[string]string{".gitattributes": "marked.txt filter=owlstop\n"})
	worktree := rb.worktree(t)
	marker := filepath.Join(rb.l.root, "rebasing")
	rb.slowFilter(t, marker)
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")

	// Started and then killed while git is in the middle of the rebase.
	cmd := exec.Command(owlBin, "start")
	cmd.Env = rb.l.env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the rebase to be under way", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// The rebase finishes on its own rather than being killed half way, which
	// is what leaves a worktree nobody can use.
	deadline := time.Now().Add(20 * time.Second)
	for midRebase(t, rb.repo, worktree) {
		if time.Now().After(deadline) {
			t.Fatalf("the worktree is still mid-rebase twenty seconds after the client went away:\n%s",
				gitIn(t, rb.repo, worktree, "status"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(worktree, "from-base.txt")); err != nil {
		t.Errorf("the base branch's commit did not reach the worktree: %v", err)
	}
	if got := strings.TrimSpace(gitIn(t, rb.repo, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != rb.branch(t) {
		t.Errorf("the worktree is on %q, want the job's own branch", got)
	}
	// Given a moment to write down anything it was going to.
	sleepABit()
	if got := jobState(t, rb.l, rb.job); got == "blocked" {
		t.Errorf("the job is blocked, and nothing went wrong with it: %q",
			line(t, mustOwl(t, rb.l, "jobs", "show", rb.job).stdout, "reason"))
	}
	rb.stub.let(t)
}

// stopsPartWay makes the Project require a filter that fails the second thing
// git filters and lets everything after it through, which stops a rebase after
// it has begun rather than before it starts.
func (rb *rebasing) stopsPartWay(t *testing.T) {
	t.Helper()
	rb.stopsWhileReplaying(t, "rm -f \"$t\"; exit 1")
}

// midRebaseMark is content that exists in one of the Job branch's commits and
// in none of its later ones. A filter watching for it therefore fires while
// the rebase is replaying that commit - after the rebase has begun, and never
// before, because the worktree does not hold it.
const midRebaseMark = "only-while-replaying"

// stopsWhileReplaying makes the Project require a filter that does what onMark
// says when git writes that content, and puts a commit carrying it - and a
// later commit removing it - on the Job's branch.
func (rb *rebasing) stopsWhileReplaying(t *testing.T, onMark string) {
	t.Helper()
	worktree := rb.worktree(t)
	gitIn(t, rb.repo, worktree, "config", "user.name", "Owl Test")
	gitIn(t, rb.repo, worktree, "config", "user.email", "owl@example.com")
	if err := os.WriteFile(filepath.Join(worktree, "marked.txt"), []byte(midRebaseMark+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, rb.repo, worktree, "add", "--", "marked.txt")
	gitIn(t, rb.repo, worktree, "commit", "-m", "a commit whose content the filter reacts to")
	gitIn(t, rb.repo, worktree, "rm", "-q", "--", "marked.txt")
	gitIn(t, rb.repo, worktree, "commit", "-m", "and one that takes it away again")

	path := filepath.Join(rb.l.root, "filter.sh")
	script := "#!/bin/sh\nt=$(mktemp)\ncat > \"$t\"\n" +
		"if grep -q " + midRebaseMark + " \"$t\"; then\n  " + onMark + "\nfi\n" +
		"cat \"$t\"\nrm -f \"$t\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	rb.repo.git("config", "filter.owlstop.clean", path)
	rb.repo.git("config", "filter.owlstop.smudge", path)
	rb.repo.git("config", "filter.owlstop.required", "true")
}

func TestS18AWorktreeThatIsGoneBlocksTheJobNotTheQueue(t *testing.T) {
	rb := rebasingJob(t, nil)
	// A second Job, so that the queue can be seen to carry on.
	addJob(t, rb.l, rb.repo.dir, "the next one", "--no-plan")
	if err := os.RemoveAll(rb.worktree(t)); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start ran a job whose worktree is gone\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	// Not merely the path, which the worktree's own directory name would
	// satisfy whatever the reason said.
	if reason := line(t, out, "reason"); !strings.Contains(reason, "not a worktree git can work in") {
		t.Errorf("the reason does not say the worktree is not one git can work in: %q", reason)
	}
	// And the queue is not held by it.
	_, next := startRun(t, rb.l)
	if next == rb.job {
		t.Errorf("owl start ran job %s again, want the next one in the queue", next)
	}
	rb.stub.let(t)
}

// sleepABit gives the daemon a moment to write down what it decided, where
// waiting for a condition would be waiting for something not to happen.
func sleepABit() { time.Sleep(500 * time.Millisecond) }

func TestS20AWorktreeOfAnotherRepositoryIsNotRebased(t *testing.T) {
	rb := rebasingJob(t, nil)
	addJob(t, rb.l, rb.repo.dir, "the next one", "--no-plan")
	worktree := rb.worktree(t)
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	// Somebody else's repository, with a worktree of its own at the path the
	// Job's used to be: git can work in it, and it is not the Project's.
	other := newRepo(t, rb.l, "elsewhere")
	other.git("worktree", "add", "-q", "-b", "theirs", worktree, "main")
	before := strings.TrimSpace(other.git("rev-parse", "theirs"))
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")

	res := runOwl(t, rb.l, "start")

	if res.code == 0 {
		t.Fatalf("owl start rebased a worktree of another repository\nstdout:\n%s", res.stdout)
	}
	out := mustOwl(t, rb.l, "jobs", "show", rb.job).stdout
	if got := line(t, out, "state"); got != "blocked" {
		t.Errorf("state = %q, want blocked", got)
	}
	if reason := line(t, out, "reason"); !strings.Contains(reason, "another repository") {
		t.Errorf("the reason does not say the worktree belongs to another repository: %q", reason)
	}
	if got := strings.TrimSpace(other.git("rev-parse", "theirs")); got != before {
		t.Errorf("the other repository's branch moved from %s to %s", before, got)
	}
	if midRebase(t, other, worktree) {
		t.Errorf("the other repository's worktree was left mid-rebase")
	}
	_, next := startRun(t, rb.l)
	if next == rb.job {
		t.Errorf("owl start ran job %s again, want the next one in the queue", next)
	}
	rb.stub.let(t)
}

