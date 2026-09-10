package run

// Model and effort cascade global to Project to Job, narrowest winning
// (ADR-0028), and each phase asks the Agent for something different
// (ADR-0026). Both are worth pinning where they are decided.

import (
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/store"
)

func TestResolveTakesTheNarrowestLevelThatSaysAnything(t *testing.T) {
	global := config.Global{Phases: map[string]config.Phase{
		"plan":    {Model: "sonnet"},
		"execute": {Effort: "low"},
	}}
	project := config.Config{Phases: map[string]config.Phase{
		"plan": {Model: "haiku"},
	}}

	plan, err := resolve(PhasePlan, global, project, store.Job{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	execute, err := resolve(PhaseExecute, global, project, store.Job{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if plan.Model != (Choice{Value: "haiku", From: FromProject}) {
		t.Errorf("plan model = %+v, want haiku from the project", plan.Model)
	}
	if plan.Effort != (Choice{Value: "xhigh", From: FromDefault}) {
		t.Errorf("plan effort = %+v, want the default", plan.Effort)
	}
	if execute.Model != (Choice{Value: "opus", From: FromDefault}) {
		t.Errorf("execute model = %+v, want the default", execute.Model)
	}
	if execute.Effort != (Choice{Value: "low", From: FromGlobal}) {
		t.Errorf("execute effort = %+v, want low from the global file", execute.Effort)
	}
}

func TestResolveLetsTheJobOverrideEverything(t *testing.T) {
	global := config.Global{Phases: map[string]config.Phase{"plan": {Model: "sonnet", Effort: "low"}}}
	project := config.Config{Phases: map[string]config.Phase{"plan": {Model: "haiku"}}}

	got, err := resolve(PhasePlan, global, project, store.Job{Model: "opus", Effort: "max"})

	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Model != (Choice{Value: "opus", From: FromJob}) || got.Effort != (Choice{Value: "max", From: FromJob}) {
		t.Errorf("settings = %+v, want both from the job", got)
	}
}

func TestResolveRefusesASettingATotalWouldReadAsAnOption(t *testing.T) {
	project := config.Config{Phases: map[string]config.Phase{"plan": {Model: "--oops"}}}

	_, err := resolve(PhasePlan, config.Global{}, project, store.Job{})

	if err == nil {
		t.Fatal("resolve with a dash-leading model = nil, want an error")
	}
	if !strings.Contains(err.Error(), "--oops") {
		t.Errorf("error %q does not name the setting it refused", err)
	}
}

func TestPhaseOfPlansOnceAndThenExecutes(t *testing.T) {
	for _, tc := range []struct {
		name string
		job  store.Job
		want Phase
	}{
		{"planned, no plan yet", store.Job{Planned: true}, PhasePlan},
		{"planned, plan in hand", store.Job{Planned: true, Plan: "step one"}, PhaseExecute},
		{"not planned", store.Job{}, PhaseExecute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := phaseOf(tc.job); got != tc.want {
				t.Errorf("phaseOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPromptsAskForOneThingEach(t *testing.T) {
	plan := planPrompt("fix the flaky test")
	if !strings.Contains(plan, "fix the flaky test") || !strings.Contains(plan, HandoffPath) {
		t.Errorf("the planning prompt does not carry the work and the handoff:\n%s", plan)
	}
	if !strings.Contains(strings.ToLower(plan), "do not carry it out") {
		t.Errorf("the planning prompt does not say to plan rather than do:\n%s", plan)
	}

	first := executePrompt("fix the flaky test", "")
	if strings.Contains(first, "Where the work stands") {
		t.Errorf("the first run is told about a handoff nobody wrote:\n%s", first)
	}
	if !strings.Contains(first, HandoffPath) {
		t.Errorf("the first run is not asked to keep a handoff:\n%s", first)
	}

	later := executePrompt("fix the flaky test", "step one: read the tests")
	for _, want := range []string{"fix the flaky test", "step one: read the tests", HandoffPath, "git log"} {
		if !strings.Contains(later, want) {
			t.Errorf("the execution prompt does not carry %q:\n%s", want, later)
		}
	}
}
