package run_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/skill"
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
	// reports is what this tool says about an account's utilization, and
	// silent is a tool that does not report any at all (ADR-0020).
	reports map[string]driver.Usage
	silent  bool
}

func (*fakeDriver) Name() string { return "fake" }
func (d *fakeDriver) Capabilities() driver.Capabilities {
	return driver.Capabilities{StreamingOutput: true, UsageReporting: !d.silent}
}

// Usage is what this tool says about the account, for the lines the test gave
// it a figure for.
func (d *fakeDriver) Usage(line string) (driver.Usage, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	u, ok := d.reports[strings.TrimSpace(line)]
	return u, ok && !d.silent
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

// SkillsDir is where this fake's tool would read Skills from.
func (d *fakeDriver) SkillsDir() string { return ".fake/skills" }

// SetupToken is the tool's own token flow, which this fake has no need of
// beyond satisfying the Driver.
func (d *fakeDriver) SetupToken(configDir string) (agent.Invocation, error) {
	return agent.Invocation{Path: "/fake/agent", Args: []string{"setup-token"}, Dir: configDir}, nil
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

func (p *fakeProcess) Stdout() io.Reader           { return p.out }
func (p *fakeProcess) SignalGroup(os.Signal) error { return nil }
func (p *fakeProcess) Stderr() string              { return "" }
func (p *fakeProcess) Wait() (int, error)          { <-p.done; return p.code, nil }

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
	// Every Job runs on its Project's Account (ADR-0023), so the Project this
	// fixture registers names one, whatever else the caller configures.
	body := "apiVersion: codingowl.dev/v1\n"
	for _, extra := range config {
		body = extra
	}
	commitFile(t, repo, ".coding-owl.yaml", body+"\naccount: "+testAccount+"\n")

	st, _, err := store.Open(filepath.Join(root, "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	projects := project.NewService(st, filepath.Join(root, "config"))
	if _, err := projects.Add(context.Background(), project.AddRequest{Path: repo}); err != nil {
		t.Fatalf("registering the project: %v", err)
	}
	accounts := account.NewService(st, credential.NewFile(filepath.Join(root, "credentials.json")), root)
	if _, err := accounts.Add(context.Background(), account.AddRequest{
		Name: testAccount, Token: testToken,
	}); err != nil {
		t.Fatalf("adding the account to run on: %v", err)
	}
	svc := run.NewService(run.Options{
		Store:             st,
		Projects:          projects,
		Accounts:          accounts,
		Skills:            skill.NewService(skill.NewCache(filepath.Join(root, "skills"))),
		WorktreeConfigDir: filepath.Join(root, "worktree-config"),
		Driver:            d,
		Executor:          e,
		Verifier:          v,
		WorktreeDir:       filepath.Join(root, "worktrees"),
		LogDir:            filepath.Join(root, "logs"),
		// The daemon's own file, which a test writes when it has something to
		// say about grace windows, idleness or what an Account is held to. A
		// file that is not there is a daemon nobody configured.
		ConfigPath: filepath.Join(root, "config.yaml"),
	})
	t.Cleanup(func() { _ = svc.Close() })
	return svc, st, repo, root
}

// writeGlobal writes the daemon's own configuration file for a fixture.
func writeGlobal(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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

// testAccount is the Account the fixture's Project runs on, and testToken the
// credential it runs with.
const (
	testAccount = "test"
	testToken   = "sk-ant-oat01-fixture"
)

// queueJob puts one pending Job in the store, with the attempts a Job arrives
// from the queue with (ADR-0025).
func queueJob(t *testing.T, st *store.Store, prompt string) store.Job {
	t.Helper()
	return queueJobWithAttempts(t, st, prompt, queue.DefaultTTL)
}

// queueJobWithAttempts puts one pending Job in the store that may take n Runs.
func queueJobWithAttempts(t *testing.T, st *store.Store, prompt string, n int) store.Job {
	t.Helper()
	j, err := st.UpsertJob(context.Background(), store.Job{
		Source: "local", SourceRef: prompt, Project: "repo", Prompt: prompt,
		State: string(queue.StatePending), TTL: n, Created: time.Now().UTC(),
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

// One Run at a time is the default rather than the rule now: a cap says so,
// and a cap is what a second start is refused by (ADR-0021, superseding the
// one-Run clause of ADR-0011). Both Jobs are in one Project here, so the
// Project's cap is the narrower one and is the one a person is told about.
func TestStartRefusesWhileTheCapIsTaken(t *testing.T) {
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
		t.Error("a second run started with the cap already taken")
	}
	var capped *run.CappedError
	if !errors.As(err, &capped) {
		t.Fatalf("Start = %v, want a CappedError", err)
	}
	// Which cap, so that "I raised it and nothing changed" is answerable
	// (ADR-0021).
	for _, want := range []string{"the project repo", "1 run"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not carry %q", err, want)
		}
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

func TestARunThatSpendsTheLastAttemptLeavesTheJobExhausted(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{hold: make(chan struct{}), started: started})
	j := queueJobWithAttempts(t, st, "work", 1)
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	after, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// Nothing is wrong with the work: the Job has simply had its Runs, so it
	// waits for a decision rather than for another night (ADR-0025).
	if queue.State(after.State) != queue.StateExhausted {
		t.Errorf("state = %q, want exhausted", after.State)
	}
	if after.TTL != 0 {
		t.Errorf("the job has %d attempts left, want none", after.TTL)
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

func TestStartPassesOverAJobWithNoAttemptsLeft(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	out := queueJobWithAttempts(t, st, "out of attempts", 0)
	next := queueJob(t, st, "work")

	job, _, started, err := svc.Start(ctx)

	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !started || job.ID != next.ID {
		t.Errorf("started %v for job %d, want a run for the job behind the one with nothing left", started, job.ID)
	}
	after, err := st.GetJob(ctx, out.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateExhausted {
		t.Errorf("the job with no attempts left is %q, want exhausted", after.State)
	}
}

func TestAJobRunsOnTheAccountItRecordedRatherThanTheProjectsCurrentOne(t *testing.T) {
	ctx := context.Background()
	d := &fakeDriver{}
	svc, st, _, root := newVerifiedFixture(t, d, &fakeExecutor{}, &fakeVerifier{})
	accounts := account.NewService(st, credential.NewFile(filepath.Join(root, "credentials.json")), root)
	second, err := accounts.Add(ctx, account.AddRequest{Name: "second", Token: "sk-ant-oat01-second"})
	if err != nil {
		t.Fatalf("adding the second account: %v", err)
	}
	j := queueJob(t, st, "work")
	// The Job has already run on another Account than the one its Project's
	// configuration names.
	if err := st.SetJobAccount(ctx, j.ID, second.Name); err != nil {
		t.Fatalf("SetJobAccount: %v", err)
	}

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	// Which Account a Job drew on is stable for its whole life (ADR-0023), so
	// reconfiguring the Project moves its next Jobs, not this one.
	if got := d.given().ConfigDir; got != second.ConfigDir {
		t.Errorf("the run was made in %s, want the account the job recorded, %s", got, second.ConfigDir)
	}
	if got := d.given().Token; got != "sk-ant-oat01-second" {
		t.Errorf("the run drew on %q, want the token of the account the job recorded", got)
	}
}

func TestARunIsBlockedWhenTheProjectAsksForAVerifierNobodyCanGive(t *testing.T) {
	// A Job must not reach review because nobody was asked (ADR-0013): a
	// daemon built without the agent Verifier refuses rather than letting the
	// work through unjudged.
	ctx := context.Background()
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nverification:\n  agent: true\n")
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := awaitState(t, st, j.ID, queue.StateBlocked)

	if !strings.Contains(done.Reason, config.AgentVerifierName) {
		t.Errorf("reason = %q, want it to name the verifier", done.Reason)
	}
	// Why it could not be carried out is recorded where a user reads what
	// Verification said, beside whatever the Project's own checks said.
	results, err := st.ListCheckResults(context.Background(), lastRun(t, st, j.ID).ID)
	if err != nil {
		t.Fatalf("ListCheckResults: %v", err)
	}
	if len(results) != 1 || results[0].Passed || !strings.Contains(results[0].Reason, "no agent verifier") {
		t.Errorf("verification recorded %+v, want a verifier saying the daemon has none", results)
	}
}

// lastRun is a Job's most recent Run.
func lastRun(t *testing.T, st *store.Store, job int64) store.Run {
	t.Helper()
	runs, err := st.ListRuns(context.Background(), job)
	if err != nil || len(runs) == 0 {
		t.Fatalf("ListRuns = %+v, %v", runs, err)
	}
	return runs[len(runs)-1]
}

func TestARunPlacesTheSkillsTheProjectDeclares(t *testing.T) {
	ctx := context.Background()
	src := skillSource(t)
	svc, st, repo, root := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nskills:\n  - git: "+src+"\n")
	// A Skill runs at the commit the lockfile records, and `owl skills add`
	// writes both files for the user to commit (ADR-0033).
	commitFile(t, repo, skill.LockName, lockFor(t, src))
	j := queueJob(t, st, "work")

	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := awaitState(t, st, j.ID, queue.StateReview)

	body, err := os.ReadFile(filepath.Join(done.Worktree, ".fake", "skills", "go-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("the skill was not placed: %v", err)
	}
	if !strings.Contains(string(body), "Be kind.") {
		t.Errorf("the placed skill is %q, want what the source holds", body)
	}
	runs, err := st.ListRuns(ctx, j.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListRuns = %+v, %v", runs, err)
	}
	recorded, err := st.ListRunSkills(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("ListRunSkills: %v", err)
	}
	if len(recorded) != 1 || recorded[0].Name != "go-review" {
		t.Fatalf("the run recorded %+v, want the skill it read", recorded)
	}
	if recorded[0].Commit == "" || recorded[0].Digest == "" {
		t.Errorf("the run recorded %+v, want the commit and digest it read", recorded[0])
	}
	// The Skill is a link into the cache rather than a copy, so the same
	// content used twice is one directory on disk (ADR-0033).
	link, err := os.Readlink(filepath.Join(done.Worktree, ".fake", "skills", "go-review"))
	if err != nil {
		t.Fatalf("the skill is not a link into the cache: %v", err)
	}
	if !strings.HasPrefix(link, filepath.Join(root, "skills")) {
		t.Errorf("the skill points at %s, want somewhere in the cache", link)
	}
}

func TestARunWithASkillThatCannotBeFetchedIsRefused(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nskills:\n  - git: /nowhere/at/all\n")
	j := queueJob(t, st, "work")

	_, _, started, err := svc.Start(ctx)

	var refused *run.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Start with a skill that cannot be fetched = %v, want it refused", err)
	}
	if started {
		t.Error("a run started for a job whose skills could not be placed")
	}
	after, err := st.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// Nothing is wrong with the work, so the Job waits exactly where it was.
	if queue.State(after.State) != queue.StatePending {
		t.Errorf("state = %q, want the job left pending", after.State)
	}
}

// lockFor is the lockfile a Project commits beside a manifest declaring that
// source, as `owl skills add` would have written it.
func lockFor(t *testing.T, src string) string {
	t.Helper()
	cache := skill.NewCache(filepath.Join(t.TempDir(), "cache"))
	commit, err := cache.Resolve(src, "main")
	if err != nil {
		t.Fatalf("resolving the skill source: %v", err)
	}
	got, err := cache.Fetch("go-review", src, "main", commit, "")
	if err != nil {
		t.Fatalf("fetching the skill source: %v", err)
	}
	data, err := skill.Lock{Skills: map[string]skill.Locked{got.Name: got.Locked}}.Render()
	if err != nil {
		t.Fatalf("rendering the lockfile: %v", err)
	}
	return string(data)
}

// skillSource makes a git repository carrying a SKILL.md, and returns its path.
func skillSource(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "go-review")
	gitInit(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"),
		[]byte("---\nname: go-review\ndescription: how we review go\n---\n\nBe kind.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "--", "SKILL.md")
	gitIn(t, dir, "commit", "-m", "write the skill")
	return dir
}

// A ceiling is only as good as what it is kept against: a tool that reports no
// utilization cannot be held to one, and Owl says so rather than working on in
// the dark (ADR-0020). No behaviour scenario can reach this, because the
// daemon has one Driver and it reports usage.
func TestStartRefusesACeilingOnAToolThatReportsNoUsage(t *testing.T) {
	silent := &fakeDriver{silent: true}
	svc, st, _, root := newVerifiedFixture(t, silent, &fakeExecutor{}, &fakeVerifier{})
	writeGlobal(t, root, "apiVersion: codingowl.dev/v1\naccounts:\n  "+testAccount+
		":\n    limits:\n      fiveHourMax: 60\n")
	queueJob(t, st, "work")

	_, _, started, err := svc.Start(context.Background())

	if started {
		t.Fatal("a run started on an account held to a ceiling nothing can keep")
	}
	var refusal *run.RefusedError
	if !errors.As(err, &refusal) {
		t.Fatalf("Start = %v, want it refused", err)
	}
	for _, want := range []string{testAccount, "does not report", "ceiling"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not carry %q", err, want)
		}
	}
	// And nothing is held against the Job: it is waiting for a person to
	// change something, not failing at anything.
	after, err := st.GetJob(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StatePending || after.Reason != "" {
		t.Errorf("job = %+v, want it pending with nothing held against it", after)
	}
}

// And a tool that does report usage is held to the ceiling rather than to
// whether it reports.
func TestStartRunsUnderACeilingOnAToolThatReportsUsage(t *testing.T) {
	svc, st, _, root := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{})
	writeGlobal(t, root, "apiVersion: codingowl.dev/v1\naccounts:\n  "+testAccount+
		":\n    limits:\n      fiveHourMax: 60\n")
	queueJob(t, st, "work")

	_, _, started, err := svc.Start(context.Background())

	if err != nil || !started {
		t.Fatalf("Start = %v, %v; want the run started", started, err)
	}
}

// What a Run's stream says about the account is written down as it arrives,
// so that the next Run is held to what this one spent (ADR-0020).
func TestARunsStreamIsReadForWhatTheAccountHasUsed(t *testing.T) {
	resets := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	reporting := &fakeDriver{reports: map[string]driver.Usage{
		"the usage line": {Windows: []driver.UsageWindow{
			{Name: "five_hour", Utilization: 42, Resets: resets},
			{Name: "seven_day", Utilization: 7, Resets: resets},
			// A window Owl holds nothing to, and one that starts again next
			// year: neither is a window Owl keeps a figure about.
			{Name: "monthly", Utilization: 99, Resets: resets},
			{Name: "seven_day_later", Utilization: 99, Resets: resets.AddDate(0, 0, 60)},
		}},
	}}
	svc, st, _, _ := newVerifiedFixture(t, reporting, &fakeExecutor{
		lines: []string{`{"type":"system"}`, "the usage line", `{"type":"result"}`},
	}, &fakeVerifier{})
	j := queueJob(t, st, "work")

	if _, _, started, err := svc.Start(context.Background()); err != nil || !started {
		t.Fatalf("Start = %v, %v; want the run started", started, err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	got, err := st.ListAccountUsage(context.Background())
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAccountUsage = %+v, want the two windows Owl holds the account to", got)
	}
	if got[0].Account != testAccount || got[0].Window != "five_hour" || got[0].Utilization != 42 {
		t.Errorf("the five-hour reading is %+v, want the account at 42%%", got[0])
	}
	if !got[0].Resets.Equal(resets) {
		t.Errorf("the reading resets at %s, want %s", got[0].Resets, resets)
	}
}

// An Account keeps a figure about the windows Owl holds it to and no more: a
// tool that invents them cannot fill the table (ADR-0020).
func TestARunsStreamKeepsOnlySoManyWindows(t *testing.T) {
	resets := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	var many []driver.UsageWindow
	for at := range 12 {
		many = append(many, driver.UsageWindow{
			Name: fmt.Sprintf("seven_day_%02d", at), Utilization: float64(at), Resets: resets,
		})
	}
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{reports: map[string]driver.Usage{
		"the usage line": {Windows: many},
	}}, &fakeExecutor{
		lines: []string{`{"type":"system"}`, "the usage line", `{"type":"result"}`},
	}, &fakeVerifier{})
	j := queueJob(t, st, "work")

	if _, _, started, err := svc.Start(context.Background()); err != nil || !started {
		t.Fatalf("Start = %v, %v; want the run started", started, err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	got, err := st.ListAccountUsage(context.Background())
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	// Exactly as many as Owl keeps: more would let a tool fill the table, and
	// fewer would drop a window the tool really reports.
	if len(got) != 8 {
		t.Errorf("ListAccountUsage kept %d windows, want the eight owl keeps", len(got))
	}
}

// A figure is kept only about a window that starts again within the week or so
// Owl knows about. A tool reporting one that holds for longer is a tool making
// a mistake, and keeping it would park the Account on a figure nothing clears
// (ADR-0020).
func TestARunsStreamKeepsNoFigureAboutAWindowTooFarAhead(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{reports: map[string]driver.Usage{
		"the usage line": {Windows: []driver.UsageWindow{
			{Name: "seven_day_soon", Utilization: 10, Resets: now.Add(7 * 24 * time.Hour)},
			{Name: "seven_day_never", Utilization: 99, Resets: now.Add(9 * 24 * time.Hour)},
		}},
	}}, &fakeExecutor{
		lines: []string{`{"type":"system"}`, "the usage line", `{"type":"result"}`},
	}, &fakeVerifier{})
	j := queueJob(t, st, "work")

	if _, _, started, err := svc.Start(context.Background()); err != nil || !started {
		t.Fatalf("Start = %v, %v; want the run started", started, err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	got, err := st.ListAccountUsage(context.Background())
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 1 || got[0].Window != "seven_day_soon" {
		t.Fatalf("ListAccountUsage = %+v, want only the window that starts again within the week owl knows", got)
	}
}

// A window nobody set a ceiling for holds nothing back, however spent it is:
// capping the short window is not capping the weekly one, and a Run ended over
// a ceiling nobody set would be Owl inventing a limit (ADR-0020). No behaviour
// scenario reaches this: they set both ceilings or neither.
func TestARunIsNotEndedByAWindowNobodySetACeilingFor(t *testing.T) {
	resets := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	svc, st, _, root := newVerifiedFixture(t, &fakeDriver{reports: map[string]driver.Usage{
		"the usage line": {Windows: []driver.UsageWindow{
			{Name: "five_hour", Utilization: 10, Resets: resets},
			{Name: "seven_day", Utilization: 99, Resets: resets},
		}},
	}}, &fakeExecutor{
		lines: []string{`{"type":"system"}`, "the usage line", `{"type":"result"}`},
	}, &fakeVerifier{})
	// The short window is capped and the weekly one is not, and the stream
	// reports the weekly one as all but spent.
	writeGlobal(t, root, "apiVersion: codingowl.dev/v1\naccounts:\n  "+testAccount+
		":\n    limits:\n      fiveHourMax: 60\n")
	j := queueJob(t, st, "work")

	if _, _, started, err := svc.Start(context.Background()); err != nil || !started {
		t.Fatalf("Start = %v, %v; want the run started", started, err)
	}

	// The run is carried through rather than ended on a window Owl was told
	// nothing about.
	awaitState(t, st, j.ID, queue.StateReview)
}

// Readings about windows that are over do not stand in the way of a window a
// tool really reports: they are forgotten before what is known is counted
// against how many Owl keeps (ADR-0020).
func TestAWindowIsKeptEvenWhenTheAccountIsFullOfReadingsThatAreOver(t *testing.T) {
	ctx := context.Background()
	resets := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{reports: map[string]driver.Usage{
		"the usage line": {Windows: []driver.UsageWindow{
			{Name: "five_hour", Utilization: 42, Resets: resets},
		}},
	}}, &fakeExecutor{
		lines: []string{`{"type":"system"}`, "the usage line", `{"type":"result"}`},
	}, &fakeVerifier{})
	// As many readings as Owl keeps, every one of them about a window that has
	// already started again.
	over := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	for at := range 8 {
		if err := st.RecordAccountUsage(ctx, store.AccountUsage{
			Account: testAccount, Window: fmt.Sprintf("seven_day_%02d", at),
			Utilization: 99, Resets: over, Observed: over,
		}); err != nil {
			t.Fatalf("RecordAccountUsage: %v", err)
		}
	}
	j := queueJob(t, st, "work")

	if _, _, started, err := svc.Start(ctx); err != nil || !started {
		t.Fatalf("Start = %v, %v; want the run started", started, err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	got, err := st.ListAccountUsage(ctx)
	if err != nil {
		t.Fatalf("ListAccountUsage: %v", err)
	}
	if len(got) != 1 || got[0].Window != "five_hour" {
		t.Fatalf("ListAccountUsage = %+v, want the window this run reported and nothing that is over", got)
	}
}
