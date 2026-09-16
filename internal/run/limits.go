package run

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// limits bounds a Run in flight against the two things its phase says about it
// (ADR-0036): how long it may run at all, and how long it may go without
// saying anything.
//
// Both are needed and neither subsumes the other. A total bound is the only
// one that catches an Agent working busily towards nothing, and it has to be
// generous enough for the longest honest Run, which makes it slow to notice a
// tool that has simply stopped. Silence catches that in minutes, and says
// nothing about a Run that talks steadily for ever.
//
// Everything past the deciding is the interruption machinery's: a limit ends
// the Run the same way the grace window does, through SIGTERM and then SIGKILL
// to the whole process group (ADR-0011, ADR-0034). The one difference is that
// it ends as a failure, so the attempt is spent.
type limits struct {
	mu       sync.Mutex
	total    *time.Timer
	stall    *time.Timer
	stallFor time.Duration
	stopped  bool
}

// watchLimits starts both of a phase's limits. The returned value is stopped
// when the Run ends, and told whenever the Agent says something.
func (s *Service) watchLimits(runID int64, set Settings) *limits {
	// resolve always sets both, since every level it reads from starts at the
	// defaults. A zero here would mean a timer that fires at once and a Run
	// killed the moment it starts, so it is read as the default rather than
	// taken literally: ADR-0036 promises that every Run is bounded, not that
	// every caller remembered to say by how much.
	timeout, stall := set.Timeout.Value, set.Stall.Value
	if timeout <= 0 {
		timeout = DefaultExecuteTimeout
		s.opts.Logger.Warn("a run reached its agent with no timeout, using the default",
			"run", runID, "phase", set.Phase, "timeout", timeout)
	}
	if stall <= 0 {
		stall = DefaultStall
		s.opts.Logger.Warn("a run reached its agent with no stall limit, using the default",
			"run", runID, "phase", set.Phase, "stall", stall)
	}
	l := &limits{stallFor: stall}
	l.total = time.AfterFunc(timeout, func() {
		s.opts.Logger.Warn("a run passed its timeout and is being ended",
			"run", runID, "phase", set.Phase, "timeout", timeout)
		s.endFailing(runID, fmt.Sprintf("the %s phase ran for %s without finishing",
			set.Phase, limitString(timeout)))
	})
	l.stall = time.AfterFunc(stall, func() {
		s.opts.Logger.Warn("a run said nothing for its stall limit and is being ended",
			"run", runID, "phase", set.Phase, "stall", stall)
		s.endFailing(runID, fmt.Sprintf("the %s phase said nothing for %s",
			set.Phase, limitString(stall)))
	})
	return l
}

// heard restarts the silence. It is called for every line the Agent writes, so
// it is on the hot path of a Run's output and does no more than the timer.
func (l *limits) heard() {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Nothing is restarted once the Run is over, so a late line cannot leave a
	// timer running after the Run it bounded. A line arriving between a limit
	// firing and the Agent dying does restart this one, which changes nothing:
	// the Run is already recorded as ending and a second ending is refused.
	if l.stopped {
		return
	}
	l.stall.Reset(l.stallFor)
}

// stop ends both, and is safe to call more than once.
func (l *limits) stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stopped = true
	l.total.Stop()
	l.stall.Stop()
}

// limitString says a limit the way the file that set it does, so that what a
// blocked Job reports can be matched against the configuration that caused it:
// a Project that wrote `4h` is told `4h` and not `4h0m0s`.
func limitString(d time.Duration) string {
	s := d.String()
	// Whole hours lose both empty units, anything else only the empty seconds:
	// trimming a bare "0m" would take the zero out of the middle of "1h30m".
	if strings.HasSuffix(s, "h0m0s") {
		return strings.TrimSuffix(s, "0m0s")
	}
	if strings.HasSuffix(s, "m0s") {
		return strings.TrimSuffix(s, "0s")
	}
	return s
}
