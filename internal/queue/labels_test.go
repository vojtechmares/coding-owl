package queue_test

// What the queue Service makes of a Job's labels (issue #132): which ones it
// accepts, and what it says when a change asks for something a Job cannot
// give. The fixtures come from queue_test.go.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// queued adds a Job carrying labels and fails the test if it is refused.
func queued(t *testing.T, svc *queue.Service, dir, prompt string, labels ...string) queue.Job {
	t.Helper()
	j, err := svc.Add(context.Background(), queue.AddRequest{
		Prompt: prompt, WorkingDir: dir, Labels: labels,
	})
	if err != nil {
		t.Fatalf("Add %q: %v", prompt, err)
	}
	return j
}

func TestAddTrimsLabelsAndKeepsEachOnce(t *testing.T) {
	svc, dir := oneProject(t)

	j := queued(t, svc, dir, "work", "  bug  ", "urgent", "bug")

	if want := []string{"bug", "urgent"}; !slices.Equal(j.Labels, want) {
		t.Errorf("labels = %v, want %v", j.Labels, want)
	}
}

func TestAddRefusesALabelThatIsNotOneName(t *testing.T) {
	svc, dir := oneProject(t)

	for _, c := range []struct{ label, says string }{
		{"", "empty"},
		{"   ", "empty"},
		{"two words", "whitespace"},
		{"tab\there", "whitespace"},
	} {
		_, err := svc.Add(context.Background(), queue.AddRequest{
			Prompt: "work", WorkingDir: dir, Labels: []string{c.label},
		})

		var invalid *queue.InvalidError
		if !errors.As(err, &invalid) {
			t.Errorf("Add with label %q gave %v, want an InvalidError", c.label, err)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("Add with label %q said %q, want it to say %q", c.label, err, c.says)
		}
	}
}

func TestAddLabelsIsAskingThatTheJobCarryThem(t *testing.T) {
	svc, dir := oneProject(t)
	j := queued(t, svc, dir, "work", "bug")

	// Twice, because asking for a label the Job already carries is asking for
	// something that is already true, not a failure.
	if _, err := svc.AddLabels(context.Background(), j.ID, []string{"urgent"}); err != nil {
		t.Fatalf("AddLabels: %v", err)
	}
	again, err := svc.AddLabels(context.Background(), j.ID, []string{"urgent"})
	if err != nil {
		t.Fatalf("AddLabels a second time: %v", err)
	}

	if want := []string{"bug", "urgent"}; !slices.Equal(again.Labels, want) {
		t.Errorf("labels = %v, want %v", again.Labels, want)
	}
}

func TestRemoveLabelsRefusesOneTheJobDoesNotCarry(t *testing.T) {
	svc, dir := oneProject(t)
	j := queued(t, svc, dir, "work", "bug")

	_, err := svc.RemoveLabels(context.Background(), j.ID, []string{"bug", "urgent"})

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("RemoveLabels gave %v, want an InvalidError", err)
	}
	if !strings.Contains(err.Error(), "urgent") {
		t.Errorf("RemoveLabels said %q, want it to name urgent", err)
	}
	// Nothing was written, not even the label that was there: a refusal
	// leaves the Job as the user last left it.
	after, err := svc.List(context.Background(), false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"bug"}; len(after) != 1 || !slices.Equal(after[0].Labels, want) {
		t.Errorf("labels = %v, want %v", after, want)
	}
}

func TestLabellingAJobThatIsNotThereIsRefused(t *testing.T) {
	svc, dir := oneProject(t)
	queued(t, svc, dir, "work")

	_, addErr := svc.AddLabels(context.Background(), 999, []string{"bug"})
	_, removeErr := svc.RemoveLabels(context.Background(), 999, []string{"bug"})

	for what, err := range map[string]error{"AddLabels": addErr, "RemoveLabels": removeErr} {
		if !errors.Is(err, store.ErrJobNotFound) {
			t.Errorf("%s on a job that is not there gave %v, want ErrJobNotFound", what, err)
		}
	}
}

func TestLabellingWithNoLabelIsRefused(t *testing.T) {
	svc, dir := oneProject(t)
	j := queued(t, svc, dir, "work", "bug")

	_, err := svc.AddLabels(context.Background(), j.ID, nil)

	var invalid *queue.InvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("AddLabels with no label gave %v, want an InvalidError", err)
	}
}

func TestListNarrowsToTheJobsCarryingEveryLabel(t *testing.T) {
	svc, dir := oneProject(t)
	queued(t, svc, dir, "both", "bug", "urgent")
	queued(t, svc, dir, "one", "bug")
	queued(t, svc, dir, "none")

	both, err := svc.List(context.Background(), false, "bug", "urgent")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	one, err := svc.List(context.Background(), false, "bug")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if got := listedPrompts(both); !slices.Equal(got, []string{"both"}) {
		t.Errorf("two labels matched %v, want [both]", got)
	}
	if got := listedPrompts(one); !slices.Equal(got, []string{"both", "one"}) {
		t.Errorf("one label matched %v, want [both one]", got)
	}
}

// listedPrompts is what a listing matched, by prompt.
func listedPrompts(jobs []queue.Job) []string {
	out := make([]string, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Prompt)
	}
	return out
}
