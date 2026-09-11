package run_test

import (
	"context"
	"errors"
	"strings"
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

// recorded waits until what the daemon reports about the machine is what the
// test is waiting for, so an assertion reads a reading rather than a race: the
// detector is called one statement before the reading is written down.
func recorded(t *testing.T, svc *run.Service, what string, is func(run.Machine) bool) run.Machine {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := svc.MachineState(); is(got) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the daemon never reported %s; it reports %+v", what, svc.MachineState())
	return run.Machine{}
}

// watched starts Watch over that detector and stops it when the test ends.
func watched(t *testing.T, svc *run.Service, d idle.Detector) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Watch(ctx, d, 10*time.Millisecond)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
}

func TestWatchReportsTheMachineAsItReadsIt(t *testing.T) {
	svc, _, _ := newFixture(t, &fakeDriver{}, &fakeExecutor{})
	// Eleven minutes on AC power: Idle under the policy nothing has changed.
	machine := &readings{state: idle.State{Since: 11 * time.Minute, OnPower: true}}

	watched(t, svc, machine)

	got := recorded(t, svc, "an idle machine", func(m run.Machine) bool { return m.Read })
	if !got.Idle {
		t.Errorf("the machine is reported as %+v, want one Owl may work on", got)
	}
	if got.Since != 11*time.Minute || !got.OnPower {
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

	inUse := recorded(t, svc, "a machine in use", func(m run.Machine) bool { return m.Read })
	if inUse.Idle || inUse.Detail == "" {
		t.Errorf("a machine in use is reported as %+v, want it said why", inUse)
	}

	// Idle for an hour, and still not one Owl may work on: the other half of
	// the policy refuses it.
	machine.says(idle.State{Since: time.Hour}, nil)

	got := recorded(t, svc, "a machine on battery",
		func(m run.Machine) bool { return m.Read && m.Since == time.Hour })
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

	got := recorded(t, svc, "a machine it could not read",
		func(m run.Machine) bool { return strings.Contains(m.Detail, "said no") })
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
