package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// jobStore opens a database holding one Project to hang Jobs off.
func jobStore(t *testing.T) *store.Store {
	t.Helper()
	s := openStore(t, filepath.Join(t.TempDir(), "owl.db"))
	if err := s.AddProject(context.Background(), store.Project{
		Name: "api", Path: "/repos/api", BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	return s
}

func job(prompt string, ref string) store.Job {
	return store.Job{
		Source: "local", SourceRef: ref, Project: "api", Prompt: prompt,
		State: "pending", Created: time.Now().UTC(),
	}
}

func TestUpsertJobQueuesInFIFOOrder(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)

	first, err := s.UpsertJob(ctx, job("first", "a"))
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	second, err := s.UpsertJob(ctx, job("second", "b"))
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	if first.Position != 1 || second.Position != 2 {
		t.Errorf("positions are %d and %d, want 1 and 2", first.Position, second.Position)
	}
	if first.ID == second.ID {
		t.Errorf("both jobs have id %d", first.ID)
	}
	jobs, err := s.ListQueue(ctx, "pending")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 2 || jobs[0].Prompt != "first" || jobs[1].Prompt != "second" {
		t.Errorf("queue = %+v, want first then second", jobs)
	}
}

func TestUpsertJobIsIdempotentOnItsSourceReference(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)

	first, err := s.UpsertJob(ctx, job("first", "a"))
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	again, err := s.UpsertJob(ctx, job("rewritten", "a"))
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}

	if again.ID != first.ID || again.Position != first.Position {
		t.Errorf("second production made job %+v, want the first one back: %+v", again, first)
	}
	if again.Prompt != "rewritten" {
		t.Errorf("prompt = %q, want the second production's", again.Prompt)
	}
	jobs, err := s.ListAllJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("the database holds %d jobs, want 1: %+v", len(jobs), jobs)
	}
}

func TestGetJobReportsAnUnknownID(t *testing.T) {
	s := jobStore(t)

	_, err := s.GetJob(context.Background(), 999)

	if !errors.Is(err, store.ErrJobNotFound) {
		t.Errorf("GetJob(999) = %v, want ErrJobNotFound", err)
	}
}

// queued returns the prompts in the queue, in order.
func queued(t *testing.T, s *store.Store, all bool) []string {
	t.Helper()
	list := func() ([]store.Job, error) {
		if all {
			return s.ListAllJobs(context.Background())
		}
		return s.ListQueue(context.Background(), "pending")
	}
	jobs, err := list()
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	var out []string
	for _, j := range jobs {
		out = append(out, j.Prompt)
	}
	return out
}

// threeQueued fills the queue with first, second and third.
func threeQueued(t *testing.T, s *store.Store) []store.Job {
	t.Helper()
	var jobs []store.Job
	for i, prompt := range []string{"first", "second", "third"} {
		j, err := s.UpsertJob(context.Background(), job(prompt, string(rune('a'+i))))
		if err != nil {
			t.Fatalf("UpsertJob: %v", err)
		}
		jobs = append(jobs, j)
	}
	return jobs
}

func TestDequeueJobClosesTheGapBehindIt(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	jobs := threeQueued(t, s)

	if err := s.DequeueJob(ctx, jobs[0].ID, "cancelled"); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}

	if got := queued(t, s, false); strings.Join(got, ",") != "second,third" {
		t.Errorf("queue = %v, want second, third", got)
	}
	remaining, err := s.ListQueue(ctx, "pending")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if remaining[0].Position != 1 || remaining[1].Position != 2 {
		t.Errorf("positions are %d and %d, want 1 and 2", remaining[0].Position, remaining[1].Position)
	}
	gone, err := s.GetJob(ctx, jobs[0].ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gone.State != "cancelled" || gone.Position != 0 {
		t.Errorf("dequeued job is %+v, want cancelled with no position", gone)
	}
	if got := queued(t, s, true); strings.Join(got, ",") != "second,third,first" {
		t.Errorf("every job = %v, want the queue then first", got)
	}
}

func TestDequeueJobRefusesAJobThatIsNotQueued(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	jobs := threeQueued(t, s)
	if err := s.DequeueJob(ctx, jobs[0].ID, "cancelled"); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}

	err := s.DequeueJob(ctx, jobs[0].ID, "cancelled")

	if !errors.Is(err, store.ErrNotQueued) {
		t.Errorf("dequeuing twice = %v, want ErrNotQueued", err)
	}
	if err := s.DequeueJob(ctx, 999, "cancelled"); !errors.Is(err, store.ErrJobNotFound) {
		t.Errorf("dequeuing an unknown job = %v, want ErrJobNotFound", err)
	}
}

func TestMoveJobShiftsTheJobsItPasses(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	jobs := threeQueued(t, s)

	if err := s.MoveJob(ctx, jobs[2].ID, 1); err != nil {
		t.Fatalf("MoveJob: %v", err)
	}

	if got := queued(t, s, false); strings.Join(got, ",") != "third,first,second" {
		t.Errorf("queue = %v, want third, first, second", got)
	}
	if err := s.MoveJob(ctx, jobs[2].ID, 3); err != nil {
		t.Fatalf("MoveJob: %v", err)
	}
	if got := queued(t, s, false); strings.Join(got, ",") != "first,second,third" {
		t.Errorf("queue = %v, want first, second, third", got)
	}
}

func TestCountQueuedCountsOnlyTheQueue(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	jobs := threeQueued(t, s)
	if err := s.DequeueJob(ctx, jobs[1].ID, "cancelled"); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}

	n, err := s.CountQueued(ctx)

	if err != nil {
		t.Fatalf("CountQueued: %v", err)
	}
	if n != 2 {
		t.Errorf("CountQueued = %d, want 2", n)
	}
}

func TestRemoveProjectTakesItsJobsAndClosesTheGaps(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	if err := s.AddProject(ctx, store.Project{
		Name: "web", Path: "/repos/web", BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	for i, prompt := range []string{"first", "second", "third"} {
		j := job(prompt, string(rune('a'+i)))
		if prompt == "second" {
			j.Project = "web"
		}
		if _, err := s.UpsertJob(ctx, j); err != nil {
			t.Fatalf("UpsertJob: %v", err)
		}
	}

	// One of api's Jobs has already left the queue: the count is every Job
	// that went, not only the ones that were still waiting.
	left, err := s.ListQueue(ctx, "pending")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if err := s.DequeueJob(ctx, left[0].ID, "cancelled"); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}

	gone, err := s.RemoveProject(ctx, "api")
	if err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if gone != 2 {
		t.Errorf("RemoveProject reported %d jobs, want 2", gone)
	}

	jobs, err := s.ListAllJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Prompt != "second" {
		t.Fatalf("jobs = %+v, want only web's", jobs)
	}
	if jobs[0].Position != 1 {
		t.Errorf("the remaining job is at position %d, want 1", jobs[0].Position)
	}
}

func TestRenameProjectCarriesItsJobsAcross(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	threeQueued(t, s)

	if err := s.RenameProject(ctx, "api", "backend"); err != nil {
		t.Fatalf("RenameProject: %v", err)
	}

	jobs, err := s.ListAllJobs(ctx)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("the database holds %d jobs after the rename, want 3", len(jobs))
	}
	for _, j := range jobs {
		if j.Project != "backend" {
			t.Errorf("job %d is queued against %q, want backend", j.ID, j.Project)
		}
	}
}
