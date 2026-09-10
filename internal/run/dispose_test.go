package run_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/queue"
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
