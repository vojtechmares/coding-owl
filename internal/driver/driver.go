// Package driver is the tool axis of ADR-0018: a Driver knows how to operate
// one coding tool - how to build its invocation, what it is capable of, and
// whether the installed version is one Owl drives. Nothing outside a Driver's
// own package names that tool's flags.
package driver

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// Model is a model as Owl names it: a vendor, then that vendor's own name for
// the model, as in `anthropic/claude-opus` (ADR-0028).
//
// The vendor is Owl's half. It says which Driver can serve the model, so a
// Project that asks for one nobody drives is refused by name rather than
// finding out when an Agent will not start.
//
// Everything after the slash is the Driver's half, passed through as it was
// written. That is what lets one spelling cover both a versionless alias, which
// follows whatever the vendor calls its latest, and a pinned model - Owl does
// not need to know which it was given.
type Model struct {
	Vendor string
	Name   string
}

// ParseModel reads a model Owl was given. Both halves are required: a bare
// name was the spelling before vendors were named, and guessing a vendor for it
// would quietly pick a Driver on the user's behalf.
func ParseModel(s string) (Model, error) {
	vendor, name, ok := strings.Cut(s, "/")
	if !ok {
		return Model{}, fmt.Errorf("model %q names no vendor; write it as vendor/model, like anthropic/claude-opus", s)
	}
	if vendor == "" || name == "" || strings.Contains(name, "/") {
		return Model{}, fmt.Errorf("model %q is not vendor/model, like anthropic/claude-opus", s)
	}
	// The name is what reaches a tool's argv as the value of an option, and a
	// dash-leading one would land there as another option instead. Refused
	// here rather than by each Driver: the vendor prefix hides it from a check
	// on the written model, which does not start with a dash at all.
	//
	// The vendor is refused on the same terms even though it is never passed
	// to a tool. It buys nothing today and costs nothing, and a check that
	// holds for one half of a name and not the other is one a later reader has
	// to work out the reason for - there isn't one.
	for _, half := range []struct{ what, value string }{{"vendor", vendor}, {"model", name}} {
		if strings.HasPrefix(half.value, "-") {
			return Model{}, fmt.Errorf("model %q is not usable: the %s %q may not start with a dash",
				s, half.what, half.value)
		}
	}
	return Model{Vendor: vendor, Name: name}, nil
}

func (m Model) String() string { return m.Vendor + "/" + m.Name }

// ModelInfo is one model a Driver serves, as `owl models` and the desktop app
// list it. A Driver declaring these is what makes the set discoverable instead
// of something a user learns from a rejected configuration file.
type ModelInfo struct {
	// Model is what to write as a phase's model.
	Model Model
	// Alias is true for a versionless name that follows whatever the vendor
	// currently calls its latest of that model, and false for one pinned to a
	// version that will not move.
	Alias bool
	// About says what the model is for, in one line.
	About string
}

// Usage is what a tool reported about the account's utilization, which is what
// a ceiling is kept against (ADR-0020). Utilization is account-wide: it counts
// what the user spent themselves as well as what Owl did.
type Usage struct {
	// Windows is every window the tool said something about, in the tool's own
	// words for them.
	Windows []UsageWindow
}

// UsageWindow is one window's figure.
type UsageWindow struct {
	// Name is the window, as the tool names it.
	Name string
	// Utilization is how much of the window is spent, as a percentage.
	Utilization float64
	// Resets is when the window starts again.
	Resets time.Time
}

// Request is what a Run asks its Agent to do.
type Request struct {
	// Prompt is what the Agent is asked to do: the user's own words, inside
	// whatever the phase of the Job builds around them (ADR-0026).
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
	// ConfigDir is the Account's own tool configuration directory, and Token
	// the credential to run on (ADR-0019). Every Run has both: which Account a
	// Job runs on is its Project's to say (ADR-0023).
	ConfigDir string
	Token     string
	// AllowedTools are the tool rules the Project grants its Agents on top of
	// the Account's own allowlist, in the tool's own rule syntax (ADR-0035).
	AllowedTools []string
}

// Driver knows one coding tool.
type Driver interface {
	// Name identifies the Driver, and is what a Run records.
	Name() string
	// Capabilities is what this tool can do.
	Capabilities() Capabilities
	// Models are the models this Driver serves, in the order a listing shows
	// them. It is what `owl models` and the desktop app print, and what a
	// phase's model is checked against before an Agent is started (ADR-0028).
	Models() []ModelInfo
	// Check reports whether the tool is installed and a version Owl supports.
	Check(ctx context.Context) error
	// Command builds the Agent for one Run.
	Command(req Request) (agent.Invocation, error)
	// SetupToken builds the command that authenticates an Account: the tool's
	// own flow, which prints a long-lived token for the user to paste
	// (ADR-0019). It runs in the Account's own configuration directory rather
	// than the user's.
	SetupToken(configDir string) (agent.Invocation, error)
	// Exec builds the tool's own CLI, run against an Account's configuration
	// directory with that Account's credential. It is how an Account is
	// configured with the commands the tool already has - its MCP servers,
	// its plugins, whatever it grows next - without Owl having to model any
	// of them (ADR-0019). The arguments are the user's and reach the tool
	// untouched.
	Exec(configDir, token string, args []string) (agent.Invocation, error)
	// InstructionsFile is the file inside an Account's configuration
	// directory that this tool reads standing instructions from, and empty
	// for a tool that reads none. The name is the tool's own, which is why it
	// lives here rather than where Accounts do (ADR-0018, ADR-0037).
	InstructionsFile() string
	// SkillsDir is where this tool reads Skills from, relative to the worktree
	// it runs in (ADR-0033). It is the tool's own convention, which is why it
	// lives behind the Driver rather than in the scheduler.
	SkillsDir() string
	// Usage reads what one line of the tool's output says about the account's
	// utilization, and says whether that line said anything at all (ADR-0020).
	// The shape of the line is the tool's, like its flags, so nothing outside
	// a Driver reads it. A Driver whose Capabilities do not claim
	// UsageReporting never finds anything.
	Usage(line string) (Usage, bool)
}
