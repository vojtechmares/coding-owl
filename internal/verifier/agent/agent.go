// Package agent is the Verifier that asks a fresh Agent Session to review the
// work (ADR-0013). Fresh is the whole point: the reviewer must not carry the
// context that produced the work, so it is a new Session on the Job's own
// Driver and Account, given the plan and the diff and nothing else.
//
// It costs a second Agent invocation per Run, so a Project opts into it.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	agentpkg "github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/executor"
	"github.com/vojtechmares/coding-owl/internal/verifier"
)

// VerdictPath is where the reviewer is asked to leave its verdict, inside the
// Job's worktree. It sits beside the handoff, in the directory Owl already
// owns there (ADR-0026), and Owl takes it away again once it has read it: what
// the reviewer says is not the Job's work.
const VerdictPath = ".coding-owl/REVIEW.md"

// ResultName is what a review is called in a report, beside the Project's own
// checks.
const ResultName = "agent review"

// DefaultTimeout bounds a reviewer that the Project did not bound. Reading a
// diff is not a long job, and a reviewer nobody stops holds a Run open
// (ADR-0030).
const DefaultTimeout = 15 * time.Minute

// maxOutput is how much of what the reviewer printed is kept for the message a
// failed review is reported with. The verdict is the file; this is for when
// there is not one.
const maxOutput = 4 << 10

// maxVerdict bounds the verdict file, which is read into the daemon, the
// database and the report a user reads.
const maxVerdict = 256 << 10

// Verifier asks an Agent to review a Run's work.
type Verifier struct {
	driver   driver.Driver
	executor executor.Executor
}

// New returns the agent Verifier, which starts its reviewer on the same Driver
// and in the same place as the Run it judges.
func New(d driver.Driver, e executor.Executor) *Verifier {
	return &Verifier{driver: d, executor: e}
}

// Name identifies the Verifier.
func (*Verifier) Name() string { return verifier.KindAgent }

// Verify asks a fresh Agent Session for a verdict on the work, and reports
// what it said. A Project that did not ask for a review gets nothing at all.
//
// A review that could not be carried out is a failure rather than an error: a
// Job must not reach review because nobody was asked (ADR-0013), and the
// reason says what happened.
func (v *Verifier) Verify(ctx context.Context, req verifier.Request) ([]verifier.Result, error) {
	if !req.Review.Agent {
		return nil, nil
	}
	return []verifier.Result{v.review(ctx, req)}, nil
}

// review carries out one review.
func (v *Verifier) review(ctx context.Context, req verifier.Request) verifier.Result {
	out := verifier.Result{
		Name:     ResultName,
		Command:  "a fresh " + v.driver.Name() + " session reviewing the diff",
		ExitCode: -1,
		Verifier: verifier.KindAgent,
	}
	// The verdict is read from the worktree, so anything left there by an
	// earlier Run is not this reviewer's.
	verdictPath := filepath.Join(req.WorkingDir, filepath.FromSlash(VerdictPath))
	_ = os.Remove(verdictPath)

	timeout := req.Review.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inv, err := v.driver.Command(driver.Request{
		Prompt:       prompt(req),
		SystemPrompt: systemPrompt,
		WorkingDir:   req.WorkingDir,
		Model:        req.Agent.Model,
		Effort:       req.Agent.Effort,
		ConfigDir:    req.Agent.ConfigDir,
		Token:        req.Agent.Token,
	})
	if err != nil {
		out.Reason = fmt.Sprintf("could not be asked for: %v", err)
		return out
	}
	printed, code, runErr := v.run(ctx, inv)
	out.ExitCode = code

	verdict, findings, found, err := readVerdict(verdictPath)
	if err != nil {
		out.Reason = fmt.Sprintf("left a verdict Owl could not read: %v", err)
		return out
	}
	if !found {
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			out.Reason = fmt.Sprintf("was stopped after %s, before it had left a verdict", timeout)
		case runErr != nil:
			out.Reason = fmt.Sprintf("could not be run: %v", runErr)
		default:
			out.Reason = fmt.Sprintf("left no verdict in %s", VerdictPath)
		}
		out.Output = printed
		return out
	}
	out.Output = findings
	switch verdict {
	case verdictPass:
		out.Passed = true
	case verdictFail:
		out.Reason = "the reviewer refused the work"
	default:
		out.Reason = fmt.Sprintf("left the verdict %q, which is neither %s nor %s",
			verdict, verdictPass, verdictFail)
	}
	return out
}

// run starts the reviewer and waits for it, draining what it prints: an Agent
// nobody reads blocks on a full pipe.
func (v *Verifier) run(ctx context.Context, inv agentpkg.Invocation) (printed string, code int, err error) {
	p, err := v.executor.Start(ctx, inv)
	if err != nil {
		return "", -1, err
	}
	var b strings.Builder
	if _, copyErr := io.Copy(&b, io.LimitReader(p.Stdout(), maxOutput)); copyErr == nil {
		// Whatever is left is read and dropped, so the reviewer is never
		// waiting on a reader that stopped listening.
		_, _ = io.Copy(io.Discard, p.Stdout())
	}
	code, waitErr := p.Wait()
	if waitErr != nil {
		return b.String(), code, waitErr
	}
	if stderr := strings.TrimSpace(p.Stderr()); code != 0 && stderr != "" {
		return b.String() + "\n" + stderr, code, nil
	}
	return b.String(), code, nil
}

// verdictPass and verdictFail are what a reviewer may say.
const (
	verdictPass = "pass"
	verdictFail = "fail"
)

// verdictKey is the line Owl reads the verdict from.
const verdictKey = "verdict:"

// readVerdict reads the verdict the reviewer left and takes the file away
// again. found is false for a reviewer that left nothing.
func readVerdict(path string) (verdict, findings string, found bool, err error) {
	// The file is in a worktree an Agent writes, so it is opened without
	// following a link: a verdict Owl reads out of somebody's SSH key is not a
	// verdict, and it would land in the database and the report.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	defer func() { _ = f.Close() }()
	// It is read once and taken away: a verdict on this Run is not evidence
	// about the next one, and it is not the Job's work either.
	defer func() { _ = os.Remove(path) }()
	info, err := f.Stat()
	if err != nil {
		return "", "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", "", false, fmt.Errorf("%s is not an ordinary file", VerdictPath)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxVerdict))
	if err != nil {
		return "", "", false, err
	}
	body := string(data)
	for _, ln := range strings.Split(body, "\n") {
		rest, ok := cutFold(strings.TrimSpace(ln), verdictKey)
		if !ok {
			continue
		}
		return strings.ToLower(strings.TrimSpace(rest)), strings.TrimSpace(body), true, nil
	}
	return "", strings.TrimSpace(body), true, fmt.Errorf(
		"no %s line in %s", verdictKey, VerdictPath)
}

// cutFold is strings.CutPrefix, ignoring case: a reviewer writing `Verdict:`
// means what one writing `verdict:` means.
func cutFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
