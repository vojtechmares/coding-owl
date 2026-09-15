package cli

import (
	"bytes"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// shownPrompts is what owl jobs show prints about the system prompts of a Job
// with these details.
func shownPrompts(d client.JobDetails) string {
	var out bytes.Buffer
	printSystemPrompts(Env{Stdout: &out}, d)
	return out.String()
}

func TestAJobWithNoRunsShowsThePromptItsNextRunWillBeGiven(t *testing.T) {
	got := shownPrompts(client.JobDetails{
		Job:              client.Job{State: "pending"},
		SystemPrompt:     "contract\n\n- clause",
		NextSystemPrompt: "contract\n\n- clause",
	})

	want := "\nthe next run will be given this system prompt:\ncontract\n\n- clause\n"
	if got != want {
		t.Errorf("owl jobs show prints\n%q\nwant\n%q", got, want)
	}
}

func TestRunsGivenTheSamePromptShareOneCopyNamingThemAll(t *testing.T) {
	got := shownPrompts(client.JobDetails{
		Job: client.Job{State: "review"},
		Runs: []client.Run{
			{ID: 7, SystemPrompt: "contract"},
			{ID: 9, SystemPrompt: "contract"},
		},
		SystemPrompt:     "contract",
		NextSystemPrompt: "contract",
	})

	want := "\nruns 7, 9 were given this system prompt:\ncontract\n"
	if got != want {
		t.Errorf("owl jobs show prints\n%q\nwant\n%q", got, want)
	}
}

func TestEachPromptIsShownOnceInTheOrderOfTheLatestRunGivenIt(t *testing.T) {
	// The first and the last Run were given one prompt and the Run between
	// them another: the prompt the latest Run was given comes last, nearest
	// what the next Run will be given.
	got := shownPrompts(client.JobDetails{
		Job: client.Job{State: "review"},
		Runs: []client.Run{
			{ID: 1, SystemPrompt: "contract\n\n- run gofmt"},
			{ID: 2, SystemPrompt: "contract\n\n- run the linter"},
			{ID: 3, SystemPrompt: "contract\n\n- run gofmt"},
		},
		SystemPrompt:     "contract\n\n- run gofmt",
		NextSystemPrompt: "contract\n\n- run vet",
	})

	want := "\nrun 2 was given this system prompt:\ncontract\n\n- run the linter\n" +
		"\nruns 1, 3 were given this system prompt:\ncontract\n\n- run gofmt\n"
	if got != want {
		t.Errorf("owl jobs show prints\n%q\nwant\n%q", got, want)
	}
}

func TestARunWhosePromptWasNotRecordedSaysSoAndShowsNoOther(t *testing.T) {
	got := shownPrompts(client.JobDetails{
		Job: client.Job{State: "review"},
		Runs: []client.Run{
			{ID: 1},
			{ID: 2},
			{ID: 3, SystemPrompt: "contract"},
		},
		SystemPrompt:     "contract",
		NextSystemPrompt: "contract",
	})

	want := "\nthe system prompt runs 1, 2 were given was not recorded\n" +
		"\nrun 3 was given this system prompt:\ncontract\n"
	if got != want {
		t.Errorf("owl jobs show prints\n%q\nwant\n%q", got, want)
	}
}

func TestAPendingJobShowsTheNextRunsPromptOnlyWhenItDiffersFromTheLatestRuns(t *testing.T) {
	given := "\nrun 4 was given this system prompt:\ncontract\n\n- run gofmt\n"
	next := "\nthe next run will be given this system prompt:\ncontract\n\n- run the linter\n"
	for _, tc := range []struct {
		name   string
		state  string
		latest string
		next   string
		want   string
	}{
		{"a change that has not reached a run yet", "pending", "contract\n\n- run gofmt", "contract\n\n- run the linter", given + next},
		{"nothing has changed", "pending", "contract\n\n- run gofmt", "contract\n\n- run gofmt", given},
		// A Job that is not waiting to run has no next Run to speak of.
		{"a job in review", "review", "contract\n\n- run gofmt", "contract\n\n- run the linter", given},
		{"a job that is blocked", "blocked", "contract\n\n- run gofmt", "contract\n\n- run the linter", given},
		// What the latest Run was given is not known, so it cannot be said
		// to be the same.
		{"a latest run with nothing recorded", "pending", "", "contract\n\n- run the linter",
			"\nthe system prompt run 4 was given was not recorded\n" + next},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := shownPrompts(client.JobDetails{
				Job:              client.Job{State: tc.state},
				Runs:             []client.Run{{ID: 4, SystemPrompt: tc.latest}},
				SystemPrompt:     tc.latest,
				NextSystemPrompt: tc.next,
			})

			if got != tc.want {
				t.Errorf("owl jobs show prints\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestAPromptIsShownAsTextAndNeverAsATerminalInstruction(t *testing.T) {
	// A Project's clauses are whatever its base branch says, escapes included.
	clause := "contract\n\n- \x1b]0;pwned\a"
	for name, d := range map[string]client.JobDetails{
		"given to a run":   {Job: client.Job{State: "review"}, Runs: []client.Run{{ID: 1, SystemPrompt: clause}}},
		"for the next run": {Job: client.Job{State: "pending"}, SystemPrompt: clause, NextSystemPrompt: clause},
	} {
		t.Run(name, func(t *testing.T) {
			got := shownPrompts(d)

			if !bytes.Contains([]byte(got), []byte(`- \x1b]0;pwned\x07`)) || bytes.ContainsRune([]byte(got), '\x1b') {
				t.Errorf("owl jobs show prints %q, want the escape made visible", got)
			}
		})
	}
}
