package run

import (
	"context"
	"fmt"
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

// killAfterTerm is how long an Agent the grace window ended has to go before it
// is killed outright. ADR-0011 escalates to SIGTERM and ADR-0034 past it: an
// Agent that will not act on being asked would otherwise hold its Job for
// ever, with no verb that reaches it.
const killAfterTerm = 5 * time.Second

// Freezer is who asked for a Run to be frozen, which decides who may continue
// it: the machine going idle again continues what the machine froze, and what
// the user froze is the user's to continue (ADR-0011).
type Freezer string

const (
	// ByUser is `owl pause` and `owl resume`.
	ByUser Freezer = "user"
	// ByMachine is somebody coming back to the machine, and leaving again.
	ByMachine Freezer = "machine"
)

// live is a Run this daemon can still reach: the Agent it started, and whether
// it is frozen.
type live struct {
	proc   agent.Process
	jobID  int64
	paused bool
	// by is who froze it, and is meaningless while it is not frozen.
	by Freezer
	// window ends a Run that stays frozen. It runs only while paused.
	window *time.Timer
	// endedWhy is why this daemon is ending the Run on purpose - the grace
	// window passing, its Account crossing a ceiling - which is what makes
	// the Agent's death an interruption rather than a failure. Empty for a Run
	// nothing is ending.
	endedWhy string
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
		delete(s.ours, runID)
	}
}

// interrupted is why this daemon ended the Run, and whether it did.
func (s *Service) interrupted(runID int64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.live[runID]
	if !ok || l.endedWhy == "" {
		return "", false
	}
	return l.endedWhy, true
}

// Pause freezes the Run in progress and everything its Agent started, and
// starts the grace window. The machine is the user's again the moment this
// returns (ADR-0011).
func (s *Service) Pause(ctx context.Context, by Freezer) (Run, error) {
	// How long the Run may stay frozen is read before anything is signalled:
	// a configuration nobody can read is the caller's mistake, not a reason to
	// freeze a Run that nothing will then release.
	//
	// That holds for a person, who is told and can fix it. It cannot hold for
	// the machine: refusing there would leave the Agent running on a machine
	// its user has come back to, for as long as the file stays broken. Owl's
	// own window is used instead, and said out loud.
	window, err := s.graceWindow()
	if err != nil {
		if by != ByMachine {
			return Run{}, err
		}
		s.opts.Logger.Warn("freezing on the default grace window: the configuration could not be read",
			"grace", DefaultGraceWindow, "error", err)
		window = DefaultGraceWindow
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// A daemon that is stopping has already asked its Agents to stop, and
	// freezing one now would leave it unable to hear that.
	if s.ctx.Err() != nil {
		return Run{}, refused("the daemon is stopping; nothing is frozen now")
	}
	runID, l, err := s.onlyLive(ctx, "paused")
	if err != nil {
		return Run{}, err
	}
	if l.endedWhy != "" {
		return Run{}, ending(runID, l.endedWhy)
	}
	if l.paused {
		// Nothing to do, but who asked still matters: `owl pause` stops a Run
		// regardless (ADR-0011), so a user asking for a Run the machine froze
		// takes it over, and walking away again will not continue it.
		if by == ByUser {
			l.by = ByUser
			// Nothing was done, but something changed, and a person told their
			// pause failed would not know that it had taken hold.
			return Run{}, refused(
				"run %d is already paused, and will stay paused whatever the machine does; owl resume continues it",
				runID)
		}
		return Run{}, refused("run %d is already paused; owl resume continues it", runID)
	}
	if err := l.proc.SignalGroup(syscall.SIGSTOP); err != nil {
		// The Agent exited between the lookup and the signal, which is the
		// Run ending rather than anything going wrong - but whatever the
		// system said is worth writing down, in case it was something else.
		s.opts.Logger.Warn("a run could not be paused", "run", runID, "error", err)
		return Run{}, refused("run %d ended before it could be paused", runID)
	}
	l.paused, l.by = true, by
	l.window = time.AfterFunc(window, func() { s.expire(runID) })
	s.opts.Logger.Info("run paused", "run", runID, "job", l.jobID, "grace", window, "by", by)
	return s.runOf(ctx, runID)
}

// Resume continues a frozen Run where it was, in the same Run: what an Agent
// was in the middle of survives, because nothing was ended.
func (s *Service) Resume(ctx context.Context, by Freezer) (Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runID, l, err := s.onlyLive(ctx, "resumed")
	if err != nil {
		return Run{}, err
	}
	if l.endedWhy != "" {
		return Run{}, ending(runID, l.endedWhy)
	}
	if !l.paused {
		return Run{}, refused("run %d is not paused", runID)
	}
	// The machine going idle again continues what the machine froze. A Run the
	// user paused stays paused until the user says otherwise: a decision
	// somebody took deliberately is not the machine's to reverse.
	if by == ByMachine && l.by == ByUser {
		return Run{}, refused("run %d was paused with owl pause; owl resume continues it", runID)
	}
	// A Run the user continues by hand stops being the machine's to freeze on
	// its own: they have said they want it going, on the machine they are at.
	if by == ByUser {
		delete(s.ours, runID)
	}
	if err := l.proc.SignalGroup(syscall.SIGCONT); err != nil {
		s.opts.Logger.Warn("a run could not be resumed", "run", runID, "error", err)
		l.release()
		return Run{}, refused("run %d ended while it was paused", runID)
	}
	l.release()
	s.opts.Logger.Info("run resumed", "run", runID, "job", l.jobID, "by", by)
	return s.runOf(ctx, runID)
}

// ending is the refusal for a Run this daemon is already ending, in the words
// it is being ended with: its Agent has been asked to stop and will be killed
// if it does not (ADR-0034). Freezing it again would arm a fresh window around
// a Run that is over, and continuing it would report as running what is being
// ended. The reason is carried rather than assumed, because the grace window
// is only one of them: an Account crossing its ceiling is another (ADR-0020),
// and a person told the wrong one would go looking in the wrong place.
func ending(runID int64, why string) error {
	return refused("run %d is being ended: %s, and its job will be queued again", runID, why)
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

// onlyLive is the Run whose Agent this daemon can still reach. One Agent runs
// at a time in this milestone (ADR-0029), so pausing and resuming take no
// argument. what names what the caller wanted to do to it, so a refusal reads
// as an answer to the question that was asked. The caller holds the lock.
func (s *Service) onlyLive(ctx context.Context, what string) (int64, *live, error) {
	for id, l := range s.live {
		return id, l, nil
	}
	// A Run whose Agent has exited is still in progress until its Project's
	// checks have had their say (ADR-0013), and those are nobody's to freeze:
	// saying there is no run at all would not be true. The same goes for the
	// moments before the Agent runs and after everything has.
	r, ok, err := s.opts.Store.RunInProgress(ctx)
	switch {
	case err != nil:
		// Not knowing is not the same as knowing there is nothing.
		return 0, nil, err
	case !ok:
		return 0, nil, refused("there is no run in progress")
	}
	switch s.stages[r.ID] {
	case StageVerifying:
		return 0, nil, refused(
			"run %d is being verified rather than carried out by an agent, and cannot be %s", r.ID, what)
	case StageFinishing:
		return 0, nil, refused(
			"run %d is being finished: its agent has exited, and it cannot be %s", r.ID, what)
	default:
		return 0, nil, refused(
			"run %d is starting: its agent is not running yet, and it cannot be %s", r.ID, what)
	}
}

// setStage records where a Run in progress is once its Agent is no longer
// the answer; an empty stage forgets the Run.
func (s *Service) setStage(runID int64, stage Stage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stage == "" {
		delete(s.stages, runID)
		return
	}
	s.stages[runID] = stage
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

// withPaused says where a Run that has not ended is, and whether it is frozen.
// Only this daemon knows: the Agent is its own child, and a restart ends every
// Run anyway. The caller holds the lock.
func (s *Service) withPaused(r Run) Run {
	if r.Outcome != "" {
		return r
	}
	if l, ok := s.live[r.ID]; ok {
		r.Paused = l.paused
		r.Stage = StageAgent
		return r
	}
	if stage, ok := s.stages[r.ID]; ok {
		r.Stage = stage
		return r
	}
	r.Stage = StageStarting
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
//
// Whether it is still frozen is settled in the same breath as the ending: a
// Run continued in between is one its user has just been told is running, and
// ending it anyway would make that a lie.
func (s *Service) expire(runID int64) {
	if s.endIf(runID, expiredReason, func(l *live) bool { return l.paused }) {
		s.opts.Logger.Info("the grace window ended a paused run", "run", runID)
	}
}

// end stops the Agent of a Run this daemon is ending on purpose, and records
// why: what it says is what the Run ends with, and the Job goes back in the
// queue rather than being blamed for it.
func (s *Service) end(runID int64, why string) { s.endIf(runID, why, nil) }

// endIf is end for a caller that ends the Run only while something is still
// true of it. The condition is read under the same hold of the lock that
// records the ending, so nothing can change between the two. It reports
// whether this call is the one ending the Run.
func (s *Service) endIf(runID int64, why string, only func(*live) bool) bool {
	s.mu.Lock()
	l, ok := s.live[runID]
	if !ok || l.endedWhy != "" || (only != nil && !only(l)) {
		s.mu.Unlock()
		return false
	}
	l.endedWhy = why
	frozen := l.paused
	l.release()
	proc := l.proc
	jobID := l.jobID
	s.mu.Unlock()

	s.opts.Logger.Debug("a run is being ended", "run", runID, "job", jobID, "reason", why)
	if frozen {
		// Continued first: a stopped process cannot act on being asked to
		// stop, and the signal would sit pending until something released it.
		if err := proc.SignalGroup(syscall.SIGCONT); err != nil {
			s.opts.Logger.Error("continuing a run before ending it", "run", runID, "error", err)
		}
	}
	if err := proc.SignalGroup(syscall.SIGTERM); err != nil {
		s.opts.Logger.Error("ending a run", "run", runID, "error", err)
	}
	// An Agent that has not gone by now is not going to: the Run would
	// otherwise stay in progress with its Job held and nothing able to release
	// it. Signalling a Run that has already ended is refused by the executor,
	// so this costs nothing when the Agent did stop.
	time.AfterFunc(killAfterTerm, func() { s.kill(runID) })
	return true
}

// kill ends an Agent that did not act on being asked to stop.
func (s *Service) kill(runID int64) {
	s.mu.Lock()
	l, ok := s.live[runID]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.opts.Logger.Warn("an agent did not stop when it was asked, and is being killed", "run", runID)
	if err := l.proc.SignalGroup(syscall.SIGKILL); err != nil {
		s.opts.Logger.Error("killing a run that would not stop", "run", runID, "error", err)
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

// requeueLeftOver puts every Job that was being carried out back in the queue. No
// Agent survives a restart, so a Job left active is a Job nothing is working
// on; its worktree and branch are left alone, so the next Run carries on in
// place rather than from the Project's base branch (ADR-0011).
//
// It asks what is active rather than what the Runs it just ended belonged to,
// which means a daemon that died halfway through recovering finishes the job
// next time rather than stranding a Job in a state no command can reach.
func (s *Service) requeueLeftOver(ctx context.Context) error {
	jobs, err := s.opts.Store.ListAllJobs(ctx)
	if err != nil {
		return fmt.Errorf("reading the jobs an earlier daemon left behind: %w", err)
	}
	for _, j := range jobs {
		// Anything else has been decided since, and a decision is not
		// something to undo.
		if queue.State(j.State) != queue.StateActive {
			continue
		}
		s.requeue(ctx, j.ID, "returning a job an earlier daemon was carrying out to the queue")
		s.opts.Logger.Info("a job an earlier daemon was carrying out is queued again", "job", j.ID)
	}
	return nil
}
