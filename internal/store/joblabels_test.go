package store_test

// The labels a Job carries (issue #132). The fixtures come from
// jobs_test.go, which is where a Job in this package's shape is built.

import (
	"context"
	"slices"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// labelled is job with labels on it, for the scenarios about them.
func labelled(prompt, ref string, labels ...string) store.Job {
	j := job(prompt, ref)
	j.Labels = labels
	return j
}

// mustUpsert queues j and fails the test rather than returning an error, so
// that a label scenario reads as what it is about.
func mustUpsert(t *testing.T, s *store.Store, j store.Job) store.Job {
	t.Helper()
	out, err := s.UpsertJob(context.Background(), j)
	if err != nil {
		t.Fatalf("UpsertJob %q: %v", j.Prompt, err)
	}
	return out
}

// labelsOf is the labels a Job carries, read back the way every caller reads
// them: off the Job.
func labelsOf(t *testing.T, s *store.Store, id int64) []string {
	t.Helper()
	j, err := s.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("GetJob %d: %v", id, err)
	}
	return j.Labels
}

// promptsOf renders the prompts of some Jobs, which is how a filtered listing
// says which ones it matched.
func promptsOf(jobs []store.Job) []string {
	out := make([]string, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.Prompt)
	}
	return out
}

func TestUpsertJobCarriesItsLabelsBack(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)

	j := mustUpsert(t, s, labelled("first", "a", "urgent", "bug"))

	// Back in label order rather than the order they were given, so that two
	// Jobs carrying the same labels read the same wherever they are printed.
	if !slices.Equal(j.Labels, []string{"bug", "urgent"}) {
		t.Errorf("UpsertJob returned labels %v, want [bug urgent]", j.Labels)
	}
	got, err := s.GetJob(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if !slices.Equal(got.Labels, []string{"bug", "urgent"}) {
		t.Errorf("GetJob returned labels %v, want [bug urgent]", got.Labels)
	}
}

func TestAddJobLabelsKeepsALabelOnce(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := mustUpsert(t, s, labelled("first", "a", "bug"))

	if err := s.AddJobLabels(ctx, j.ID, []string{"bug", "urgent"}); err != nil {
		t.Fatalf("AddJobLabels: %v", err)
	}

	if labels := labelsOf(t, s, j.ID); !slices.Equal(labels, []string{"bug", "urgent"}) {
		t.Errorf("labels = %v, want [bug urgent]", labels)
	}
}

func TestRemoveJobLabelsReportsWhatItRemoved(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := mustUpsert(t, s, labelled("first", "a", "bug", "urgent"))

	missing, err := s.RemoveJobLabels(ctx, j.ID, []string{"urgent", "never-there"})
	if err != nil {
		t.Fatalf("RemoveJobLabels: %v", err)
	}

	if !slices.Equal(missing, []string{"never-there"}) {
		t.Errorf("RemoveJobLabels reported %v missing, want [never-there]", missing)
	}
	// Nothing at all was removed, urgent included: a caller that refuses the
	// request must not find the labels that were there already gone.
	if labels := labelsOf(t, s, j.ID); !slices.Equal(labels, []string{"bug", "urgent"}) {
		t.Errorf("labels = %v, want [bug urgent] left exactly as they were", labels)
	}
}

func TestRemoveJobLabelsTakesThemOffWhenTheJobCarriesThemAll(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := mustUpsert(t, s, labelled("first", "a", "bug", "urgent"))

	missing, err := s.RemoveJobLabels(ctx, j.ID, []string{"urgent"})
	if err != nil {
		t.Fatalf("RemoveJobLabels: %v", err)
	}

	if len(missing) != 0 {
		t.Errorf("RemoveJobLabels reported %v missing, want none", missing)
	}
	if labels := labelsOf(t, s, j.ID); !slices.Equal(labels, []string{"bug"}) {
		t.Errorf("labels = %v, want [bug]", labels)
	}
}

func TestListQueueNarrowsToJobsCarryingEveryLabel(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	mustUpsert(t, s, labelled("both", "a", "bug", "urgent"))
	mustUpsert(t, s, labelled("one", "b", "bug"))
	mustUpsert(t, s, labelled("other", "c", "chore"))
	mustUpsert(t, s, job("none", "d"))

	one, err := s.ListQueue(ctx, "pending", "bug")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	both, err := s.ListQueue(ctx, "pending", "bug", "urgent")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	unfiltered, err := s.ListQueue(ctx, "pending")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if got := promptsOf(one); !slices.Equal(got, []string{"both", "one"}) {
		t.Errorf("one label matched %v, want [both one]", got)
	}
	if got := promptsOf(both); !slices.Equal(got, []string{"both"}) {
		t.Errorf("two labels matched %v, want [both]", got)
	}
	if got := promptsOf(unfiltered); len(got) != 4 {
		t.Errorf("no label matched %v, want all four", got)
	}
}

func TestRepeatingALabelDoesNotNarrowTwice(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	mustUpsert(t, s, labelled("one", "a", "bug"))

	// Counting the label twice would ask for two matches where the Job can
	// only ever give one, and nothing would be found.
	got, err := s.ListQueue(ctx, "pending", "bug", "bug")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if want := []string{"one"}; !slices.Equal(promptsOf(got), want) {
		t.Errorf("the same label twice matched %v, want %v", promptsOf(got), want)
	}
}

func TestListAllJobsNarrowsByLabelWhateverTheState(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	mustUpsert(t, s, labelled("kept", "a", "bug"))
	gone := mustUpsert(t, s, labelled("cancelled", "b", "bug"))
	mustUpsert(t, s, labelled("other", "c", "chore"))
	if err := s.DequeueJob(ctx, gone.ID, "pending", "cancelled", ""); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}

	all, err := s.ListAllJobs(ctx, "bug")
	if err != nil {
		t.Fatalf("ListAllJobs: %v", err)
	}
	queued, err := s.ListQueue(ctx, "pending", "bug")
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}

	if got := promptsOf(all); !slices.Equal(got, []string{"kept", "cancelled"}) {
		t.Errorf("every job carrying bug = %v, want [kept cancelled]", got)
	}
	if got := promptsOf(queued); !slices.Equal(got, []string{"kept"}) {
		t.Errorf("the queued jobs carrying bug = %v, want [kept]", got)
	}
}

func TestReProducingAJobThatHasLeftTheQueueAddsNoLabels(t *testing.T) {
	ctx := context.Background()
	s := jobStore(t)
	j := mustUpsert(t, s, labelled("first", "a", "bug"))
	if err := s.DequeueJob(ctx, j.ID, "pending", "cancelled", ""); err != nil {
		t.Fatalf("DequeueJob: %v", err)
	}

	// The same rule the prompt follows: a Job that has left the queue is left
	// exactly as it is, however often its Source produces it again.
	again := mustUpsert(t, s, labelled("first", "a", "urgent"))

	if !slices.Equal(again.Labels, []string{"bug"}) {
		t.Errorf("labels = %v, want [bug] untouched", again.Labels)
	}
	if labels := labelsOf(t, s, j.ID); !slices.Equal(labels, []string{"bug"}) {
		t.Errorf("labels = %v, want [bug] untouched", labels)
	}
}
