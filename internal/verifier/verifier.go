// Package verifier is what decides a Run's work is acceptable (ADR-0013). A
// Job reaches review only if Verification passes; a failure blocks it and
// waits for the user, because there is no retry in the MVP.
package verifier

import (
	"context"

	"github.com/vojtechmares/coding-owl/internal/config"
)

// Result is what one check said. Every check runs, so a Verification produces
// one of these per check whatever the earlier ones did (ADR-0030).
type Result struct {
	// Name identifies the check.
	Name string
	// Command is what it ran, so a report can be acted on without opening the
	// configuration.
	Command string
	// Passed is whether it was satisfied.
	Passed bool
	// ExitCode is what the command exited with.
	ExitCode int
	// Output is what it printed, standard error included.
	Output string
	// Reason says why it failed, in the words the user reads. It is empty for
	// a check that passed.
	Reason string
}

// Request is one Verification.
type Request struct {
	// WorkingDir is the Job's worktree, which is where checks run.
	WorkingDir string
	// Checks are the Project's own checks, read from its base branch.
	Checks []config.Check
}

// Verifier decides whether a Run's work is acceptable.
type Verifier interface {
	// Name identifies the Verifier.
	Name() string
	// Verify runs the Verification and returns one Result per check, in the
	// order the checks were configured. The error is for a Verification that
	// could not be carried out at all.
	Verify(ctx context.Context, req Request) ([]Result, error)
}

// Failed returns the results that refused the work.
func Failed(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if !r.Passed {
			out = append(out, r)
		}
	}
	return out
}
