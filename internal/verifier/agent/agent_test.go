package agent_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentpkg "github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/verifier"
	"github.com/vojtechmares/coding-owl/internal/verifier/agent"
)

var ctx = context.Background()

// fakeDriver builds an invocation out of whatever it was asked for, and keeps
// the request so a test can read what the reviewer was told.
type fakeDriver struct {
	mu   sync.Mutex
	last driver.Request
	err  error
}

func (*fakeDriver) Name() string { return "fake" }

func (*fakeDriver) Capabilities() driver.Capabilities { return driver.Capabilities{} }

func (*fakeDriver) Check(context.Context) error { return nil }

func (d *fakeDriver) Command(req driver.Request) (agentpkg.Invocation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.last = req
	if d.err != nil {
		return agentpkg.Invocation{}, d.err
	}
	return agentpkg.Invocation{Path: "/usr/bin/true", Dir: req.WorkingDir}, nil
}

func (*fakeDriver) SetupToken(string) (agentpkg.Invocation, error) {
	return agentpkg.Invocation{}, nil
}

func (*fakeDriver) SkillsDir() string { return ".fake/skills" }

func (d *fakeDriver) given() driver.Request {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.last
}

// fakeExecutor stands in for a place an Agent runs. It writes what the test
// told it to write, as a reviewer leaving its verdict, and ends how the test
// said it ends.
type fakeExecutor struct {
	// verdict is written into the working directory before the Agent exits,
	// at the path the reviewer was asked to write to.
	verdict string
	// waitFor holds the Agent until the context is cancelled, which is how a
	// reviewer that will not finish is exercised.
	waitFor bool
	// exitCode is what it exits with, and startErr refuses to start it.
	exitCode int
	startErr error
	// stdout is what it prints.
	stdout string
}

func (*fakeExecutor) Name() string { return "fake" }

func (e *fakeExecutor) Start(ctx context.Context, inv agentpkg.Invocation) (agentpkg.Process, error) {
	if e.startErr != nil {
		return nil, e.startErr
	}
	if e.verdict != "" {
		path := filepath.Join(inv.Dir, agent.VerdictPath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(e.verdict), 0o644); err != nil {
			return nil, err
		}
	}
	return &fakeProcess{ctx: ctx, wait: e.waitFor, code: e.exitCode, out: e.stdout}, nil
}

type fakeProcess struct {
	ctx  context.Context
	wait bool
	code int
	out  string
}

func (p *fakeProcess) Stdout() io.Reader { return strings.NewReader(p.out) }

func (*fakeProcess) Signal(os.Signal) error { return nil }

func (p *fakeProcess) Wait() (int, error) {
	if p.wait {
		<-p.ctx.Done()
		return -1, p.ctx.Err()
	}
	return p.code, nil
}

func (*fakeProcess) Stderr() string { return "" }

// review runs one review over a temporary worktree and returns what it said.
func review(t *testing.T, d *fakeDriver, e *fakeExecutor, req verifier.Request) ([]verifier.Result, string, error) {
	t.Helper()
	worktree := t.TempDir()
	req.WorkingDir = worktree
	req.Review.Agent = true
	results, err := agent.New(d, e).Verify(ctx, req)
	return results, worktree, err
}

// only is the one Result a review produces.
func only(t *testing.T, results []verifier.Result, err error) verifier.Result {
	t.Helper()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Verify = %+v, want one result", results)
	}
	return results[0]
}

func TestVerifyReportsAPassingVerdictWithWhatTheReviewerWrote(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "verdict: pass\n\nIt does what the plan said.\n"}

	results, _, err := review(t, d, e, verifier.Request{})

	got := only(t, results, err)
	if !got.Passed {
		t.Errorf("a passing verdict was reported as %+v", got)
	}
	if !strings.Contains(got.Output, "It does what the plan said.") {
		t.Errorf("Output = %q, want what the reviewer wrote", got.Output)
	}
	if got.Reason != "" {
		t.Errorf("Reason = %q, want none for a review that passed", got.Reason)
	}
}

func TestVerifyReportsAFailingVerdictWithItsFindings(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "verdict: fail\n\n1. The error from os.Rename is dropped.\n"}

	results, _, err := review(t, d, e, verifier.Request{})
	got := only(t, results, err)

	if got.Passed {
		t.Error("a failing verdict was reported as passed")
	}
	if !strings.Contains(got.Output, "os.Rename") {
		t.Errorf("Output = %q, want the findings", got.Output)
	}
}

func TestVerifyGivesTheReviewerThePlanTheDiffAndWhereToWrite(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "verdict: pass\n"}

	_, _, err := review(t, d, e, verifier.Request{
		Plan:         "Add the file and stop.",
		Diff:         "+a line the agent added",
		DiffComplete: true,
		Agent: verifier.Agent{
			ConfigDir: "/accounts/work", Token: "sk-ant-oat01-x", Model: "opus", Effort: "high",
		},
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	given := d.given()
	for _, want := range []string{"Add the file and stop.", "+a line the agent added", agent.VerdictPath} {
		if !strings.Contains(given.Prompt, want) {
			t.Errorf("the reviewer's prompt does not carry %q:\n%s", want, given.Prompt)
		}
	}
	if given.ConfigDir != "/accounts/work" || given.Token != "sk-ant-oat01-x" {
		t.Errorf("the reviewer runs as %+v, want the account the job runs on", given)
	}
	if given.Model != "opus" || given.Effort != "high" {
		t.Errorf("the reviewer runs at %s/%s, want what the run it judges ran at", given.Model, given.Effort)
	}
	if given.SystemPrompt == "" {
		t.Error("the reviewer was given no system prompt of its own")
	}
}

func TestVerifySaysWhenTheDiffWasTooLongToCarryWhole(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "verdict: pass\n"}

	_, _, err := review(t, d, e, verifier.Request{Diff: "+a line", DiffComplete: false})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if !strings.Contains(d.given().Prompt, "git diff") {
		t.Errorf("a reviewer given half a diff is not told how to read the rest:\n%s", d.given().Prompt)
	}
}

func TestVerifyTakesTheVerdictAwayAgain(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "verdict: pass\n"}

	_, worktree, err := review(t, d, e, verifier.Request{})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	// The verdict is Owl's to read, not the Job's work: a worktree the Agent
	// left clean stays clean.
	if _, err := os.Stat(filepath.Join(worktree, agent.VerdictPath)); err == nil {
		t.Error("the verdict file was left in the worktree")
	}
}

func TestVerifyRefusesWhenTheReviewerLeftNoVerdict(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{}

	results, _, err := review(t, d, e, verifier.Request{})
	got := only(t, results, err)

	if got.Passed {
		t.Error("a review nobody answered was reported as passed")
	}
	if !strings.Contains(got.Reason, "verdict") {
		t.Errorf("Reason = %q, want it to say no verdict was left", got.Reason)
	}
}

func TestVerifyRefusesAVerdictItCannotRead(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "looks fine to me\n"}

	results, _, err := review(t, d, e, verifier.Request{})
	got := only(t, results, err)

	if got.Passed {
		t.Error("a verdict Owl could not read was reported as passed")
	}
	if !strings.Contains(got.Reason, "verdict") {
		t.Errorf("Reason = %q, want it to say the verdict could not be read", got.Reason)
	}
}

func TestVerifyStopsAReviewerThatWillNotFinish(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{waitFor: true}

	started := time.Now()
	results, _, err := review(t, d, e, verifier.Request{Review: config.Review{Timeout: 200 * time.Millisecond}})
	got := only(t, results, err)

	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("the review took %s, want the timeout to end it", took)
	}
	if got.Passed {
		t.Error("a review that was stopped was reported as passed")
	}
	if !strings.Contains(got.Reason, "stopped") {
		t.Errorf("Reason = %q, want it to say the reviewer was stopped", got.Reason)
	}
}

func TestVerifyReportsAReviewerThatCouldNotBeStarted(t *testing.T) {
	d := &fakeDriver{}
	e := &fakeExecutor{startErr: errors.New("no such tool")}

	results, _, err := review(t, d, e, verifier.Request{})
	got := only(t, results, err)

	if got.Passed {
		t.Error("a reviewer that never ran was reported as passed")
	}
	if !strings.Contains(got.Reason, "no such tool") {
		t.Errorf("Reason = %q, want it to say what went wrong", got.Reason)
	}
}

func TestVerifyIsNothingForAProjectThatAsksForNoReview(t *testing.T) {
	d, e := &fakeDriver{}, &fakeExecutor{verdict: "verdict: pass\n"}

	results, err := agent.New(d, e).Verify(ctx, verifier.Request{WorkingDir: t.TempDir()})

	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Verify = %+v, want nothing for a project that asked for no review", results)
	}
	if d.given().Prompt != "" {
		t.Error("an agent was started for a project that asked for no review")
	}
}
