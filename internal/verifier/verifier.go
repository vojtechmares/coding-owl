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
	// Verifier is which Verifier said it, so a report can tell a Project's own
	// check from the review it asked for (ADR-0013). What a review says is its
	// answer rather than evidence for a failure, and reads differently.
	Verifier string
}

// KindCommand and KindAgent are what a Result's Verifier holds: the Project's
// own checks, and the fresh Agent Session that reviews the work.
const (
	KindCommand = "command"
	KindAgent   = "agent"
)

// Request is one Verification.
type Request struct {
	// WorkingDir is the Job's worktree, which is where checks run.
	WorkingDir string
	// Checks are the Project's own checks, read from its base branch.
	Checks []config.Check
	// Review is what the Project asks of a reviewer (ADR-0013).
	Review config.Review
	// Plan is what the Job's planning Run decided, empty for a Job that was
	// not planned (ADR-0026).
	Plan string
	// Diff is what the Job's branch changed against the Project's base
	// branch, and DiffComplete is false when it was too long to carry whole.
	Diff         string
	DiffComplete bool
	// Agent is what a Verifier needs to start an Agent of its own: the
	// Account the Job runs on, and what its Runs run as (ADR-0019, ADR-0023).
	Agent Agent
}

// Agent is the Account and the settings a Verifier starts its own Agent with.
// It is what the Run it judges was given, so a reviewer is a second pair of
// eyes on the same terms rather than on cheaper ones.
type Agent struct {
	// ConfigDir is the Account's own tool configuration directory, and Token
	// the credential to run on.
	ConfigDir string
	Token     string
	// Model is what it runs as, and Effort how hard it thinks (ADR-0028).
	Model  string
	Effort string
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
