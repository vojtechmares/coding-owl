package run

import (
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// Phase is what a Run is carrying out. A Job is planned before it is executed,
// and the plan becomes the execution prompt and the first handoff (ADR-0026).
type Phase string

const (
	// PhasePlan works out what to do and writes it down.
	PhasePlan Phase = config.PhasePlan
	// PhaseExecute carries the plan out.
	PhaseExecute Phase = config.PhaseExecute
)

// HandoffPath is where a Job's handoff document lives on its branch. It is
// committed rather than kept in Owl's state, so that it survives a Run ending
// abruptly and the user can read and edit it (ADR-0026).
const HandoffPath = ".coding-owl/HANDOFF.md"

// Where a setting came from, which is what owl jobs show reports beside it so
// that a surprising value can be traced (ADR-0028).
const (
	FromDefault = "default"
	FromGlobal  = "global"
	FromProject = "project"
	FromJob     = "job"
)

// Choice is an effective setting and the level it came from.
type Choice struct {
	Value string
	From  string
}

// Settings are what one phase of a Job runs at.
type Settings struct {
	Phase  Phase
	Model  Choice
	Effort Choice
}

// defaults are what a phase runs at when nothing says otherwise: planning is
// short and decides everything downstream, so it thinks harder than the
// execution it decides (ADR-0028).
var defaults = map[Phase]Settings{
	PhasePlan: {
		Phase:  PhasePlan,
		Model:  Choice{Value: "opus", From: FromDefault},
		Effort: Choice{Value: "xhigh", From: FromDefault},
	},
	PhaseExecute: {
		Phase:  PhaseExecute,
		Model:  Choice{Value: "opus", From: FromDefault},
		Effort: Choice{Value: "high", From: FromDefault},
	},
}

// phases is the order owl jobs show reports them in, which is the order a Job
// passes through them.
var phases = []Phase{PhasePlan, PhaseExecute}

// resolve works out what a phase runs at, narrowest level winning: the
// defaults, then the daemon's own configuration, then the Project's, then the
// Job's own overrides (ADR-0028).
func resolve(phase Phase, global config.Global, project config.Config, job store.Job) (Settings, error) {
	s := defaults[phase]
	s.Phase = phase
	apply := func(p config.Phase, from string) {
		if p.Model != "" {
			s.Model = Choice{Value: p.Model, From: from}
		}
		if p.Effort != "" {
			s.Effort = Choice{Value: p.Effort, From: from}
		}
	}
	apply(global.Phases[string(phase)], FromGlobal)
	apply(project.Phases[string(phase)], FromProject)
	apply(config.Phase{Model: job.Model, Effort: job.Effort}, FromJob)

	// A value a tool would read as an option is refused here, before an Agent
	// is started, rather than reaching the argv. The settings are returned
	// either way, so owl jobs show can print the value that is the problem.
	for _, check := range []struct {
		what   string
		choice Choice
	}{{"model", s.Model}, {"effort", s.Effort}} {
		switch {
		case strings.TrimSpace(check.choice.Value) == "":
			return s, fmt.Errorf("the %s phase has no %s", phase, check.what)
		case strings.HasPrefix(check.choice.Value, "-"):
			return s, fmt.Errorf("the %s phase's %s %q is not usable: it may not start with a dash",
				phase, check.what, check.choice.Value)
		}
	}
	return s, nil
}

// planPrompt asks for a plan and nothing else. What it produces is all that
// reaches the execution Run, so it says so.
func planPrompt(work string) string {
	return "Plan this piece of work. Do not carry it out.\n\n" +
		work + "\n\n" +
		"Read the repository you are in to work out what needs doing, then write the plan to " +
		HandoffPath + " and commit it. That file is the whole of what the next run is given: " +
		"nothing of this session survives, and the run that carries the work out starts with no " +
		"memory of it. Say what you have decided and why, what you have ruled out, and what the " +
		"first steps are."
}

// executePrompt carries the work out, orienting from the handoff the last Run
// left rather than from a conversation (ADR-0026).
func executePrompt(work, handoff string) string {
	prompt := "Carry out this piece of work.\n\n" + work + "\n\n"
	if strings.TrimSpace(handoff) == "" {
		return prompt + "You are the first run on this branch. Keep " + HandoffPath +
			" current as you go: the next run starts with no memory of this one and reads that " +
			"file to find out where you got to."
	}
	return prompt + "Where the work stands, from " + HandoffPath + " on this branch:\n\n" +
		strings.TrimRight(handoff, "\n") + "\n\n" +
		"Read that file and the branch's git log to orient yourself - no conversation carries " +
		"over between runs - and keep " + HandoffPath + " current as you go, not at the end."
}
