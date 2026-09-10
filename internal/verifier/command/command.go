// Package command is the Verifier that runs a Project's own checks: its tests,
// its linters, its formatters (ADR-0013). Every check runs, so a blocked Job
// reports everything that is wrong at once rather than one thing a morning
// (ADR-0030).
package command

import (
	"context"
	"fmt"
	"time"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/shell"
	"github.com/vojtechmares/coding-owl/internal/verifier"
)

// DefaultTimeout bounds a check that does not set one. A default is required
// rather than optional, because a hung test suite would otherwise pin a Run
// until something else noticed (ADR-0030).
const DefaultTimeout = 5 * time.Minute

// Verifier runs a Project's checks.
type Verifier struct {
	// timeout is what a check without one gets.
	timeout time.Duration
}

// New returns the command Verifier.
func New() *Verifier { return &Verifier{timeout: DefaultTimeout} }

// Name identifies the Verifier.
func (*Verifier) Name() string { return "command" }

// Verify runs every check, in order, and reports what each one said.
func (v *Verifier) Verify(ctx context.Context, req verifier.Request) ([]verifier.Result, error) {
	results := make([]verifier.Result, 0, len(req.Checks))
	for _, check := range req.Checks {
		results = append(results, v.run(ctx, req.WorkingDir, check))
	}
	return results, nil
}

// run carries out one check and judges it.
func (v *Verifier) run(ctx context.Context, dir string, check config.Check) verifier.Result {
	timeout := check.Timeout
	if timeout <= 0 {
		timeout = v.timeout
	}
	out := verifier.Result{Name: check.Name, Command: check.Run}
	res, err := shell.Run(ctx, dir, check.Run, timeout)
	out.ExitCode = res.ExitCode
	out.Output = res.Output
	switch {
	case res.Cancelled:
		out.Reason = "was stopped before it finished"
	case err != nil:
		out.Reason = fmt.Sprintf("could not be run: %v", err)
	case res.TimedOut:
		out.Reason = fmt.Sprintf("timed out after %s", timeout)
	case res.ExitCode != 0:
		out.Reason = fmt.Sprintf("exited %d", res.ExitCode)
	case check.Expect == config.ExpectEmptyOutput && res.Stdout != "":
		// gofmt -l exits zero and prints the files it disagrees with, which is
		// exactly the shape this expectation is for (ADR-0030).
		out.Reason = "exited 0 but printed output where none was expected"
	default:
		out.Passed = true
	}
	return out
}
