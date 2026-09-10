package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// queuedJob puts one pending Job in the store and returns it.
func queuedJob(t *testing.T, s *store.Store, prompt, ref string) store.Job {
	t.Helper()
	j, err := s.UpsertJob(context.Background(), job(prompt, ref))
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	return j
}

func TestStartRunNumbersTheAttempts(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	now := time.Now().UTC()

	first, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now, LogPath: "/logs/1.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.FinishRun(ctx, first.ID, now, "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	second, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now, LogPath: "/logs/2.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if first.Attempt != 1 || second.Attempt != 2 {
		t.Errorf("attempts are %d and %d, want 1 and 2", first.Attempt, second.Attempt)
	}
	if first.Outcome != "" || !first.Ended.IsZero() {
		t.Errorf("a run starts with outcome %q ended %v, want neither", first.Outcome, first.Ended)
	}
	if first.ExitCode != store.NoExitCode {
		t.Errorf("a run starts with exit code %d, want none", first.ExitCode)
	}
}

func TestFinishRunRecordsTheOutcome(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	started := time.Now().UTC()
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: started, LogPath: "/logs/1.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	ended := started.Add(time.Minute)
	if err := s.FinishRun(ctx, r.ID, ended, "failed", "agent exited with status 3", 3); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	got, err := s.GetRun(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Outcome != "failed" || got.Error != "agent exited with status 3" {
		t.Errorf("run = %+v, want a failed run carrying its reason", got)
	}
	if got.ExitCode != 3 {
		t.Errorf("exit code = %d, want the 3 the agent exited with", got.ExitCode)
	}
	if !got.Ended.Equal(ended) {
		t.Errorf("ended = %v, want %v", got.Ended, ended)
	}
	if err := s.FinishRun(ctx, 999, ended, "failed", "", 0); !errors.Is(err, store.ErrRunNotFound) {
		t.Errorf("FinishRun on an unknown run = %v, want ErrRunNotFound", err)
	}
	if _, err := s.GetRun(ctx, 999); !errors.Is(err, store.ErrRunNotFound) {
		t.Errorf("GetRun(999) = %v, want ErrRunNotFound", err)
	}
}

func TestRunInProgressFindsTheOneStillRunning(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")

	if _, ok, err := s.RunInProgress(ctx); err != nil || ok {
		t.Fatalf("RunInProgress on an idle daemon = %v, %v, want none", ok, err)
	}
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC(), LogPath: "/logs/1.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	got, ok, err := s.RunInProgress(ctx)

	if err != nil || !ok {
		t.Fatalf("RunInProgress = %v, %v, want the run that is going", ok, err)
	}
	if got.ID != r.ID || got.JobID != j.ID {
		t.Errorf("RunInProgress = %+v, want run %d for job %d", got, r.ID, j.ID)
	}
	if err := s.FinishRun(ctx, r.ID, time.Now().UTC(), "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if _, ok, _ := s.RunInProgress(ctx); ok {
		t.Error("a finished run is still reported as in progress")
	}
}

func TestListRunsReturnsAJobsRunsInOrder(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	other := queuedJob(t, s, "elsewhere", "b")
	now := time.Now().UTC()
	for _, id := range []int64{j.ID, j.ID, other.ID} {
		r, err := s.StartRun(ctx, store.Run{JobID: id, Started: now, LogPath: "/logs/x.jsonl"})
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if err := s.FinishRun(ctx, r.ID, now, "succeeded", "", 0); err != nil {
			t.Fatalf("FinishRun: %v", err)
		}
	}

	runs, err := s.ListRuns(ctx, j.ID)

	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 || runs[0].Attempt != 1 || runs[1].Attempt != 2 {
		t.Errorf("runs = %+v, want this job's two attempts in order", runs)
	}
}

func TestSetJobStateKeepsThePositionAndDequeueDropsIt(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	queuedJob(t, s, "next", "b")

	if err := s.SetJobState(ctx, j.ID, "active"); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}

	running, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if running.State != "active" || running.Position != 1 {
		t.Errorf("job = %+v, want an active job holding position 1", running)
	}
	if err := s.DequeueJob(ctx, j.ID, "review", ""); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}
	reviewed, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if reviewed.State != "review" || reviewed.Position != 0 {
		t.Errorf("job = %+v, want a job in review with no position", reviewed)
	}
	if err := s.SetJobState(ctx, 999, "active"); !errors.Is(err, store.ErrJobNotFound) {
		t.Errorf("SetJobState on an unknown job = %v, want ErrJobNotFound", err)
	}
}

func TestSetJobWorkspaceRecordsTheBranchAndWorktree(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")

	if err := s.SetJobWorkspace(ctx, j.ID, "owl/job-1", "/data/worktrees/1"); err != nil {
		t.Fatalf("SetJobWorkspace: %v", err)
	}

	got, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Branch != "owl/job-1" || got.Worktree != "/data/worktrees/1" {
		t.Errorf("job = %+v, want its branch and worktree recorded", got)
	}
}

func TestListQueueHoldsOnlyThePendingJobs(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	queuedJob(t, s, "next", "b")
	if err := s.SetJobState(ctx, j.ID, "active"); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}

	queue, err := s.ListQueue(ctx, "pending")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	all, err := s.ListAllJobs(ctx)
	if err != nil {
		t.Fatalf("ListAllJobs: %v", err)
	}

	if len(queue) != 1 || queue[0].Prompt != "next" {
		t.Errorf("queue = %+v, want only the pending job", queue)
	}
	if len(all) != 2 {
		t.Errorf("all jobs = %+v, want both", all)
	}
}
