package run_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/idle"
	"github.com/vojtechmares/coding-owl/internal/run"
)

// readings is a Detector that answers from a list the test writes, and says
// how many times it has been asked.
type readings struct {
	mu    sync.Mutex
	state idle.State
	err   error
	reads int
}

func (r *readings) Name() string { return "the test's" }

func (r *readings) Read(context.Context) (idle.State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	return r.state, r.err
}

// says makes the machine report that from the next reading on.
func (r *readings) says(state idle.State, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state, r.err, r.reads = state, err, 0
}

// looked waits until the machine has been read again, so a test asserts on a
// reading rather than on a race.
func (r *readings) looked(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		read := r.reads
		r.mu.Unlock()
		if read > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the machine was never read")
}

// watched starts Watch over that detector and stops it when the test ends.
func watched(t *testing.T, svc *run.Service, d idle.Detector) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Watch(ctx, d, idle.Policy{After: time.Minute, RequirePower: true}, 10*time.Millisecond)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
}

func TestWatchReportsTheMachineAsItReadsIt(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	machine := &readings{state: idle.State{Since: 2 * time.Minute, OnPower: true}}

	watched(t, svc, machine)
	machine.looked(t)

	got := svc.MachineState()
	if !got.Read || !got.Idle {
		t.Errorf("the machine is reported as %+v, want one Owl may work on", got)
	}
	if got.Since != 2*time.Minute || !got.OnPower {
		t.Errorf("the machine is reported as %+v, want what the detector said", got)
	}
	if got.Detail != "" {
		t.Errorf("an idle machine is reported with the reason %q", got.Detail)
	}
}

func TestWatchReportsWhyAMachineIsNotOneOwlMayWorkOn(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	machine := &readings{state: idle.State{Since: time.Second, OnPower: true}}
	watched(t, svc, machine)
	machine.looked(t)

	if got := svc.MachineState(); !got.Read || got.Idle || got.Detail == "" {
		t.Errorf("a machine in use is reported as %+v, want it said why", got)
	}

	machine.says(idle.State{Since: time.Hour}, nil)
	machine.looked(t)

	got := svc.MachineState()
	if got.Idle {
		t.Errorf("a machine on battery is reported as %+v, want work held back", got)
	}
	if got.Detail == "" {
		t.Error("a machine on battery is reported without saying so")
	}
}

func TestWatchReportsAMachineItCannotReadAsUnread(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	machine := &readings{err: errors.New("the tool said no")}

	watched(t, svc, machine)
	machine.looked(t)

	got := svc.MachineState()
	// Not knowing must never read as a machine Owl may work on.
	if got.Read || got.Idle {
		t.Errorf("a machine that could not be read is reported as %+v", got)
	}
	if got.Detail == "" {
		t.Error("a machine that could not be read is reported without saying why")
	}
}

func TestTheMachineIsUnreadUntilItHasBeenLookedAt(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})

	got := svc.MachineState()

	if got.Read || got.Idle {
		t.Errorf("a machine nobody has looked at is reported as %+v", got)
	}
	if got.Detail == "" {
		t.Error("a machine nobody has looked at is reported without saying so")
	}
}
