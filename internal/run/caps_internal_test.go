package run

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
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

// And whether it is worth raising. The watcher treats a refusal and a failure
// the same way; the log is the only place they read differently, and a log
// nobody raises is one nobody reads.
func TestWhatTheWatcherRaises(t *testing.T) {
	for _, c := range []struct {
		what string
		err  error
		want bool
	}{
		{what: "nothing went wrong", err: nil, want: false},
		{what: "a cap", err: &CappedError{Err: errors.New("owl is at its cap of 1 run")}, want: false},
		{what: "a refusal for a person", err: refused("claude is older than owl drives"), want: false},
		{what: "work already in hand", err: busy("a run is already in progress"), want: false},
		{what: "a daemon on its way out", err: context.Canceled, want: false},
		{what: "a worktree that could not be made", err: errors.New("git worktree add: no space left"), want: true},
	} {
		if got := broke(c.err); got != c.want {
			t.Errorf("with %s, raising it is %v, want %v", c.what, got, c.want)
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

// blockedStore is a Service over a real database holding one Project to hang
// Jobs off, which is all the dependency check reads: it runs before the
// Project is read, so there is no Projects service to stand up.
func blockedStore(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, _, err := store.Open(filepath.Join(t.TempDir(), "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.AddProject(context.Background(), store.Project{
		Name: "api", Path: "/repos/api", BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	// A Projects service that answers for the `done` case, which is the one
	// that falls through to it. The repository is not there, so what it
	// answers is a refusal - which is all this needs: it is not "it waits
	// for", so the dependency has stopped holding the Job.
	return NewService(Options{Store: st, Projects: project.NewService(st, t.TempDir())}), st
}

// queueOne puts a Job in that state and returns it.
func queueOne(t *testing.T, st *store.Store, ref, state string) store.Job {
	t.Helper()
	ctx := context.Background()
	j, err := st.UpsertJob(ctx, store.Job{
		Source: "test", SourceRef: ref, Project: "api", Prompt: "work",
		State: string(queue.StatePending), TTL: 3, Created: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if state != string(queue.StatePending) {
		if err := st.SetJobState(ctx, j.ID, state); err != nil {
			t.Fatalf("SetJobState: %v", err)
		}
		j.State = state
	}
	return j
}

// A Job queued behind another is passed over until that one is done, and the
// reason names the Job and the state it is in. None of these clear themselves:
// a Job reaches done only when somebody accepts it or merges its branch, so
// looking again in a moment would spend an idle machine on a wait for a person
// (ADR-0011).
func TestAJobWaitsUntilTheJobItWasQueuedBehindIsDone(t *testing.T) {
	ctx := context.Background()
	s, st := blockedStore(t)
	global, err := s.global()
	if err != nil {
		t.Fatalf("global: %v", err)
	}

	for _, c := range []struct {
		state string
		want  string
	}{
		{state: string(queue.StatePending), want: "it waits for job %d (pending)"},
		{state: string(queue.StateActive), want: "it waits for job %d (active)"},
		{state: string(queue.StateReview), want: "it waits for job %d (review)"},
		{state: string(queue.StateBlocked), want: "it waits for job %d (blocked)"},
		{state: string(queue.StateCancelled), want: "it waits for job %d (cancelled)"},
		{state: string(queue.StateExhausted), want: "it waits for job %d (exhausted)"},
		{state: string(queue.StateDone), want: ""},
	} {
		t.Run(c.state, func(t *testing.T) {
			blocker := queueOne(t, st, "blocker-"+c.state, c.state)
			waiting := store.Job{ID: blocker.ID + 1000, Project: "api", TTL: 3, BlockedBy: blocker.ID}

			why, clears, err := s.holds(ctx, waiting, flight{project: map[string]int{}, account: map[string]int{}},
				global, newLookup())

			if err != nil {
				t.Fatalf("holds: %v", err)
			}
			want := c.want
			if want != "" {
				want = fmt.Sprintf(want, blocker.ID)
			}
			if c.state == string(queue.StateDone) {
				// Nothing holds it for the dependency; what stops it now is
				// whatever comes after, which this Service cannot answer.
				if strings.HasPrefix(why, "it waits for") {
					t.Errorf("a job behind a done job is held by %q, want the dependency not to hold it", why)
				}
				return
			}
			if why != want {
				t.Errorf("holds = %q, want %q", why, want)
			}
			if clears {
				t.Error("the wait is reported as clearing itself; nothing but a person makes a job done")
			}
		})
	}
}

// Jobs are never deleted, so this should not happen - but one Job nobody can
// read is passed over rather than stopping the whole scan, the way a Project
// nobody can read is.
func TestAJobWaitingForAJobThatIsGoneIsPassedOverNotAWall(t *testing.T) {
	s, _ := blockedStore(t)
	global, err := s.global()
	if err != nil {
		t.Fatalf("global: %v", err)
	}
	waiting := store.Job{ID: 1, Project: "api", TTL: 3, BlockedBy: 999}

	why, clears, err := s.holds(context.Background(), waiting,
		flight{project: map[string]int{}, account: map[string]int{}}, global, newLookup())

	if err != nil {
		t.Fatalf("holds = %v, want the job passed over rather than the scan stopped", err)
	}
	if why != "it waits for job 999, which is gone" {
		t.Errorf("holds = %q, want it to say the job it waits for is gone", why)
	}
	if clears {
		t.Error("a job that is gone is reported as clearing itself")
	}
}

// A queue of Jobs behind one blocker reads it once: `owl status` asks for a
// scan and the desktop app asks every two seconds.
func TestOneBlockingJobIsReadOncePerScan(t *testing.T) {
	ctx := context.Background()
	s, st := blockedStore(t)
	blocker := queueOne(t, st, "blocker", string(queue.StatePending))
	look := newLookup()

	for range 3 {
		if _, err := look.job(ctx, s, blocker.ID); err != nil {
			t.Fatalf("job: %v", err)
		}
	}
	// And one that is not there is remembered as not being there, rather than
	// asked for again per Job behind it.
	for range 3 {
		if _, err := look.job(ctx, s, 999); err == nil {
			t.Fatal("reading job 999 succeeded")
		}
	}

	if len(look.blockers) != 1 || len(look.noJob) != 1 {
		t.Errorf("the lookup remembers %d jobs and %d failures, want one of each",
			len(look.blockers), len(look.noJob))
	}
}
