package run

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// stubProcess stands in for an Agent this daemon can reach. What these tests
// are about is who decides to signal it, not what the signal does, so every
// signal is taken and nothing is read.
type stubProcess struct{}

func (stubProcess) Stdout() io.Reader           { return strings.NewReader("") }
func (stubProcess) SignalGroup(os.Signal) error { return nil }
func (stubProcess) Wait() (int, error)          { return 0, nil }
func (stubProcess) Stderr() string              { return "" }

// interruptFixture is a Service with nothing but what pausing and resuming
// touch: the Runs this daemon can reach, and a store to read one back from.
// Only a test inside the package can put a Run in the moments these are about.
func interruptFixture(t *testing.T, st *store.Store) *Service {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Service{
		opts:   Options{Store: st, Logger: slog.New(slog.DiscardHandler)},
		now:    time.Now,
		ctx:    ctx,
		cancel: cancel,
		live:   map[int64]*live{},
		stages: map[int64]Stage{},
		ours:   map[int64]bool{},
	}
}

func emptyStore(t *testing.T) *store.Store {
	t.Helper()
	st, _, err := store.Open(filepath.Join(t.TempDir(), "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// A Run this daemon is ending is refused in the words it is being ended with.
// The grace window passing is one reason and an Account crossing its ceiling
// is another (ADR-0011, ADR-0020), and a person told the wrong one would go
// looking in the wrong place.
func TestPauseAndResumeSayWhyTheRunIsBeingEnded(t *testing.T) {
	const why = "account work is at 92% of its five-hour window, at or above the 80% ceiling"
	st := emptyStore(t)

	for _, verb := range []string{"pause", "resume"} {
		s := interruptFixture(t, st)
		s.live[1] = &live{proc: stubProcess{}, jobID: 1, endedWhy: why}

		var err error
		if verb == "pause" {
			_, _, err = s.Pause(context.Background(), ByUser)
		} else {
			_, _, err = s.Resume(context.Background(), ByUser)
		}

		if err == nil {
			t.Fatalf("owl %s reported success on a run that is being ended", verb)
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("owl %s says %q, want it to say the run is being ended because %s", verb, err, why)
		}
	}
}

// The grace window and `owl resume` race whenever the window fires on a Run
// somebody is continuing at that moment. Either outcome is fine: the window
// ends the Run and the resume is refused, or the resume continues it and the
// window leaves it alone. What must never happen is both, which would tell a
// person their Run is going again while it is being killed.
func TestTheGraceWindowDoesNotEndARunSomebodyContinuedFirst(t *testing.T) {
	st := emptyStore(t)

	for round := range 200 {
		s := interruptFixture(t, st)
		const runID = 1
		s.live[runID] = &live{proc: stubProcess{}, jobID: 1, paused: true}

		var (
			wg      sync.WaitGroup
			start   = make(chan struct{})
			resumed bool
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := s.Resume(context.Background(), ByUser)
			// A Run that is being ended is refused by name. Anything else -
			// including the store not knowing this run - means Resume got past
			// that and reported the Run as going again.
			resumed = err == nil || !strings.Contains(err.Error(), "being ended")
		}()
		go func() {
			defer wg.Done()
			<-start
			s.expire(runID)
		}()
		close(start)
		wg.Wait()

		if _, ended := s.interrupted(runID); resumed && ended {
			t.Fatalf("round %d: owl resume continued run %d and the grace window ended it anyway", round, runID)
		}
	}
}
