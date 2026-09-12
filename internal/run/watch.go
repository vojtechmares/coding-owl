package run

import (
	"context"
	"errors"
	"time"

	"github.com/vojtechmares/coding-owl/internal/idle"
)

// maxHold is the longest the watcher waits before asking the queue again after
// something refused to start. A Job that cannot start - no Account named, a
// tool nobody can run - is waiting for a person, and asking every few seconds
// all night would spend the machine this product exists to leave alone.
const maxHold = 5 * time.Minute

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
//
// The policy is read from the configuration on every look, so that changing
// what counts as Idle takes effect the way changing the grace window does.
// How often the machine is looked at is this daemon's for its lifetime.
func (s *Service) Watch(ctx context.Context, d idle.Detector, every time.Duration) {
	if every <= 0 {
		every = idle.DefaultInterval
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	// What the last readable look said, and nothing before the first one: a
	// machine Owl has not read yet has not changed. A look that fails leaves
	// this alone, so that a reading lost in the middle of somebody returning
	// is caught by the next one rather than forgotten.
	var was *bool
	ask := &asking{}
	// Starting a Job can take minutes - a Project's setup commands run first -
	// and the machine has to stay in view throughout, so it is done off this
	// loop, one at a time.
	// It is not waited for on the way out: a start in the middle of a
	// Project's setup commands is cut short by the daemon closing its Runs,
	// and waiting for it here would sit in front of the very cancellation
	// that ends it. What the goroutine still has to do it does on its own,
	// and the Runs it started are waited for where every Run is.
	var attempt chan string
	for {
		state, err := d.Read(ctx)
		switch {
		case err != nil:
			// Not knowing is not knowing somebody is back, and it is not
			// permission to work either: nothing is started and nothing is
			// frozen.
			if ctx.Err() == nil {
				s.opts.Logger.Debug("the machine could not be read", "error", err)
			}
			s.machineUnreadable(err)
		default:
			isIdle, why := s.idlePolicy().Allows(state)
			s.machineRead(state, isIdle, why)
			s.act(ctx, isIdle, was)
			switch {
			case !isIdle:
				// A fresh idle window is a fresh chance: whatever refused last
				// night may have been seen to.
				ask.reset()
				// And a Run this daemon started is not one to leave going on a
				// machine somebody is at, however late it came to be going:
				// starting takes as long as a Project's setup commands do, and
				// the machine can change its mind while it does.
				s.freezeMine(ctx)
			case attempt == nil && ask.due(s.now()):
				attempt = make(chan string, 1)
				go func(c chan string) { c <- s.begin(ctx) }(attempt)
			}
			was = &isIdle
		}
		if attempt != nil {
			select {
			case refusal := <-attempt:
				attempt = nil
				if refusal == "" {
					ask.reset()
				} else {
					ask.refused(s.now(), every)
				}
			default:
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// asking is when the watcher may ask the queue to start something again. What
// cannot start is waiting for a person, so it is asked about less and less
// often rather than on every look.
type asking struct {
	at     time.Time
	waited time.Duration
}

// due reports whether it is time to ask again.
func (a *asking) due(now time.Time) bool { return !now.Before(a.at) }

// refused records that something refused, and waits longer next time.
func (a *asking) refused(now time.Time, every time.Duration) {
	a.waited = nextAsk(a.waited, every)
	a.at = now.Add(a.waited)
}

// reset forgets the waiting, so the next look asks.
func (a *asking) reset() { a.at, a.waited = time.Time{}, 0 }

// nextAsk is how long to wait before asking again after something refused to
// start, given how long the last wait was: one look, then twice that each time,
// up to maxHold. A Job that cannot start is waiting for a person, and a person
// is not going to answer in the next five seconds.
func nextAsk(waited, every time.Duration) time.Duration {
	return min(max(waited*2, every), maxHold)
}

// idlePolicy is what this look holds the machine to: what the configuration
// says, with Owl's own answer where it says nothing. A configuration that
// cannot be read leaves the default in place rather than stopping the watch,
// because a daemon that stopped looking would neither work nor give the
// machine back.
func (s *Service) idlePolicy() idle.Policy {
	policy := idle.DefaultPolicy()
	global, err := s.global()
	if err != nil {
		s.opts.Logger.Debug("the idle policy could not be read", "error", err)
		return policy
	}
	if global.Idle.After > 0 {
		policy.After = global.Idle.After
	}
	if global.Idle.RequirePower != nil {
		policy.RequirePower = *global.Idle.RequirePower
	}
	return policy
}

// act is what a reading means for the work in flight: the change, and nothing
// otherwise. The caller holds no lock.
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
}

// freeze stops the Agent of the Run in flight, if there is one to stop.
func (s *Service) freeze(ctx context.Context) {
	toFreeze := s.freezable()
	_, err := s.Pause(ctx, ByMachine)
	switch {
	case err == nil:
	case !toFreeze:
		// There was usually nothing to freeze - no Run, or one the user froze
		// themselves - which is not worth a word above debug.
		s.opts.Logger.Debug("nothing was frozen when the machine came back into use", "error", err)
	default:
		// There was a Run going and it is still going: the machine is not the
		// user's again, and nothing else is going to say so.
		s.opts.Logger.Warn("a run could not be frozen when the machine came back into use", "error", err)
	}
}

// mine records that this daemon started that Run itself, because the machine
// was Idle. It is called while the Run is being started, before anything can
// end it, so what is written down is never a note about a Run that is over.
func (s *Service) mine(runID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ours[runID] = true
}

// disown forgets that this daemon started that Run, which is what keeps the
// note from outliving the Run it is about.
func (s *Service) disown(runID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ours, runID)
}

// freezeMine stops a Run this daemon started that is still going on a machine
// somebody is at. Nothing is frozen twice, and nothing somebody asked for by
// hand is frozen at all.
func (s *Service) freezeMine(ctx context.Context) {
	if !s.hasMineGoing() {
		return
	}
	if _, err := s.Pause(ctx, ByMachine); err != nil {
		if ctx.Err() != nil || s.ctx.Err() != nil {
			// A daemon on its way out has already asked its Agents to stop.
			return
		}
		s.opts.Logger.Warn("a run this daemon started could not be frozen on a machine in use", "error", err)
	}
}

// hasMineGoing reports whether a Run this daemon started is going and not
// frozen.
func (s *Service) hasMineGoing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, l := range s.live {
		if s.ours[id] && !l.paused {
			return true
		}
	}
	return false
}

// freezable reports whether there is a Run this daemon could freeze, which is
// what tells a refusal nobody needs from one somebody does.
func (s *Service) freezable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.live {
		if !l.paused {
			return true
		}
	}
	return false
}

// thaw continues a Run frozen because somebody came back.
func (s *Service) thaw(ctx context.Context) {
	if _, err := s.Resume(ctx, ByMachine); err != nil {
		s.opts.Logger.Debug("nothing was continued when the machine went idle", "error", err)
	}
}

// begin starts what the caps allow, oldest first, on a machine Owl has to
// itself. It returns what refused: empty when nothing did, and empty for a
// refusal that clears itself. What it returns is both what `owl status` says
// and what makes the watcher wait before asking again.
//
// It keeps starting until nothing more will start, because more than one Run
// may go at once (ADR-0021) and a watcher that started one per look would take
// a minute to reach a cap of four.
func (s *Service) begin(ctx context.Context) string {
	for {
		why, more := s.beginOne(ctx)
		if !more {
			return why
		}
	}
}

// beginOne starts one Job and says whether it is worth asking again.
func (s *Service) beginOne(ctx context.Context) (string, bool) {
	job, r, started, err := s.start(ctx, ByMachine)
	var refusal *RefusedError
	var inHand *busyError
	var capped *CappedError
	switch {
	case started:
		s.opts.Logger.Info("run started on an idle machine", "run", r.ID, "job", job.ID)
	case errors.As(err, &capped):
		// Every cap that applies is taken. That is not something waiting for a
		// person: the Jobs it passed over say so themselves, and it clears
		// itself as Runs finish.
		s.holdingBack("")
		return "", false
	case errors.As(err, &inHand):
		// The daemon already has work in hand, or is stopping. Nothing is
		// waiting for a person, so nothing is reported and nothing waits.
		s.holdingBack("")
		return "", false
	case ctx.Err() != nil:
		// The daemon is stopping. Whatever it was in the middle of - a
		// Project's setup commands, a rebase - was cut short by that rather
		// than by anything being wrong.
		s.holdingBack("")
		return "", false
	case errors.As(err, &refusal):
		// A Job that cannot start is waiting for a person - an Account nobody
		// named, a tool nobody installed - so it is what `owl status` should
		// say rather than something to raise.
		s.opts.Logger.Debug("nothing was started on an idle machine", "reason", err)
		s.holdingBack(err.Error())
		return err.Error(), false
	case err != nil:
		s.opts.Logger.Error("starting a run on an idle machine", "error", err)
		s.holdingBack(err.Error())
		return err.Error(), false
	}
	s.holdingBack("")
	// Something started, so there may be room for another.
	return "", started
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

// holdingBack records what refused to start on an idle machine, so that a
// person asking why nothing is running is told.
func (s *Service) holdingBack(why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holding = why
}

// Holding is what refused to start work on a machine Owl may work on, empty
// when nothing has.
func (s *Service) Holding() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.holding
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
