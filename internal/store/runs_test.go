package store_test

import (
	"context"
	"errors"
	"strings"
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

func TestStartRunRecordsTheSystemPromptTheRunIsGiven(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	prompt := "You are running unattended.\n\nThis project also asks that you:\n\n- run gofmt"

	started, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC(), SystemPrompt: prompt})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	got, err := s.GetRun(ctx, started.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	listed, err := s.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	for what, r := range map[string]store.Run{"StartRun": started, "GetRun": got, "ListRuns": listed[0]} {
		if r.SystemPrompt != prompt {
			t.Errorf("%s reports the system prompt as %q, want the one the run was started with", what, r.SystemPrompt)
		}
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
	if err := s.DequeueJob(ctx, j.ID, "active", "review", ""); err != nil {
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

func TestListRunsInProgressLeavesOutTheRunsThatEnded(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	first := queuedJob(t, s, "first", "a")
	second := queuedJob(t, s, "second", "b")
	now := time.Now().UTC()
	ended, err := s.StartRun(ctx, store.Run{JobID: first.ID, Started: now, LogPath: "/logs/1.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.FinishRun(ctx, ended.ID, now, "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	going, err := s.StartRun(ctx, store.Run{JobID: second.ID, Started: now, LogPath: "/logs/2.jsonl"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	rs, err := s.ListRunsInProgress(ctx)
	if err != nil {
		t.Fatalf("ListRunsInProgress: %v", err)
	}

	if len(rs) != 1 {
		t.Fatalf("%d runs in progress, want only the one that has not ended: %+v", len(rs), rs)
	}
	if rs[0].ID != going.ID || rs[0].JobID != second.ID {
		t.Errorf("run %d of job %d, want run %d of job %d", rs[0].ID, rs[0].JobID, going.ID, second.ID)
	}
}

// attemptsLeft is how many Runs the store says a Job may still take.
func attemptsLeft(t *testing.T, s *store.Store, id int64) int {
	t.Helper()
	j, err := s.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return j.TTL
}

// jobWithAttempts queues a Job that may take n Runs.
func jobWithAttempts(t *testing.T, s *store.Store, ref string, n int) store.Job {
	t.Helper()
	j := job("work", ref)
	j.TTL = n
	out, err := s.UpsertJob(context.Background(), j)
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	return out
}

func TestFailRunRewritesHowAnEndedRunEndedAndRefusesOneStillGoing(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 3)
	now := time.Now().UTC()
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if err := s.FailRun(ctx, r.ID, "too soon"); err == nil || !strings.Contains(err.Error(), "not ended") {
		t.Errorf("FailRun of a run still going = %v, want a refusal saying it has not ended: only an ended run has an end to rewrite", err)
	}
	if err := s.FailRun(ctx, r.ID+100, "nobody"); !errors.Is(err, store.ErrRunNotFound) {
		t.Errorf("FailRun of a run that is not there = %v, want ErrRunNotFound", err)
	}
	if err := s.FinishRun(ctx, r.ID, now, "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if err := s.FailRun(ctx, r.ID, "the job could not be moved on"); err != nil {
		t.Fatalf("FailRun: %v", err)
	}

	got, err := s.GetRun(ctx, r.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Outcome != "failed" || got.Error != "the job could not be moved on" {
		t.Errorf("run = %+v, want it failed for the reason given", got)
	}
	if !got.Ended.Equal(now) {
		t.Errorf("ended = %v, want %v: rewriting the outcome keeps when the run ended", got.Ended, now)
	}
	after, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if after.TTL != 2 {
		t.Errorf("attempts left = %d, want 2: the attempt the run spent stays spent, once", after.TTL)
	}
}

func TestFinishRunSpendsOneOfTheJobsAttempts(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 3)
	now := time.Now().UTC()
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if err := s.FinishRun(ctx, r.ID, now, "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	if got := attemptsLeft(t, s, j.ID); got != 2 {
		t.Errorf("the job has %d attempts left after one run, want two", got)
	}
}

func TestFinishRunSpendsNothingTwiceForOneRun(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 3)
	now := time.Now().UTC()
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.FinishRun(ctx, r.ID, now, "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	if err := s.FinishRun(ctx, r.ID, now, "failed", "", 1); err == nil {
		t.Error("FinishRun on a run that has already ended = nil, want an error")
	}

	if got := attemptsLeft(t, s, j.ID); got != 2 {
		t.Errorf("the job has %d attempts left, want the two one run left it", got)
	}
}

func TestFinishRunNeverTakesAJobPastItsLastAttempt(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 1)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now})
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		if err := s.FinishRun(ctx, r.ID, now, "succeeded", "", 0); err != nil {
			t.Fatalf("FinishRun: %v", err)
		}
	}

	if got := attemptsLeft(t, s, j.ID); got != 0 {
		t.Errorf("the job has %d attempts left, want none: a job cannot owe attempts", got)
	}
}

func TestInterruptRunsInProgressSpendsAnAttempt(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 3)
	other := jobWithAttempts(t, s, "b", 3)
	now := time.Now().UTC()
	if _, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: now}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	if _, err := s.InterruptRunsInProgress(ctx, now, "interrupted", "the daemon stopped"); err != nil {
		t.Fatalf("InterruptRunsInProgress: %v", err)
	}

	if got := attemptsLeft(t, s, j.ID); got != 2 {
		t.Errorf("the job whose run was interrupted has %d attempts left, want two", got)
	}
	if got := attemptsLeft(t, s, other.ID); got != 3 {
		t.Errorf("a job that never ran has %d attempts left, want the three it was given", got)
	}
}

func TestReturnJobToQueueLeavesAJobWithAttemptsPending(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 2)
	if err := s.SetJobState(ctx, j.ID, "active"); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}

	state, err := s.ReturnJobToQueue(ctx, j.ID, "pending", "exhausted")

	if err != nil {
		t.Fatalf("ReturnJobToQueue: %v", err)
	}
	if state != "pending" {
		t.Errorf("a job with attempts left came back as %q, want pending", state)
	}
	got, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.State != "pending" || got.Position != 1 {
		t.Errorf("the job is %q at position %d, want pending at the place it kept", got.State, got.Position)
	}
}

func TestReturnJobToQueueExhaustsAJobWithNothingLeft(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := jobWithAttempts(t, s, "a", 0)
	if err := s.SetJobState(ctx, j.ID, "active"); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}

	state, err := s.ReturnJobToQueue(ctx, j.ID, "pending", "exhausted")

	if err != nil {
		t.Fatalf("ReturnJobToQueue: %v", err)
	}
	if state != "exhausted" {
		t.Errorf("a job with no attempts left came back as %q, want exhausted", state)
	}
	got, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.State != "exhausted" {
		t.Errorf("the job is %q, want exhausted", got.State)
	}
	if got.Position != 1 {
		t.Errorf("the exhausted job is at position %d, want the place it kept", got.Position)
	}
}

func TestReturnJobToQueueReportsAJobThatIsNotThere(t *testing.T) {
	_, err := jobStore(t).ReturnJobToQueue(context.Background(), 999, "pending", "exhausted")

	if !errors.Is(err, store.ErrJobNotFound) {
		t.Errorf("ReturnJobToQueue on an unknown job = %v, want ErrJobNotFound", err)
	}
}

func TestJobsAndRunsInProgressReadsBothTogether(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	running := queuedJob(t, s, "running", "a")
	idle := queuedJob(t, s, "idle", "b")
	r, err := s.StartRun(ctx, store.Run{JobID: running.ID, Started: time.Now().UTC()})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	ended, err := s.StartRun(ctx, store.Run{JobID: idle.ID, Started: time.Now().UTC()})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.FinishRun(ctx, ended.ID, time.Now().UTC(), "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	jobs, runs, err := s.JobsAndRunsInProgress(ctx)

	if err != nil {
		t.Fatalf("JobsAndRunsInProgress: %v", err)
	}
	if len(jobs) != 2 {
		t.Errorf("jobs = %+v, want both", jobs)
	}
	if len(runs) != 1 || runs[0].ID != r.ID {
		t.Errorf("runs = %+v, want only the run that has not ended", runs)
	}
}

func TestLatestRunIsTheMostRecentOneWhateverTheClockSays(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	// Times are stored as text, and RFC 3339 trims the trailing zeros of a
	// fraction, so text order is not time order. The later Run here is the one
	// whose time sorts first as text, which is what an ordering by time would
	// get wrong.
	// The later Run's time sorts first as text: ".55Z" is less than ".5Z",
	// because '5' comes before 'Z'.
	first, err := s.StartRun(ctx, store.Run{
		JobID: j.ID, Started: time.Date(2026, 9, 11, 10, 0, 0, 500000000, time.UTC),
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.FinishRun(ctx, first.ID,
		time.Date(2026, 9, 11, 10, 0, 0, 500000000, time.UTC), "failed", "", 1); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	second, err := s.StartRun(ctx, store.Run{
		JobID: j.ID, Started: time.Date(2026, 9, 11, 10, 0, 0, 550000000, time.UTC),
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.FinishRun(ctx, second.ID,
		time.Date(2026, 9, 11, 10, 0, 0, 550000000, time.UTC), "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	got, ok, err := s.LatestRun(ctx, j.ID)

	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if !ok || got.ID != second.ID {
		t.Errorf("LatestRun = %+v (found %v), want the run that started later", got, ok)
	}
	if got.Outcome != "succeeded" {
		t.Errorf("outcome = %q, want the later run's", got.Outcome)
	}
}

func TestLatestRunReportsAJobThatHasNeverRun(t *testing.T) {
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")

	_, ok, err := s.LatestRun(context.Background(), j.ID)

	if err != nil {
		t.Fatalf("LatestRun: %v", err)
	}
	if ok {
		t.Error("LatestRun found a run for a job that has never run")
	}
}

func TestSetRunSkillsRecordsWhatARunReadWith(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC()})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	skills := []store.RunSkill{
		{Name: "go-review", Source: "x/go-review", Ref: "v1.0.0", Commit: "abc", Digest: "sha256:1"},
		{Name: "house-style", Source: "me/house-style", Ref: "main", Commit: "def", Digest: "sha256:2"},
	}

	if err := s.SetRunSkills(ctx, r.ID, skills); err != nil {
		t.Fatalf("SetRunSkills: %v", err)
	}

	got, err := s.ListRunSkills(ctx, r.ID)
	if err != nil {
		t.Fatalf("ListRunSkills: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListRunSkills = %+v, want both", got)
	}
	if got[0] != skills[0] || got[1] != skills[1] {
		t.Errorf("ListRunSkills = %+v, want %+v", got, skills)
	}
}

func TestSetRunSkillsReplacesWhatWasRecorded(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := queuedJob(t, s, "work", "a")
	r, err := s.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC()})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := s.SetRunSkills(ctx, r.ID, []store.RunSkill{
		{Name: "go-review", Source: "x", Ref: "main", Commit: "abc", Digest: "sha256:1"},
	}); err != nil {
		t.Fatalf("SetRunSkills: %v", err)
	}

	if err := s.SetRunSkills(ctx, r.ID, []store.RunSkill{
		{Name: "house-style", Source: "y", Ref: "main", Commit: "def", Digest: "sha256:2"},
	}); err != nil {
		t.Fatalf("SetRunSkills again: %v", err)
	}

	got, err := s.ListRunSkills(ctx, r.ID)
	if err != nil {
		t.Fatalf("ListRunSkills: %v", err)
	}
	if len(got) != 1 || got[0].Name != "house-style" {
		t.Errorf("ListRunSkills = %+v, want only what was recorded second", got)
	}
}

func TestListRunSkillsForARunWithNoneIsEmpty(t *testing.T) {
	got, err := jobStore(t).ListRunSkills(context.Background(), 999)

	if err != nil {
		t.Fatalf("ListRunSkills: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListRunSkills = %+v, want nothing", got)
	}
}
