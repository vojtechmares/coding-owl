package behavior_test

// Behavior tests for issue #132. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-132.md. They drive the built owl binary from the
// outside against a daemon started as a subprocess, because a label is only
// worth having if it survives the whole round trip: the CLI, the daemon, and
// the database it is read back out of.

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// noLabels is what owl jobs show prints for a Job carrying none, and
// noLabelsCell is the same thing in the LABELS column of owl queue list. They
// differ because the listing is a table: an empty cell would run its
// neighbours together.
const (
	noLabels     = "(none)"
	noLabelsCell = "-"
)

// addLabelled queues a Job in r with those labels and returns its id. The
// labels go on one --label each, which is the form the sheet fixes.
func addLabelled(t *testing.T, l *layout, dir, prompt string, labels ...string) string {
	t.Helper()
	var flags []string
	for _, lab := range labels {
		flags = append(flags, "--label", lab)
	}
	addJob(t, l, dir, prompt, flags...)
	return jobID(t, queueList(t, l), prompt)
}

// labelsOf is the labels owl jobs show reports for a Job, as it printed them:
// the whole line, so that a scenario can say what it expects to read rather
// than what a parser made of it.
func labelsOf(t *testing.T, l *layout, id string) string {
	t.Helper()
	return line(t, mustOwl(t, l, "jobs", "show", id).stdout, "labels")
}

// labelCell is the LABELS column of the row owl queue list printed for that
// prompt.
func labelCell(t *testing.T, rows []queueRow, prompt string) string {
	t.Helper()
	for _, r := range rows {
		if r.prompt == prompt {
			return r.labels
		}
	}
	t.Fatalf("no row with prompt %q in %v", prompt, rows)
	return ""
}

// prompts are the prompts of some rows, in the order they were printed, which
// is how a filtered listing says which Jobs it matched.
func prompts(rows []queueRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.prompt)
	}
	return out
}

// wantFiltered fails unless owl queue list under those flags matched exactly
// these prompts, in order.
func wantFiltered(t *testing.T, l *layout, flags []string, want ...string) {
	t.Helper()
	got := prompts(queueList(t, l, flags...))
	if !slices.Equal(got, want) {
		t.Errorf("owl queue list %v matched %v, want %v", flags, got, want)
	}
}

func TestS1JobQueuedWithLabelsCarriesThem(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")

	id := addLabelled(t, l, r.dir, "fix the flaky test", "bug", "urgent")

	shown := labelsOf(t, l, id)
	for _, want := range []string{"bug", "urgent"} {
		if !strings.Contains(shown, want) {
			t.Errorf("owl jobs show labels = %q, does not name %q", shown, want)
		}
	}
	if cell := labelCell(t, queueList(t, l), "fix the flaky test"); !strings.Contains(cell, "bug") || !strings.Contains(cell, "urgent") {
		t.Errorf("queue list LABELS = %q, does not name both bug and urgent", cell)
	}
}

func TestS2JobQueuedWithoutLabelsCarriesNone(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")

	id := addLabelled(t, l, r.dir, "fix the flaky test")

	if got := labelsOf(t, l, id); got != noLabels {
		t.Errorf("owl jobs show labels = %q, want %q", got, noLabels)
	}
	if got := labelCell(t, queueList(t, l), "fix the flaky test"); got != noLabelsCell {
		t.Errorf("queue list LABELS = %q, want %q", got, noLabelsCell)
	}
}

func TestS3LabelAddedToAQueuedJob(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	id := addLabelled(t, l, r.dir, "work")

	res := mustOwl(t, l, "jobs", "label", "add", id, "bug")

	if !strings.Contains(res.stdout, id) || !strings.Contains(res.stdout, "bug") {
		t.Errorf("owl jobs label add does not name the job and its labels:\n%s", res.stdout)
	}
	if got := labelsOf(t, l, id); !strings.Contains(got, "bug") {
		t.Errorf("owl jobs show labels = %q, does not name bug", got)
	}
}

func TestS4LabelRemovedFromAJob(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	id := addLabelled(t, l, r.dir, "work", "bug", "urgent")

	mustOwl(t, l, "jobs", "label", "remove", id, "urgent")

	got := labelsOf(t, l, id)
	if !strings.Contains(got, "bug") {
		t.Errorf("owl jobs show labels = %q, does not name bug", got)
	}
	if strings.Contains(got, "urgent") {
		t.Errorf("owl jobs show labels = %q, still names urgent", got)
	}
}

func TestS5AddingALabelAlreadyCarriedChangesNothing(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	id := addLabelled(t, l, r.dir, "work", "bug")

	mustOwl(t, l, "jobs", "label", "add", id, "bug")

	if got := labelsOf(t, l, id); strings.Count(got, "bug") != 1 {
		t.Errorf("owl jobs show labels = %q, want bug exactly once", got)
	}
}

func TestS6RemovingALabelNotCarriedIsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	id := addLabelled(t, l, r.dir, "work", "bug")

	res := runOwl(t, l, "jobs", "label", "remove", id, "urgent")

	if res.code == 0 {
		t.Fatalf("owl jobs label remove of a label the job does not carry exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "urgent") {
		t.Errorf("stderr does not name urgent:\n%s", res.stderr)
	}
	if got := labelsOf(t, l, id); !strings.Contains(got, "bug") {
		t.Errorf("owl jobs show labels = %q, no longer names bug", got)
	}
}

func TestS7EmptyAndWhitespaceLabelsAreRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")

	blank := runOwlIn(t, l, r.dir, "add", "work", "--label", "   ")

	if blank.code == 0 {
		t.Fatalf("owl add --label \"   \" exited 0\nstdout:\n%s", blank.stdout)
	}
	if !strings.Contains(blank.stderr, "empty") {
		t.Errorf("stderr does not say a label may not be empty:\n%s", blank.stderr)
	}
	wantEmptyQueue(t, l)

	spaced := runOwlIn(t, l, r.dir, "add", "work", "--label", "two words")

	if spaced.code == 0 {
		t.Fatalf("owl add --label \"two words\" exited 0\nstdout:\n%s", spaced.stdout)
	}
	if !strings.Contains(spaced.stderr, "whitespace") {
		t.Errorf("stderr does not say a label may not hold whitespace:\n%s", spaced.stderr)
	}
	wantEmptyQueue(t, l)

	id := addLabelled(t, l, r.dir, "queued")
	if res := runOwl(t, l, "jobs", "label", "add", id, "two words"); res.code == 0 {
		t.Errorf("owl jobs label add of a label holding whitespace exited 0\nstdout:\n%s", res.stdout)
	} else if !strings.Contains(res.stderr, "whitespace") {
		t.Errorf("stderr does not say a label may not hold whitespace:\n%s", res.stderr)
	}

	trimmed := addLabelled(t, l, r.dir, "trimmed", "  bug  ")
	if got := labelsOf(t, l, trimmed); got != "bug" {
		t.Errorf("owl jobs show labels = %q, want %q", got, "bug")
	}
}

func TestS8LabellingAJobThatDoesNotExistIsRefused(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	project(t, l, "api")
	const missing = "999"

	for _, verb := range []string{"add", "remove"} {
		res := runOwl(t, l, "jobs", "label", verb, missing, "bug")
		if res.code == 0 {
			t.Errorf("owl jobs label %s %s exited 0\nstdout:\n%s", verb, missing, res.stdout)
			continue
		}
		if !strings.Contains(res.stderr, missing) {
			t.Errorf("owl jobs label %s: stderr does not name the id %s:\n%s", verb, missing, res.stderr)
		}
	}
}

func TestS9QueueListLabelShowsOnlyThatLabel(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addLabelled(t, l, r.dir, "a bug", "bug")
	addLabelled(t, l, r.dir, "a chore", "chore")
	addLabelled(t, l, r.dir, "neither")

	wantFiltered(t, l, []string{"--label", "bug"}, "a bug")
}

func TestS10RepeatingLabelNarrowsToAllOfThem(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addLabelled(t, l, r.dir, "both", "bug", "urgent")
	addLabelled(t, l, r.dir, "one", "bug")

	wantFiltered(t, l, []string{"--label", "bug", "--label", "urgent"}, "both")
}

func TestS11AFilterNothingMatchesSaysTheQueueIsEmpty(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addLabelled(t, l, r.dir, "a bug", "bug")

	res := mustOwl(t, l, "queue", "list", "--label", "nope")

	if !strings.Contains(res.stdout, emptyQueue) {
		t.Errorf("a filter matching nothing does not say the queue is empty:\n%s", res.stdout)
	}
	if rows := queueList(t, l, "--label", "nope"); len(rows) != 0 {
		t.Errorf("a filter matching nothing printed %d rows:\n%s", len(rows), res.stdout)
	}
}

func TestS12TheFilterAppliesToAllAsWell(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addLabelled(t, l, r.dir, "kept", "bug")
	cancelled := addLabelled(t, l, r.dir, "cancelled", "bug")
	mustOwl(t, l, "queue", "remove", cancelled)

	wantFiltered(t, l, []string{"--all", "--label", "bug"}, "kept", "cancelled")

	for _, row := range queueList(t, l, "--all", "--label", "bug") {
		if row.prompt == "cancelled" && row.position != noLabelsCell {
			t.Errorf("the cancelled job has position %q, want %q", row.position, noLabelsCell)
		}
	}
	wantFiltered(t, l, []string{"--label", "bug"}, "kept")
}

func TestS13LabelsSurviveADaemonRestart(t *testing.T) {
	l := newLayout(t)
	p := daemonUp(t, l)
	r := project(t, l, "api")
	id := addLabelled(t, l, r.dir, "work", "bug")

	stopDaemon(t, p)
	daemonUp(t, l)

	if got := labelsOf(t, l, id); !strings.Contains(got, "bug") {
		t.Errorf("owl jobs show labels = %q after a restart, does not name bug", got)
	}
	wantFiltered(t, l, []string{"--label", "bug"}, "work")
}

func TestS14LabelsBelongToOneJob(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	first := addLabelled(t, l, r.dir, "first", "bug")
	second := addLabelled(t, l, r.dir, "second", "bug")

	mustOwl(t, l, "jobs", "label", "remove", first, "bug")

	if got := labelsOf(t, l, second); !strings.Contains(got, "bug") {
		t.Errorf("owl jobs show labels of the other job = %q, does not name bug", got)
	}
	wantFiltered(t, l, []string{"--label", "bug"}, "second")
}

func TestS15TheListingKeepsTheColumnsItHad(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := project(t, l, "api")
	addLabelled(t, l, r.dir, "labelled", "bug")
	addLabelled(t, l, r.dir, "bare")

	res := mustOwl(t, l, "queue", "list")

	header := strings.Fields(strings.SplitN(res.stdout, "\n", 2)[0])
	want := []string{"POSITION", "ID", "PROJECT", "STATE", "LABELS", "PROMPT"}
	if !slices.Equal(header, want) {
		t.Fatalf("owl queue list header = %v, want %v", header, want)
	}
	rows := queueList(t, l)
	if len(rows) != 2 {
		t.Fatalf("owl queue list printed %d rows, want 2:\n%s", len(rows), res.stdout)
	}
	for i, want := range []queueRow{
		{position: "1", project: "api", state: "pending", labels: "bug", prompt: "labelled"},
		{position: "2", project: "api", state: "pending", labels: noLabelsCell, prompt: "bare"},
	} {
		got := rows[i]
		if got.position != want.position || got.project != want.project ||
			got.state != want.state || got.labels != want.labels || got.prompt != want.prompt {
			t.Errorf("row %d = %+v, want %+v (ignoring the id)", i+1, got, want)
		}
		if _, err := strconv.ParseInt(got.id, 10, 64); err != nil {
			t.Errorf("row %d has id %q, which is not a job id", i+1, got.id)
		}
	}
}
