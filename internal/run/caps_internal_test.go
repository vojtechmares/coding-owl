package run

import (
	"context"
	"errors"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/queue"
)

// What a failure to start means to the watcher is the difference between
// looking again in a moment and waiting: a cap changes the moment a Run ends,
// and everything else here needs somebody to do something (ADR-0011).
func TestWhatTheWatcherWaitsFor(t *testing.T) {
	stopped, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []struct {
		what string
		ctx  context.Context
		err  error
		want string
	}{
		{what: "nothing went wrong", ctx: context.Background(), err: nil, want: ""},
		{
			what: "a cap that a finishing run clears",
			ctx:  context.Background(),
			err:  &CappedError{Err: errors.New("owl is at its cap of 1 run"), Clears: true},
			want: "",
		},
		{
			what: "a ceiling with hours to run",
			ctx:  context.Background(),
			err:  &CappedError{Err: errors.New("account work is at 92% of its five-hour window")},
			want: "account work is at 92% of its five-hour window",
		},
		{
			what: "a project that names no account",
			ctx:  context.Background(),
			err:  &CappedError{Err: errors.New("project api names no account to run on")},
			want: "project api names no account to run on",
		},
		{
			what: "work already in hand",
			ctx:  context.Background(),
			err:  busy("a run is already in progress"),
			want: "",
		},
		{
			what: "a daemon on its way out",
			ctx:  stopped,
			err:  errors.New("context canceled"),
			want: "",
		},
		{
			what: "a tool nobody installed",
			ctx:  context.Background(),
			err:  refused("claude 1.0 is older than owl drives"),
			want: "claude 1.0 is older than owl drives",
		},
		{
			what: "something that simply broke",
			ctx:  context.Background(),
			err:  errors.New("the database is locked"),
			want: "the database is locked",
		},
	} {
		if got := waitingFor(c.ctx, c.err); got != c.want {
			t.Errorf("with %s the watcher waits for %q, want %q", c.what, got, c.want)
		}
	}
}

// One Job waiting only for a slot is enough to keep looking, whatever else is
// in the queue: the slot opens the moment a Run ends, and a watcher that waited
// because an older Job needs a person would leave it idle for minutes.
func TestOneJobWaitingForASlotIsEnoughToKeepLooking(t *testing.T) {
	person := Skip{Job: queue.Job{ID: 1}, Reason: "project api names no account to run on"}
	slot := Skip{Job: queue.Job{ID: 2}, Reason: "owl is at its cap of 1 run", Clears: true}

	for _, c := range []struct {
		what    string
		skipped []Skip
		want    bool
	}{
		{what: "nothing was passed over", skipped: nil, want: false},
		{what: "only a job that needs a person", skipped: []Skip{person}, want: false},
		{what: "only a job waiting for a slot", skipped: []Skip{slot}, want: true},
		{what: "the one needing a person first", skipped: []Skip{person, slot}, want: true},
		{what: "the one waiting for a slot first", skipped: []Skip{slot, person}, want: true},
	} {
		if got := clearing(c.skipped); got != c.want {
			t.Errorf("with %s, looking again soon is %v, want %v", c.what, got, c.want)
		}
	}
}
