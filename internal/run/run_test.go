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
	"github.com/vojtechmares/coding-owl/internal/verifier"
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

// fakeVerifier answers with what a scenario scripted rather than running
// anything.
type fakeVerifier struct {
	results []verifier.Result
	err     error
	mu      sync.Mutex
	asked   verifier.Request
}

func (*fakeVerifier) Name() string { return "fake" }

func (v *fakeVerifier) Verify(_ context.Context, req verifier.Request) ([]verifier.Result, error) {
	v.mu.Lock()
	v.asked = req
	v.mu.Unlock()
	return v.results, v.err
}

func (v *fakeVerifier) request() verifier.Request {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.asked
}

// newFixture registers one Project and returns a Service driving fakes.
func newFixture(t *testing.T, d driver.Driver, e *fakeExecutor) (*run.Service, *store.Store, string) {
	t.Helper()
	svc, st, repo, _ := newVerifiedFixture(t, d, e, &fakeVerifier{})
	return svc, st, repo
}

// newVerifiedFixture is newFixture with a Verifier of the caller's choosing
// and the Project's configuration written on its base branch.
func newVerifiedFixture(t *testing.T, d driver.Driver, e *fakeExecutor, v verifier.Verifier, config ...string) (*run.Service, *store.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gitInit(t, repo)
	for _, body := range config {
		commitFile(t, repo, ".coding-owl.yaml", body)
	}

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
		Verifier:    v,
		WorktreeDir: filepath.Join(root, "worktrees"),
		LogDir:      filepath.Join(root, "logs"),
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc, st, repo, root
}

// commitFile writes a file in a repository and commits it.
func commitFile(t *testing.T, dir, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "--", path)
	gitIn(t, dir, "commit", "-m", "configure owl")
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "--", "README.md")
	gitIn(t, dir, "commit", "-m", "initial commit")
}

// gitIn runs git in a repository with an identity of its own, since the test
// environment deliberately has no git configuration.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
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

func TestVerificationRefusingTheWorkBlocksTheJobAndKeepsTheRun(t *testing.T) {
	ctx := context.Background()
	v := &fakeVerifier{results: []verifier.Result{
		{Name: "build", Passed: true},
		{Name: "test", Passed: false, Reason: "exited 1", Output: "--- FAIL\n"},
	}}
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, v,
		"apiVersion: codingowl.dev/v1\nchecks:\n  - name: test\n    run: \"false\"\n")
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	blocked := awaitState(t, st, j.ID, queue.StateBlocked)
	if !strings.Contains(blocked.Reason, "test") {
		t.Errorf("reason = %q, does not name the check that refused the work", blocked.Reason)
	}
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	// The Agent did its part; Verification is what refused it.
	if len(runs) != 1 || runs[0].Outcome != string(run.OutcomeSucceeded) {
		t.Fatalf("runs = %+v, want one succeeded run", runs)
	}
	results, err := st.ListCheckResults(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("ListCheckResults: %v", err)
	}
	if len(results) != 2 || results[0].Name != "build" || results[1].Name != "test" {
		t.Fatalf("results = %+v, want what every check said, in order", results)
	}
	if results[1].Output != "--- FAIL\n" {
		t.Errorf("the failing check's output was not kept: %q", results[1].Output)
	}
	if got := v.request(); got.WorkingDir != blocked.Worktree {
		t.Errorf("verification ran in %s, want the job's worktree %s", got.WorkingDir, blocked.Worktree)
	}
}

func TestVerificationPassingLeavesTheJobInReview(t *testing.T) {
	ctx := context.Background()
	v := &fakeVerifier{results: []verifier.Result{{Name: "build", Passed: true}}}
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, v,
		"apiVersion: codingowl.dev/v1\nchecks:\n  - name: build\n    run: \"true\"\n")
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	reviewed := awaitState(t, st, j.ID, queue.StateReview)
	if reviewed.Reason != "" {
		t.Errorf("reason = %q, want none on a job nothing refused", reviewed.Reason)
	}
}

func TestSetupFailingBlocksTheJobBeforeAnyRun(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nsetup:\n  - echo cannot prepare >&2; exit 2\n")
	j := queueJob(t, st, "work")

	_, _, started, err := svc.Start(ctx)

	if started {
		t.Error("a run started although setup failed")
	}
	if err == nil || !strings.Contains(err.Error(), "cannot prepare") {
		t.Fatalf("Start = %v, want the setup command's own words", err)
	}
	blocked, getErr := st.GetJob(ctx, j.ID)
	if getErr != nil {
		t.Fatalf("GetJob: %v", getErr)
	}
	if queue.State(blocked.State) != queue.StateBlocked {
		t.Errorf("state = %q, want blocked", blocked.State)
	}
	if !strings.Contains(blocked.Reason, "exited 2") {
		t.Errorf("reason = %q, does not say what setup did", blocked.Reason)
	}
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("runs = %+v, want none: setup failed before any run", runs)
	}
}

func TestSetupRunsInTheWorktreeBeforeTheAgent(t *testing.T) {
	ctx := context.Background()
	d := &fakeDriver{}
	svc, st, _, _ := newVerifiedFixture(t, d, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nsetup:\n  - touch prepared.txt\n")
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := awaitState(t, st, j.ID, queue.StateReview)
	if _, err := os.Stat(filepath.Join(done.Worktree, "prepared.txt")); err != nil {
		t.Errorf("setup did not run in the job's worktree: %v", err)
	}
}

func TestVerificationJudgesByTheConfigurationFromBeforeTheAgentRan(t *testing.T) {
	ctx := context.Background()
	hold := make(chan struct{})
	started := make(chan struct{})
	v := &fakeVerifier{results: []verifier.Result{{Name: "guard", Passed: false, Reason: "exited 1"}}}
	svc, st, repo, _ := newVerifiedFixture(t, &fakeDriver{},
		&fakeExecutor{hold: hold, started: started}, v,
		"apiVersion: codingowl.dev/v1\nchecks:\n  - name: guard\n    run: \"false\"\n")
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started
	// While the Agent is working, the base branch loses its checks - which is
	// what an Agent can do from a worktree, since the refs are shared.
	commitFile(t, repo, ".coding-owl.yaml", "apiVersion: codingowl.dev/v1\n")
	close(hold)

	blocked := awaitState(t, st, j.ID, queue.StateBlocked)

	if !strings.Contains(blocked.Reason, "guard") {
		t.Errorf("reason = %q, want the check the job was judged by", blocked.Reason)
	}
	asked := v.request()
	if len(asked.Checks) != 1 || asked.Checks[0].Name != "guard" {
		t.Errorf("verification was asked for %+v, want the checks from before the agent ran", asked.Checks)
	}
}

func TestVerificationCutShortByTheDaemonJudgesNothing(t *testing.T) {
	ctx := context.Background()
	verifying := make(chan struct{})
	v := &blockingVerifier{entered: verifying}
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, v,
		"apiVersion: codingowl.dev/v1\nchecks:\n  - name: slow\n    run: sleep 60\n")
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-verifying

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Outcome != string(run.OutcomeInterrupted) {
		t.Fatalf("runs = %+v, want the run recorded as interrupted", runs)
	}
	results, err := st.ListCheckResults(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("ListCheckResults: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("checks = %+v, want none recorded: nothing judged this work", results)
	}
	after, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StatePending {
		t.Errorf("state = %q, want the job back in the queue", after.State)
	}
}

// blockingVerifier waits for the context it is given to be cancelled, which is
// what a real Verification does when its checks outlast the daemon.
type blockingVerifier struct {
	entered chan struct{}
	once    sync.Once
}

func (*blockingVerifier) Name() string { return "blocking" }

func (v *blockingVerifier) Verify(ctx context.Context, req verifier.Request) ([]verifier.Result, error) {
	v.once.Do(func() { close(v.entered) })
	<-ctx.Done()
	// A real Verifier reports what it managed to find out, which for a
	// cancelled Verification is that nothing could be run.
	results := make([]verifier.Result, 0, len(req.Checks))
	for _, c := range req.Checks {
		results = append(results, verifier.Result{Name: c.Name, Reason: "was stopped before it finished"})
	}
	return results, nil
}

func TestStartRefusesOnceTheDaemonsContextIsDone(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{})
	j := queueJob(t, st, "work")
	// Close cancels before it sets the flag, so this is the moment between the
	// two: a Job started here would be judged on a run that never happened.
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, _, started, err := svc.Start(ctx)

	if started || err == nil {
		t.Fatalf("Start = %v, %v, want it refused", started, err)
	}
	after, getErr := st.GetJob(ctx, j.ID)
	if getErr != nil {
		t.Fatalf("GetJob: %v", getErr)
	}
	if queue.State(after.State) != queue.StatePending {
		t.Errorf("state = %q, want the job untouched and still waiting", after.State)
	}
}

func TestSetupStoppedByTheDaemonLeavesTheJobWhereItWas(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nsetup:\n  - sleep 60\n")
	j := queueJob(t, st, "work")

	failed := make(chan error, 1)
	go func() {
		_, _, _, err := svc.Start(ctx)
		failed <- err
	}()
	// Give Start time to reach the setup command it will be stopped in.
	time.Sleep(200 * time.Millisecond)

	closed := make(chan error, 1)
	go func() { closed <- svc.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Close waited for a setup command it should have stopped")
	}

	select {
	case err := <-failed:
		if err == nil {
			t.Error("Start reported success although its setup was stopped")
		} else if !strings.Contains(err.Error(), "stopped") {
			t.Errorf("Start = %v, want it to say the setup was stopped", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Start did not return after its setup was stopped")
	}
	after, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// Nothing failed at anything: the Job is still waiting its turn.
	if queue.State(after.State) != queue.StatePending || after.Reason != "" {
		t.Errorf("job = %+v, want it pending with nothing held against it", after)
	}
}
