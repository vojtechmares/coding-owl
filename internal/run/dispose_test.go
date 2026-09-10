package run_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
