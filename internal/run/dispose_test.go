package run_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// reviewing puts one Job in review with a worktree of its own, as a Run that
// Verification accepted leaves it.
func reviewing(t *testing.T, st *store.Store, repo string) store.Job {
	t.Helper()
	ctx := context.Background()
	j := queueJob(t, st, "work")
	worktree := filepath.Join(t.TempDir(), "job")
	gitIn(t, repo, "worktree", "add", "-b", "owl/job", worktree, "main")
	if err := st.SetJobWorkspace(ctx, j.ID, "owl/job", worktree); err != nil {
		t.Fatalf("SetJobWorkspace: %v", err)
	}
	if err := st.SetJobState(ctx, j.ID, string(queue.StateReview)); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}
	j, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return j
}

func TestAcceptFinishesAJobWhoseWorktreeIsAlreadyGone(t *testing.T) {
	svc, st, repo := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := reviewing(t, st, repo)
	// Somebody removed the directory by hand, or a disposal failed halfway.
	if err := os.RemoveAll(j.Worktree); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Accept(context.Background(), j.ID, false)

	if err != nil {
		t.Fatalf("Accept: %v; a job whose worktree is gone can still be finished", err)
	}
	if got.State != queue.StateDone {
		t.Errorf("state = %s, want done", got.State)
	}
	if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, j.Worktree) {
		t.Errorf("git still reports the worktree that is not there:\n%s", list)
	}
}

func TestAcceptTakesAwayWhatWasKeptBesideTheWorktree(t *testing.T) {
	// Owl keeps the exclude file that hides a Job's Skills outside the
	// worktree, one directory per Job (ADR-0033). Accepting the Job reclaims
	// the worktree, and what was hiding its Skills has nothing left to hide.
	svc, st, repo, root := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{})
	j := reviewing(t, st, repo)
	owned := filepath.Join(root, "worktree-config", strconv.FormatInt(j.ID, 10))
	if err := os.MkdirAll(owned, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "excludes"), []byte("/.claude/skills/\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Accept(context.Background(), j.ID, false); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if _, err := os.Stat(owned); err == nil {
		t.Errorf("%s is still there after the worktree it belonged to went", owned)
	}
}

func TestDropRefusesAJobNobodyIsWaitingOn(t *testing.T) {
	svc, st, repo := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := reviewing(t, st, repo)
	if err := st.SetJobState(context.Background(), j.ID, string(queue.StateActive)); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}

	_, err := svc.Drop(context.Background(), j.ID, false)

	if err == nil {
		t.Fatal("dropping a job that is still running reported success")
	}
	if !strings.Contains(err.Error(), "active") {
		t.Errorf("error %q does not name the job's state", err)
	}
	if _, err := os.Stat(j.Worktree); err != nil {
		t.Errorf("the worktree of a job that was not dropped is gone: %v", err)
	}
}

// gitOut is gitIn for a command whose output the test reads.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestAcceptFinishesAJobWhoseWorktreeGitCannotUse(t *testing.T) {
	svc, st, repo := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := reviewing(t, st, repo)
	// What an interrupted removal leaves behind: the directory is there, but
	// it is no longer a worktree.
	if err := os.Remove(filepath.Join(j.Worktree, ".git")); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Accept(context.Background(), j.ID, false)

	if err != nil {
		t.Fatalf("Accept: %v; a half-removed worktree must not wedge the job", err)
	}
	if got.State != queue.StateDone {
		t.Errorf("state = %s, want done", got.State)
	}
	if list := gitOut(t, repo, "worktree", "list"); strings.Contains(list, j.Worktree) {
		t.Errorf("git still reports a worktree it cannot use:\n%s", list)
	}
}

func TestAcceptAndDropAtOnceLeaveOneDecisionStanding(t *testing.T) {
	svc, st, repo := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := reviewing(t, st, repo)

	// Two people deciding at once, from two terminals. Whichever wins, the
	// other must be refused: a drop that ran alongside an accept would delete
	// the branch the accept promised to keep.
	type outcome struct {
		verb string
		job  queue.Job
		err  error
	}
	results := make(chan outcome, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for verb, dispose := range map[string]func(context.Context, int64, bool) (queue.Job, error){
		"accept": svc.Accept, "drop": svc.Drop,
	} {
		go func() {
			ready.Done()
			ready.Wait()
			got, err := dispose(context.Background(), j.ID, false)
			results <- outcome{verb, got, err}
		}()
	}
	first, second := <-results, <-results

	won, lost := first, second
	if won.err != nil {
		won, lost = second, first
	}
	if won.err != nil {
		t.Fatalf("both disposals were refused: %v; %v", first.err, second.err)
	}
	if lost.err == nil {
		t.Fatalf("%s and %s both succeeded; one of them decided about a job the other had already disposed of",
			first.verb, second.verb)
	}
	final, err := st.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(final.State) != won.job.State {
		t.Errorf("the job is %s, but %s reported %s", final.State, won.verb, won.job.State)
	}
	branch := strings.Contains(gitOut(t, repo, "branch", "--list", "--format=%(refname:short)"), "owl/job")
	if want := won.verb == "accept"; branch != want {
		t.Errorf("the branch is there = %v after %s won, want %v", branch, won.verb, want)
	}
}

func TestOverviewCountsAJobInAStateItDoesNotKnow(t *testing.T) {
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := queueJob(t, st, "work")
	// What an older binary sees in a database a newer one wrote.
	if err := st.SetJobState(context.Background(), j.ID, "hibernating"); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}

	o, err := svc.Overview(context.Background())

	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if len(o.Counts) != 1 {
		t.Fatalf("counts = %+v, want the job counted rather than dropped", o.Counts)
	}
	if got := o.Counts[0]; string(got.State) != "hibernating" || got.Count != 1 {
		t.Errorf("counts = %+v, want one hibernating job", o.Counts)
	}
}

func TestPauseAndResumeRefuseWhenThereIsNoRunToActOn(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})

	if _, err := svc.Pause(context.Background(), run.ByUser); err == nil {
		t.Error("Pause reported success with nothing running")
	} else if !strings.Contains(err.Error(), "no run in progress") {
		t.Errorf("Pause error = %q, want it to say there is nothing running", err)
	}
	if _, err := svc.Resume(context.Background(), run.ByUser); err == nil {
		t.Error("Resume reported success with nothing running")
	} else if !strings.Contains(err.Error(), "no run in progress") {
		t.Errorf("Resume error = %q, want it to say there is nothing running", err)
	}
}

func TestRecoverQueuesWhatWasBeingCarriedOutAndLeavesDecisionsAlone(t *testing.T) {
	ctx := context.Background()
	svc, st, repo := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	// A job an earlier daemon was carrying out, whose run was already recorded
	// as interrupted: a daemon that died halfway through recovering.
	carried := queueJob(t, st, "carried out")
	if err := st.SetJobState(ctx, carried.ID, string(queue.StateActive)); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}
	// Its Run was already recorded as ended, which is what a daemon that died
	// between interrupting the runs it found and putting their jobs back
	// leaves behind.
	r, err := st.StartRun(ctx, store.Run{JobID: carried.ID, Started: time.Now().UTC(), LogPath: "/logs/1.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := st.FinishRun(ctx, r.ID, time.Now().UTC(), "interrupted", "the daemon stopped", -1); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	decided := reviewing(t, st, repo)

	if err := svc.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	if got := stateOf(t, st, carried.ID); got != queue.StatePending {
		t.Errorf("the job that was being carried out is %s, want pending", got)
	}
	// A job somebody decided about is not something recovery undoes.
	if got := stateOf(t, st, decided.ID); got != queue.StateReview {
		t.Errorf("the job waiting for a decision is %s, want review", got)
	}
}

func stateOf(t *testing.T, st *store.Store, id int64) queue.State {
	t.Helper()
	j, err := st.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return queue.State(j.State)
}

func TestPauseIsRefusedOnceTheDaemonIsStopping(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := svc.Pause(context.Background(), run.ByUser)

	// Freezing an Agent that has just been asked to stop would leave it unable
	// to hear that, and the daemon waiting out the kill delay on it.
	if err == nil {
		t.Fatal("Pause reported success while the daemon was stopping")
	}
	if !strings.Contains(err.Error(), "stopping") {
		t.Errorf("Pause error = %q, want it to say the daemon is stopping", err)
	}
}
