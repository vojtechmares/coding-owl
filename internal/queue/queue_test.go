package queue_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// fixed is a Source whose reference never changes.
type fixed struct{ ref string }

func (fixed) Name() string           { return "test" }
func (f fixed) Ref() (string, error) { return f.ref, nil }

// service returns a queue Service over a temporary database holding Projects
// at the given directories, keyed by name.
func service(t *testing.T, src queue.Source, paths map[string]string) *queue.Service {
	t.Helper()
	st, _, err := store.Open(filepath.Join(t.TempDir(), "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for name, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := st.AddProject(context.Background(), store.Project{
			Name: name, Path: path, BaseBranch: "main", Registered: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("AddProject: %v", err)
		}
	}
	return queue.NewService(st, src)
}

// oneProject returns a Service with a Project named api, and its directory.
func oneProject(t *testing.T) (*queue.Service, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "api")
	return service(t, queue.Local{}, map[string]string{"api": dir}), dir
}

func TestAddResolvesTheProjectFromASubdirectory(t *testing.T) {
	svc, dir := oneProject(t)
	sub := filepath.Join(dir, "internal", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	j, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: sub})

	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if j.Project != "api" {
		t.Errorf("project = %q, want api", j.Project)
	}
	if j.State != queue.StatePending || j.Position != 1 {
		t.Errorf("job is %s at position %d, want pending at 1", j.State, j.Position)
	}
}

func TestAddPrefersTheInnermostProject(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "outer")
	inner := filepath.Join(outer, "inner")
	svc := service(t, queue.Local{}, map[string]string{"outer": outer, "inner": inner})

	j, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: inner})

	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if j.Project != "inner" {
		t.Errorf("project = %q, want inner", j.Project)
	}
}

func TestAddOutsideEveryProjectNamesTheFlag(t *testing.T) {
	svc, _ := oneProject(t)
	dir := t.TempDir()

	_, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: dir})

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Add = %v, want an InvalidError", err)
	}
	if !strings.Contains(err.Error(), "--project") || !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the directory and the --project flag", err)
	}
}

func TestAddRefusesAnEmptyPrompt(t *testing.T) {
	svc, dir := oneProject(t)

	_, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "  ", WorkingDir: dir})

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Add = %v, want an InvalidError", err)
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Errorf("error %q does not mention the prompt", err)
	}
}

func TestAddRefusesAnUnknownProject(t *testing.T) {
	svc, dir := oneProject(t)

	_, err := svc.Add(context.Background(), queue.AddRequest{Project: "ghost", Prompt: "work", WorkingDir: dir})

	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Add = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error %q does not name the project", err)
	}
}

func TestLocalSourceMintsAULIDPerJob(t *testing.T) {
	svc, dir := oneProject(t)
	ctx := context.Background()

	first, err := svc.Add(ctx, queue.AddRequest{Prompt: "first", WorkingDir: dir})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := svc.Add(ctx, queue.AddRequest{Prompt: "second", WorkingDir: dir})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if first.Source != "local" || second.Source != "local" {
		t.Errorf("sources are %q and %q, want local", first.Source, second.Source)
	}
	if len(first.SourceRef) != 26 || first.SourceRef == second.SourceRef {
		t.Errorf("references %q and %q are not two distinct ulids", first.SourceRef, second.SourceRef)
	}
}

// threePending queues first, second and third in one Project.
func threePending(t *testing.T) (*queue.Service, []queue.Job) {
	t.Helper()
	svc, dir := oneProject(t)
	var jobs []queue.Job
	for _, prompt := range []string{"first", "second", "third"} {
		j, err := svc.Add(context.Background(), queue.AddRequest{Prompt: prompt, WorkingDir: dir})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		jobs = append(jobs, j)
	}
	return svc, jobs
}

// order returns the prompts in the queue, in order.
func order(t *testing.T, svc *queue.Service) string {
	t.Helper()
	jobs, err := svc.List(context.Background(), false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var prompts []string
	for _, j := range jobs {
		prompts = append(prompts, j.Prompt)
	}
	return strings.Join(prompts, ",")
}

func TestAddProducingTheSameReferenceTwiceKeepsOneJob(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "api")
	svc := service(t, fixed{ref: "issue:1"}, map[string]string{"api": dir})

	first, err := svc.Add(ctx, queue.AddRequest{Prompt: "first", WorkingDir: dir})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	second, err := svc.Add(ctx, queue.AddRequest{Prompt: "second", WorkingDir: dir})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if second.ID != first.ID || second.Position != first.Position {
		t.Errorf("second production made %+v, want the first job back: %+v", second, first)
	}
	if second.Prompt != "second" {
		t.Errorf("prompt = %q, want the second production's", second.Prompt)
	}
	if second.Source != "test" || second.SourceRef != "issue:1" {
		t.Errorf("job carries source %q ref %q, want test / issue:1", second.Source, second.SourceRef)
	}
	jobs, err := svc.List(ctx, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("the queue holds %d jobs, want 1: %+v", len(jobs), jobs)
	}
}

func TestCancelTakesAJobOutOfTheQueue(t *testing.T) {
	ctx := context.Background()
	svc, jobs := threePending(t)

	cancelled, err := svc.Cancel(ctx, jobs[0].ID)

	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if cancelled.State != queue.StateCancelled || cancelled.Position != 0 {
		t.Errorf("cancelled job is %+v, want cancelled with no position", cancelled)
	}
	if got := order(t, svc); got != "second,third" {
		t.Errorf("queue = %s, want second,third", got)
	}
}

func TestCancelRefusesAJobThatIsNotPending(t *testing.T) {
	ctx := context.Background()
	svc, jobs := threePending(t)
	if _, err := svc.Cancel(ctx, jobs[0].ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	_, err := svc.Cancel(ctx, jobs[0].ID)

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Cancel = %v, want an InvalidError", err)
	}
	if !strings.Contains(err.Error(), "not pending") || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("error %q does not say the job is not pending and what it is instead", err)
	}
}

func TestCancelReportsAnUnknownJob(t *testing.T) {
	svc, _ := threePending(t)

	_, err := svc.Cancel(context.Background(), 999)

	if !errors.Is(err, store.ErrJobNotFound) {
		t.Errorf("Cancel(999) = %v, want ErrJobNotFound", err)
	}
}

func TestReorderMovesAJobToAPosition(t *testing.T) {
	ctx := context.Background()
	svc, jobs := threePending(t)

	moved, err := svc.Reorder(ctx, jobs[2].ID, 1)

	if err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	if moved.Position != 1 {
		t.Errorf("moved job is at position %d, want 1", moved.Position)
	}
	if got := order(t, svc); got != "third,first,second" {
		t.Errorf("queue = %s, want third,first,second", got)
	}
}

func TestReorderRefusesAPositionTheQueueDoesNotHave(t *testing.T) {
	ctx := context.Background()
	svc, jobs := threePending(t)

	for _, position := range []int{0, 4} {
		_, err := svc.Reorder(ctx, jobs[0].ID, position)

		var invalid *queue.InvalidError
		if !errors.As(err, &invalid) {
			t.Fatalf("Reorder to %d = %v, want an InvalidError", position, err)
		}
		if !strings.Contains(err.Error(), "between 1 and 3") {
			t.Errorf("error %q does not name the range of positions", err)
		}
		if got := order(t, svc); got != "first,second,third" {
			t.Errorf("queue = %s after a refused reorder, want it unchanged", got)
		}
	}
}

func TestReorderRefusesAJobThatIsNotPending(t *testing.T) {
	ctx := context.Background()
	svc, jobs := threePending(t)
	if _, err := svc.Cancel(ctx, jobs[0].ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	_, err := svc.Reorder(ctx, jobs[0].ID, 1)

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Reorder = %v, want an InvalidError", err)
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Errorf("error %q does not say the job is not pending", err)
	}
}

func TestAddGrantsTheDefaultAttemptsWhenTheRequestAsksForNone(t *testing.T) {
	svc, dir := oneProject(t)

	j, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: dir})

	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if j.TTL != queue.DefaultTTL {
		t.Errorf("a job is queued with %d attempts, want the default %d", j.TTL, queue.DefaultTTL)
	}
}

func TestAddTakesTheAttemptsItIsAskedFor(t *testing.T) {
	svc, dir := oneProject(t)

	j, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: dir, TTL: 3})

	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if j.TTL != 3 {
		t.Errorf("a job queued with three attempts has %d", j.TTL)
	}
}

func TestAddRefusesAttemptsThatAreNotANumberOfRuns(t *testing.T) {
	svc, dir := oneProject(t)

	_, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: dir, TTL: -1})

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Add with -1 attempts = %v, want an InvalidError", err)
	}
	if !strings.Contains(err.Error(), "-1") {
		t.Errorf("the error %q does not name what was asked for", err)
	}
}

func TestExtendAddsTheDefaultAttemptsWhenNoNumberIsAskedFor(t *testing.T) {
	svc, dir := oneProject(t)
	queued, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: dir, TTL: 1})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	j, err := svc.Extend(context.Background(), queued.ID, 0)

	if err != nil {
		t.Fatalf("Extend: %v", err)
	}
	if j.TTL != 1+queue.DefaultTTL {
		t.Errorf("the job has %d attempts, want the default %d added to the one it had", j.TTL, queue.DefaultTTL)
	}
}

func TestExtendRefusesANumberThatIsNotAttemptsToAdd(t *testing.T) {
	svc, dir := oneProject(t)
	queued, err := svc.Add(context.Background(), queue.AddRequest{Prompt: "work", WorkingDir: dir, TTL: 4})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, err = svc.Extend(context.Background(), queued.ID, -3)

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("Extend by -3 = %v, want an InvalidError", err)
	}
	if j, err := svc.Extend(context.Background(), queued.ID, 0); err != nil {
		t.Fatalf("Extend: %v", err)
	} else if j.TTL != 4+queue.DefaultTTL {
		t.Errorf("the job has %d attempts, want the refused extension to have changed nothing", j.TTL)
	}
}

func TestExtendReportsAJobThatIsNotThere(t *testing.T) {
	svc, _ := oneProject(t)

	_, err := svc.Extend(context.Background(), 999, 10)

	if !errors.Is(err, store.ErrJobNotFound) {
		t.Errorf("Extend on an unknown job = %v, want ErrJobNotFound", err)
	}
}
