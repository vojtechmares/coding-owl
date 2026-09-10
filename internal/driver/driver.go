// Package driver is the tool axis of ADR-0018: a Driver knows how to operate
// one coding tool - how to build its invocation, what it is capable of, and
// whether the installed version is one Owl drives. Nothing outside a Driver's
// own package names that tool's flags.
package driver

import (
	"context"

	"github.com/vojtechmares/coding-owl/internal/agent"
)

// Capabilities is what a Driver's tool can do (ADR-0018). Owl degrades
// honestly on what is missing rather than assuming.
type Capabilities struct {
	// StreamingOutput means the tool emits structured events as it works,
	// rather than one blob at the end.
	StreamingOutput bool
	// BudgetCap means a Run's spend can be capped before it starts.
	BudgetCap bool
	// PermissionModes means the tool can be told to deny anything that would
	// prompt, which is the unattended posture (ADR-0012).
	PermissionModes bool
	// UsageReporting means the tool reports the account utilization it drew,
	// which is what a ceiling needs (ADR-0020).
	UsageReporting bool
}

// Request is what a Run asks its Agent to do.
type Request struct {
	// Prompt is the work, in the user's words.
	Prompt string
	// SystemPrompt is Owl's standing unattended contract with the Project's
	// own clauses appended (ADR-0017).
	SystemPrompt string
	// WorkingDir is the Job's worktree.
	WorkingDir string
	// BudgetUSD caps the Run's spend when it is above zero.
	BudgetUSD float64
	// Model is what the Agent runs as, and Effort how hard it thinks
	// (ADR-0028). Empty leaves the tool's own default alone.
	Model  string
	Effort string
}

// Driver knows one coding tool.
type Driver interface {
	// Name identifies the Driver, and is what a Run records.
	Name() string
	// Capabilities is what this tool can do.
	Capabilities() Capabilities
	// Check reports whether the tool is installed and a version Owl supports.
	Check(ctx context.Context) error
	// Command builds the Agent for one Run.
	Command(req Request) (agent.Invocation, error)
}
