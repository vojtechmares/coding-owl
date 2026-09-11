package run

import (
	"context"
	"errors"
	"time"

	"github.com/vojtechmares/coding-owl/internal/idle"
)

// Machine is what the daemon last read about the machine it runs on, and what
// that reading meant (ADR-0011). It is what `owl status` and the app say about
// why work is or is not happening.
type Machine struct {
	// Read is whether the machine could be read at all. Everything below is
	// worth nothing when it is false.
	Read bool
	// Idle is whether the machine is one Owl may work on, under the policy.
	Idle bool
	// Since is how long it has been since any keyboard or mouse input.
	Since time.Duration
	// OnPower is whether the machine is drawing from AC power.
	OnPower bool
	// Detail is why the machine is not one Owl may work on, or why it could
	// not be read. Empty when it is Idle.
	Detail string
}

// Watch keeps the machine in view and does what it says: it starts work when
// the machine is Idle, freezes what is in flight when somebody comes back to
// it, and continues a frozen Run when they leave again (ADR-0011). It returns
// when ctx is done.
//
// Freezing and continuing happen on the change rather than on every reading:
// what freezes a Run is somebody arriving at a machine Owl had to itself, so a
// Run somebody started while sitting at the machine is left alone, as
// `owl start` means it to be.
func (s *Service) Watch(ctx context.Context, d idle.Detector, p idle.Policy, every time.Duration) {
	if every <= 0 {
		every = idle.DefaultInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	// What the last readable look said, and nothing before the first one: a
	// machine Owl has not read yet has not changed.
	var was *bool
	for {
		if state, err := d.Read(ctx); err != nil {
			// Not knowing is not knowing somebody is back, and it is not
			// permission to work either: nothing is started, nothing is
			// frozen, and what the machine last said is forgotten so that the
			// next reading is a fresh look rather than a change.
			if ctx.Err() == nil {
				s.opts.Logger.Debug("the machine could not be read", "error", err)
			}
			s.machineUnreadable(err)
			was = nil
		} else {
			isIdle, why := p.Allows(state)
			s.machineRead(state, isIdle, why)
			s.act(ctx, isIdle, was)
			was = &isIdle
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// act is what a reading means for the work: the change, and then the standing
// state. The caller holds no lock.
func (s *Service) act(ctx context.Context, isIdle bool, was *bool) {
	switch {
	case was != nil && *was && !isIdle:
		// Somebody came back to a machine Owl had to itself. The machine is
		// theirs again the moment this returns.
		s.freeze(ctx)
	case was != nil && !*was && isIdle:
		// And left again. A Run frozen for that reason carries on where it
		// was, inside its grace window.
		s.thaw(ctx)
	}
	if isIdle {
		s.begin(ctx)
	}
}

// freeze stops the Agent of the Run in flight, if there is one to stop.
func (s *Service) freeze(ctx context.Context) {
	if _, err := s.Pause(ctx, ByMachine); err != nil {
		// There is usually nothing to freeze - no Run, or one the user froze
		// themselves - which is not worth a word above debug.
		s.opts.Logger.Debug("nothing was frozen when the machine came back into use", "error", err)
	}
}

// thaw continues a Run frozen because somebody came back.
func (s *Service) thaw(ctx context.Context) {
	if _, err := s.Resume(ctx, ByMachine); err != nil {
		s.opts.Logger.Debug("nothing was continued when the machine went idle", "error", err)
	}
}

// begin starts the Job at the head of the queue, if the daemon is in a
// position to start anything.
func (s *Service) begin(ctx context.Context) {
	job, r, started, err := s.Start(ctx)
	switch {
	case started:
		s.opts.Logger.Info("run started on an idle machine", "run", r.ID, "job", job.ID)
	case err != nil && !errors.Is(err, context.Canceled):
		// A refusal is the ordinary case: a Run already in progress, or a Job
		// whose Account or tool is not ready. It is the daemon's own business
		// rather than anybody's failure, so it is logged and not raised.
		var refused *RefusedError
		if errors.As(err, &refused) {
			s.opts.Logger.Debug("nothing was started on an idle machine", "reason", err)
			return
		}
		s.opts.Logger.Error("starting a run on an idle machine", "error", err)
	}
}

// machineRead records what the machine said, for the reports that say why work
// is or is not happening.
func (s *Service) machineRead(state idle.State, isIdle bool, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.machine = Machine{
		Read: true, Idle: isIdle, Since: state.Since, OnPower: state.OnPower, Detail: why,
	}
}

// machineUnreadable records that the machine could not be read, which is a
// state to report rather than one to hide.
func (s *Service) machineUnreadable(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.machine = Machine{Detail: idle.Unreadable(err)}
}

// MachineState is what the daemon last read about the machine. A daemon that
// has not looked yet reports a machine it could not read, because that is what
// it knows.
func (s *Service) MachineState() Machine {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.machine == (Machine{}) {
		return Machine{Detail: "the machine has not been looked at yet"}
	}
	return s.machine
}
