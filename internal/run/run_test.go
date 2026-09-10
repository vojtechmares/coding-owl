package run_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// fakeDriver is the Driver seam: it records the Request it was given and
// hands the fake Executor a script instead of an Agent.
type fakeDriver struct {
	mu       sync.Mutex
	request  driver.Request
	checkErr error
	cmdErr   error
}

func (*fakeDriver) Name() string { return "fake" }
func (*fakeDriver) Capabilities() driver.Capabilities {
	return driver.Capabilities{StreamingOutput: true}
}

func (d *fakeDriver) Check(context.Context) error { return d.checkErr }

func (d *fakeDriver) Command(req driver.Request) (agent.Invocation, error) {
	d.mu.Lock()
	d.request = req
	d.mu.Unlock()
	if d.cmdErr != nil {
		return agent.Invocation{}, d.cmdErr
	}
	return agent.Invocation{Path: "/fake/agent", Dir: req.WorkingDir}, nil
}

func (d *fakeDriver) given() driver.Request {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.request
}

// fakeExecutor starts scripted Agents without a process.
type fakeExecutor struct {
	lines    []string
	exitCode int
	// hold keeps the Agent running until it is closed or the run is cancelled.
	hold chan struct{}
	// started is closed once an Agent has been started.
	started chan struct{}
	once    sync.Once
}

func (*fakeExecutor) Name() string { return "fake" }

func (e *fakeExecutor) Start(ctx context.Context, _ agent.Invocation) (agent.Process, error) {
	r, w := io.Pipe()
	p := &fakeProcess{out: r, code: e.exitCode, done: make(chan struct{})}
	e.once.Do(func() {
		if e.started != nil {
			close(e.started)
		}
	})
	go func() {
		for _, ln := range e.lines {
			_, _ = io.WriteString(w, ln+"\n")
		}
		if e.hold != nil {
			select {
			case <-e.hold:
			case <-ctx.Done():
				p.code = 130
			}
		}
		_ = w.Close()
		close(p.done)
	}()
	return p, nil
}

type fakeProcess struct {
	out  *io.PipeReader
	code int
	done chan struct{}
}

func (p *fakeProcess) Stdout() io.Reader      { return p.out }
func (p *fakeProcess) Signal(os.Signal) error { return nil }
func (p *fakeProcess) Stderr() string         { return "" }
func (p *fakeProcess) Wait() (int, error)     { <-p.done; return p.code, nil }

// newFixture registers one Project and returns a Service driving fakes.
func newFixture(t *testing.T, d driver.Driver, e *fakeExecutor) (*run.Service, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gitInit(t, repo)

	st, _, err := store.Open(filepath.Join(root, "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	projects := project.NewService(st, filepath.Join(root, "config"))
	if _, err := projects.Add(context.Background(), project.AddRequest{Path: repo}); err != nil {
		t.Fatalf("registering the project: %v", err)
	}
	svc := run.NewService(run.Options{
		Store:       st,
		Projects:    projects,
		Driver:      d,
		Executor:    e,
		WorktreeDir: filepath.Join(root, "worktrees"),
		LogDir:      filepath.Join(root, "logs"),
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc, st, repo
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"add", "--", "README.md"},
		{"commit", "-m", "initial commit"},
	} {
		if args[0] == "add" {
			if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
			"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// queueJob puts one pending Job in the store.
func queueJob(t *testing.T, st *store.Store, prompt string) store.Job {
	t.Helper()
	j, err := st.UpsertJob(context.Background(), store.Job{
		Source: "local", SourceRef: prompt, Project: "repo", Prompt: prompt,
		State: string(queue.StatePending), Created: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	return j
}

// awaitState waits for a Job to reach a state, so a test does not race the
// Run's own goroutine.
func awaitState(t *testing.T, st *store.Store, id int64, want queue.State) store.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, err := st.GetJob(context.Background(), id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if queue.State(j.State) == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d never reached %s", id, want)
	return store.Job{}
}

func TestStartWithNothingPendingStartsNothing(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})

	_, _, started, err := svc.Start(context.Background())

	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started {
		t.Error("Start reported a run with an empty queue")
	}
}

func TestStartRunsTheJobAndLandsItInReview(t *testing.T) {
	ctx := context.Background()
	d := &fakeDriver{}
	lines := []string{`{"type":"system"}`, `{"type":"result"}`}
	svc, st, repo := newFixture(t, d, &fakeExecutor{lines: lines})
	j := queueJob(t, st, "work")

	job, r, started, err := svc.Start(ctx)

	if err != nil || !started {
		t.Fatalf("Start = %v, %v", started, err)
	}
	if job.ID != j.ID || r.Attempt != 1 {
		t.Errorf("started job %d attempt %d, want job %d attempt 1", job.ID, r.Attempt, j.ID)
	}
	done := awaitState(t, st, j.ID, queue.StateReview)
	if done.Position != 0 {
		t.Errorf("a job in review still holds position %d", done.Position)
	}
	if want := "owl/job-" + strconv.FormatInt(j.ID, 10); done.Branch != want {
		t.Errorf("branch = %q, want %q", done.Branch, want)
	}
	if _, err := os.Stat(filepath.Join(done.Worktree, "README.md")); err != nil {
		t.Errorf("the worktree does not hold the repository's files: %v", err)
	}
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Outcome != string(run.OutcomeSucceeded) {
		t.Fatalf("runs = %+v, want one succeeded run", runs)
	}
	data, err := os.ReadFile(runs[0].LogPath)
	if err != nil {
		t.Fatalf("reading the run's log: %v", err)
	}
	if got, want := string(data), strings.Join(lines, "\n")+"\n"; got != want {
		t.Errorf("log = %q, want %q", got, want)
	}
	if got := d.given(); !strings.Contains(got.Prompt, "work") || got.WorkingDir != done.Worktree {
		t.Errorf("the driver was asked for %q in %s, want the job's prompt in its worktree",
			got.Prompt, got.WorkingDir)
	}
	if !strings.Contains(d.given().SystemPrompt, "unattended") {
		t.Errorf("the driver was given no unattended contract: %q", d.given().SystemPrompt)
	}
	if repo == done.Worktree {
		t.Error("the agent was pointed at the project's own checkout")
	}
}

func TestStartBlocksTheJobWhenTheAgentFails(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{exitCode: 3})
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	awaitState(t, st, j.ID, queue.StateBlocked)
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Outcome != string(run.OutcomeFailed) {
		t.Fatalf("runs = %+v, want one failed run", runs)
	}
	if !strings.Contains(runs[0].Error, "3") {
		t.Errorf("reason = %q, does not name the exit status", runs[0].Error)
	}
}

func TestStartRefusesWhileARunIsInProgress(t *testing.T) {
	ctx := context.Background()
	hold := make(chan struct{})
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{hold: hold, started: make(chan struct{})})
	first := queueJob(t, st, "first")
	queueJob(t, st, "second")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, _, started, err := svc.Start(ctx)

	if started {
		t.Error("a second run started while one was in progress")
	}
	var refused *run.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Start = %v, want a RefusedError", err)
	}
	if !strings.Contains(err.Error(), strconv.FormatInt(first.ID, 10)) {
		t.Errorf("error %q does not name the job that is running", err)
	}
	close(hold)
	awaitState(t, st, first.ID, queue.StateReview)
}

func TestStartRefusesAToolItDoesNotDrive(t *testing.T) {
	ctx := context.Background()
	d := &fakeDriver{checkErr: errString("claude is 1.9.0, which Owl does not drive")}
	svc, st, _ := newFixture(t, d, &fakeExecutor{})
	j := queueJob(t, st, "work")

	_, _, started, err := svc.Start(ctx)

	if started {
		t.Error("a run started with a tool Owl does not drive")
	}
	if err == nil || !strings.Contains(err.Error(), "1.9.0") {
		t.Fatalf("Start = %v, want the version it refused", err)
	}
	after, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StatePending || after.Branch != "" {
		t.Errorf("job = %+v, want it untouched and still pending", after)
	}
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("runs = %+v, want none recorded", runs)
	}
}

func TestCloseInterruptsARunAndReturnsTheJobToTheQueue(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{hold: make(chan struct{}), started: started})
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Outcome != string(run.OutcomeInterrupted) {
		t.Fatalf("runs = %+v, want one interrupted run", runs)
	}
	after, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// A Run that did not finish leaves its Job waiting again, in the place it
	// kept while it ran (ADR-0011, ADR-0025).
	if queue.State(after.State) != queue.StatePending {
		t.Errorf("state = %q, want the job back in the queue", after.State)
	}
	if after.Position != 1 {
		t.Errorf("position = %d, want the place it held while it ran", after.Position)
	}
}

func TestLogFollowsARunToItsEnd(t *testing.T) {
	ctx := context.Background()
	hold := make(chan struct{})
	started := make(chan struct{})
	lines := []string{`{"type":"system"}`, `{"type":"result"}`}
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{lines: lines, hold: hold, started: started})
	j := queueJob(t, st, "work")
	_, r, _, err := svc.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	got := make(chan []string, 1)
	go func() {
		var seen []string
		_ = svc.Log(ctx, r.ID, true, func(ln run.Line) error {
			seen = append(seen, ln.Text)
			return nil
		})
		got <- seen
	}()
	time.Sleep(50 * time.Millisecond)
	close(hold)
	awaitState(t, st, j.ID, queue.StateReview)

	select {
	case seen := <-got:
		if strings.Join(seen, "|") != strings.Join(lines, "|") {
			t.Errorf("followed %v, want %v", seen, lines)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Log did not end with the run")
	}
}

func TestLogOnAFinishedRunReadsTheFile(t *testing.T) {
	ctx := context.Background()
	lines := []string{`{"type":"system"}`, `{"type":"result"}`}
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{lines: lines})
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	awaitState(t, st, j.ID, queue.StateReview)
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}

	var seen []string
	if err := svc.Log(ctx, runs[0].ID, false, func(ln run.Line) error {
		seen = append(seen, ln.Text)
		return nil
	}); err != nil {
		t.Fatalf("Log: %v", err)
	}

	if strings.Join(seen, "|") != strings.Join(lines, "|") {
		t.Errorf("log = %v, want %v", seen, lines)
	}
}

func TestShowReportsTheJobItsRunsAndTheSystemPrompt(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	d, err := svc.Show(ctx, j.ID)

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if d.Job.ID != j.ID || len(d.Runs) != 1 {
		t.Errorf("details = %+v, want the job and its one run", d)
	}
	if d.SystemPrompt != run.Contract {
		t.Errorf("system prompt = %q, want the standing contract", d.SystemPrompt)
	}
}

func TestSystemPromptAppendsTheProjectsClausesAfterTheContract(t *testing.T) {
	got := run.SystemPrompt([]string{"run gofmt before committing", "  ", ""})

	if !strings.HasPrefix(got, run.Contract) {
		t.Errorf("the contract is not first:\n%s", got)
	}
	if !strings.Contains(got, "- run gofmt before committing") {
		t.Errorf("the project's clause is missing:\n%s", got)
	}
	if strings.Contains(got, "- \n") {
		t.Errorf("an empty clause was kept:\n%s", got)
	}
	if run.SystemPrompt(nil) != run.Contract {
		t.Error("a project with no clauses changes the contract")
	}
}

// errString is an error that is only its message.
type errString string

func (e errString) Error() string { return string(e) }

func TestRecoverEndsTheRunsAnEarlierDaemonLeftOpen(t *testing.T) {
	ctx := context.Background()
	hold := make(chan struct{})
	started := make(chan struct{})
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{hold: hold, started: started})
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started
	// A killed daemon records nothing, so the row is left as it was.
	if _, ok, err := st.RunInProgress(ctx); err != nil || !ok {
		t.Fatalf("RunInProgress = %v, %v, want the run that is going", ok, err)
	}

	if err := svc.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	if _, ok, err := st.RunInProgress(ctx); err != nil || ok {
		t.Errorf("RunInProgress after Recover = %v, %v, want none", ok, err)
	}
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Outcome != string(run.OutcomeInterrupted) {
		t.Errorf("runs = %+v, want the leftover run interrupted", runs)
	}
	close(hold)
}

func TestStartRefusesOnceTheDaemonIsStopping(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	queueJob(t, st, "work")
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, _, started, err := svc.Start(ctx)

	if started {
		t.Error("a run started while the daemon was stopping")
	}
	var refused *run.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Start = %v, want a RefusedError", err)
	}
	if !strings.Contains(err.Error(), "stopping") {
		t.Errorf("error %q does not say the daemon is stopping", err)
	}
}

func TestExecutionFallsBackToThePlanWhenTheWorktreeLostItsHandoff(t *testing.T) {
	ctx := context.Background()
	d := &fakeDriver{}
	svc, st, _ := newFixture(t, d, &fakeExecutor{})
	j := queueJob(t, st, "work")
	// A Job past planning, whose worktree no longer holds the handoff the plan
	// was written to.
	if err := st.SetJobState(ctx, j.ID, string(queue.StatePending)); err != nil {
		t.Fatalf("SetJobState: %v", err)
	}
	if err := st.SetJobPlan(ctx, j.ID, "step one: read the tests"); err != nil {
		t.Fatalf("SetJobPlan: %v", err)
	}

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	if got := d.given().Prompt; !strings.Contains(got, "step one: read the tests") {
		t.Errorf("the execution prompt lost the plan when the worktree had no handoff:\n%s", got)
	}
}
