package run_test

import (
	"context"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/store"
)

func TestShowReportsTheSystemPromptEachRunWasGiven(t *testing.T) {
	ctx := context.Background()
	d := &fakeDriver{}
	svc, st, _, _ := newVerifiedFixture(t, d, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run gofmt before committing\n")
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	awaitState(t, st, j.ID, queue.StateReview)

	details, err := svc.Show(ctx, j.ID)

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(details.Runs) != 1 {
		t.Fatalf("runs = %+v, want the job's one run", details.Runs)
	}
	given := d.given().SystemPrompt
	if given == "" || details.Runs[0].SystemPrompt != given {
		t.Errorf("the run reports the system prompt\n%s\nbut the driver was given\n%s", details.Runs[0].SystemPrompt, given)
	}
}

func TestShowKeepsWhatARunWasGivenWhenTheProjectsClausesChange(t *testing.T) {
	ctx := context.Background()
	svc, st, repo, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run gofmt before committing\n")
	j := queueJob(t, st, "work")
	if _, _, _, err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	awaitState(t, st, j.ID, queue.StateReview)
	commitFile(t, repo, ".coding-owl.yaml",
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run the linter before committing\naccount: "+testAccount+"\n")

	details, err := svc.Show(ctx, j.ID)

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	given := run.SystemPrompt([]string{"run gofmt before committing"})
	if details.Runs[0].SystemPrompt != given {
		t.Errorf("the run reports the system prompt\n%s\nwant the one it was given\n%s", details.Runs[0].SystemPrompt, given)
	}
	// The Job's own system prompt is its most recent Run's, which is what a
	// client that reads nothing else has to show (ADR-0017).
	if details.SystemPrompt != given {
		t.Errorf("the job reports the system prompt\n%s\nwant its most recent run's\n%s", details.SystemPrompt, given)
	}
	if next := run.SystemPrompt([]string{"run the linter before committing"}); details.NextSystemPrompt != next {
		t.Errorf("the next run is reported as given\n%s\nwant what the base branch says now\n%s", details.NextSystemPrompt, next)
	}
}

func TestShowGivesAJobWithNoRunsTheSystemPromptItsNextRunWillBeGiven(t *testing.T) {
	ctx := context.Background()
	svc, st, _, _ := newVerifiedFixture(t, &fakeDriver{}, &fakeExecutor{}, &fakeVerifier{},
		"apiVersion: codingowl.dev/v1\nunattendedClauses:\n  - run gofmt before committing\n")
	j := queueJob(t, st, "work")

	details, err := svc.Show(ctx, j.ID)

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	next := run.SystemPrompt([]string{"run gofmt before committing"})
	if details.SystemPrompt != next || details.NextSystemPrompt != next {
		t.Errorf("a job with no runs reports the system prompt\n%s\nand the next run's\n%s\nwant both to be\n%s",
			details.SystemPrompt, details.NextSystemPrompt, next)
	}
}

func TestShowPutsNothingInPlaceOfAPromptThatWasNotRecorded(t *testing.T) {
	ctx := context.Background()
	svc, st, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	j := queueJob(t, st, "work")
	// A Run written the way a daemon from before prompts were recorded wrote
	// one: with no prompt at all.
	r, err := st.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC(), Phase: string(run.PhaseExecute)})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := st.FinishRun(ctx, r.ID, time.Now().UTC(), string(run.OutcomeSucceeded), "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	details, err := svc.Show(ctx, j.ID)

	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if len(details.Runs) != 1 || details.Runs[0].SystemPrompt != "" {
		t.Errorf("runs = %+v, want the one run with no system prompt", details.Runs)
	}
	if details.SystemPrompt != "" {
		t.Errorf("the job reports the system prompt\n%s\nwant none: its most recent run's was not recorded", details.SystemPrompt)
	}
	if details.NextSystemPrompt != run.Contract {
		t.Errorf("the next run is reported as given\n%s\nwant the standing contract", details.NextSystemPrompt)
	}
}
