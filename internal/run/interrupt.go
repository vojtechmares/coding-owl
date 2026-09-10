package run

import (
	"context"
	"syscall"
	"time"

	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/queue"
)

// DefaultGraceWindow is how long a frozen Run may stay frozen when the daemon's
// configuration does not say (ADR-0011).
const DefaultGraceWindow = 15 * time.Minute

// expiredReason is what a Run that outstayed the grace window is recorded with.
const expiredReason = "the grace window passed while this run was paused"

// live is a Run this daemon can still reach: the Agent it started, and whether
// it is frozen.
type live struct {
	proc   agent.Process
	jobID  int64
	paused bool
	// window ends a Run that stays frozen. It runs only while paused.
	window *time.Timer
	// expired is set when the grace window ended the Run, which is what makes
	// the Agent's death an interruption rather than a failure.
	expired bool
}

// track records a Run as reachable, and returns the function that forgets it.
func (s *Service) track(runID, jobID int64, proc agent.Process) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live[runID] = &live{proc: proc, jobID: jobID}
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if l, ok := s.live[runID]; ok && l.window != nil {
			l.window.Stop()
		}
		delete(s.live, runID)
	}
}

// interrupted reports whether the grace window is what ended this Run.
func (s *Service) interrupted(runID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.live[runID]
	return ok && l.expired
}

// Pause freezes the Run in progress and everything its Agent started, and
// starts the grace window. The machine is the user's again the moment this
// returns (ADR-0011).
func (s *Service) Pause(ctx context.Context) (Run, error) {
	// How long the Run may stay frozen is read before anything is signalled:
	// a configuration nobody can read is the caller's mistake, not a reason to
	// freeze a Run that nothing will then release.
	window, err := s.graceWindow()
	if err != nil {
		return Run{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	runID, l, err := s.onlyLive()
	if err != nil {
		return Run{}, err
	}
	if l.paused {
		return Run{}, refused("run %d is already paused; owl resume continues it", runID)
	}
	if err := l.proc.SignalGroup(syscall.SIGSTOP); err != nil {
		return Run{}, err
	}
	l.paused = true
	l.window = time.AfterFunc(window, func() { s.expire(runID) })
	s.opts.Logger.Info("run paused", "run", runID, "job", l.jobID, "grace", window)
	return s.runOf(ctx, runID)
}

// Resume continues a frozen Run where it was, in the same Run: what an Agent
// was in the middle of survives, because nothing was ended.
func (s *Service) Resume(ctx context.Context) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runID, l, err := s.onlyLive()
	if err != nil {
		return Run{}, err
	}
	if !l.paused {
		return Run{}, refused("run %d is not paused", runID)
	}
	if err := l.proc.SignalGroup(syscall.SIGCONT); err != nil {
		return Run{}, err
	}
	l.release()
	s.opts.Logger.Info("run resumed", "run", runID, "job", l.jobID)
	return s.runOf(ctx, runID)
}

// release forgets that a Run was frozen, and stops the window that was going
// to end it. The caller holds the lock.
func (l *live) release() {
	l.paused = false
	if l.window != nil {
		l.window.Stop()
		l.window = nil
	}
}

// onlyLive is the Run in progress. One Agent runs at a time in this milestone
// (ADR-0029), so pausing and resuming take no argument. The caller holds the
// lock.
func (s *Service) onlyLive() (int64, *live, error) {
	for id, l := range s.live {
		return id, l, nil
	}
	return 0, nil, refused("there is no run in progress")
}

// runOf reads a Run back, so what a caller is told is what the daemon recorded
// rather than what this process happens to remember.
func (s *Service) runOf(ctx context.Context, runID int64) (Run, error) {
	r, err := s.opts.Store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	return s.withPaused(toRun(r)), nil
}

// withPaused says whether a Run that has not ended is frozen. Only this daemon
// knows: the Agent is its own child, and a restart ends every Run anyway.
func (s *Service) withPaused(r Run) Run {
	if l, ok := s.live[r.ID]; ok {
		r.Paused = l.paused
	}
	return r
}

// paused is withPaused for a caller that does not hold the lock.
func (s *Service) paused(r Run) Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withPaused(r)
}

// expire ends a Run that stayed frozen for the whole grace window. A stopped
// process holds its sockets and holds them across a lid close, so the Run ends
// rather than waiting for a user who is not coming back (ADR-0011).
func (s *Service) expire(runID int64) {
	s.mu.Lock()
	l, ok := s.live[runID]
	if !ok || !l.paused {
		s.mu.Unlock()
		return
	}
	l.expired = true
	l.release()
	proc := l.proc
	jobID := l.jobID
	s.mu.Unlock()

	s.opts.Logger.Info("the grace window ended a paused run", "run", runID, "job", jobID)
	// Continued first: a stopped process cannot act on being asked to stop,
	// and the signal would sit pending until something released it.
	if err := proc.SignalGroup(syscall.SIGCONT); err != nil {
		s.opts.Logger.Error("continuing a run before ending it", "run", runID, "error", err)
	}
	if err := proc.SignalGroup(syscall.SIGTERM); err != nil {
		s.opts.Logger.Error("ending a run the grace window expired on", "run", runID, "error", err)
	}
}

// releasePaused continues every frozen Run, so that a daemon which has just
// asked its Agents to stop is not waiting on processes that cannot hear it.
func (s *Service) releasePaused() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for runID, l := range s.live {
		if !l.paused {
			continue
		}
		l.release()
		if err := l.proc.SignalGroup(syscall.SIGCONT); err != nil {
			s.opts.Logger.Error("continuing a paused run while stopping", "run", runID, "error", err)
		}
	}
}

// graceWindow is how long a frozen Run may stay frozen.
func (s *Service) graceWindow() (time.Duration, error) {
	global, err := s.global()
	if err != nil {
		return 0, &RefusedError{Err: err}
	}
	if global.GraceWindow > 0 {
		return global.GraceWindow, nil
	}
	return DefaultGraceWindow, nil
}

// requeueLeftOver puts the Jobs of Runs that were going when the daemon
// stopped back in the queue. Their worktree and branch are left alone, so the
// next Run carries on in place rather than from the Project's base branch
// (ADR-0011).
func (s *Service) requeueLeftOver(ctx context.Context, jobIDs []int64) {
	for _, id := range jobIDs {
		j, err := s.opts.Store.GetJob(ctx, id)
		if err != nil {
			s.opts.Logger.Error("reading a job left over from an earlier daemon", "job", id, "error", err)
			continue
		}
		// Only a Job that was being carried out: anything else has been
		// decided since, and a decision is not something to undo.
		if queue.State(j.State) != queue.StateActive {
			continue
		}
		s.requeue(ctx, id, "returning a job left over from an earlier daemon to the queue")
	}
}
