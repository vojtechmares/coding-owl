package run

import (
	"context"
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// Skip is a Job the scheduler passed over on its way to one it could start,
// and why. A queue that skips is no longer a literal plan, so it has to be
// able to say why the order it ran in was not the order it is in (ADR-0025).
type Skip struct {
	// Job is what was passed over. It keeps its position.
	Job queue.Job
	// Reason is what stopped it: the cap that is binding, or the ceiling.
	Reason string
}

// CappedError is a refusal because every cap that applies is already taken. It
// clears itself as Runs finish, so nothing waits on it - what is waiting is
// said by the Jobs that were passed over rather than by this.
type CappedError struct{ Err error }

func (e *CappedError) Error() string { return e.Err.Error() }
func (e *CappedError) Unwrap() error { return e.Err }

// flight is how many Runs are going: in total, in each Project, and on each
// Account. A Job is active from the moment its Run starts until everything
// that Run leads to is finished, which is the span the caps are about.
type flight struct {
	total   int
	project map[string]int
	account map[string]int
}

func (s *Service) inFlight(ctx context.Context) (flight, error) {
	active, err := s.opts.Store.ListQueue(ctx, string(queue.StateActive))
	if err != nil {
		return flight{}, err
	}
	f := flight{project: map[string]int{}, account: map[string]int{}}
	for _, j := range active {
		f.total++
		f.project[j.Project]++
		if name := accountKey(j.Account); name != "" {
			f.account[name]++
		}
	}
	return f, nil
}

// accountKey is an Account's name as the caps count it: Account names are
// case-insensitive (ADR-0019).
func accountKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// scan is the Job the scheduler would start, the Jobs it passed over to get
// there, and whether there is one. It writes nothing: `owl status` asks the
// same question to say why the queue is in the order it is.
//
// FIFO, oldest first, and the first Job whose caps and ceiling all permit
// (ADR-0025). A Job that is passed over keeps its position.
func (s *Service) scan(ctx context.Context) (store.Job, []Skip, bool, error) {
	global, err := s.global()
	if err != nil {
		return store.Job{}, nil, false, err
	}
	pending, err := s.opts.Store.ListQueue(ctx, string(queue.StatePending))
	if err != nil {
		return store.Job{}, nil, false, err
	}
	f, err := s.inFlight(ctx)
	if err != nil {
		return store.Job{}, nil, false, err
	}
	var skipped []Skip
	for _, j := range pending {
		why, err := s.holds(ctx, j, f, global)
		if err != nil {
			return store.Job{}, nil, false, err
		}
		if why == "" {
			return j, skipped, true, nil
		}
		skipped = append(skipped, Skip{Job: queue.FromStore(j), Reason: why})
	}
	return store.Job{}, skipped, false, nil
}

// holds is what stops this Job starting now, empty when nothing does. The caps
// are asked in the order ADR-0021 writes them down - owl's own, then the
// Project's, then the Account's - and the ceiling last, because it is the one
// a person cannot change by editing a number.
func (s *Service) holds(ctx context.Context, j store.Job, f flight, global config.Global) (string, error) {
	// A Job with no attempts left is not one the scheduler is choosing
	// between; start is what moves it out of the queue (ADR-0025).
	if j.TTL <= 0 {
		return "it has no attempts left", nil
	}
	if f.total >= global.MaxParallelRuns {
		return fmt.Sprintf("owl is at its cap of %s", runs(global.MaxParallelRuns)), nil
	}
	// What the Project is configured to do is read from its base branch, so an
	// Agent cannot raise its own cap (ADR-0014). A Project Owl cannot read is
	// the start's business to report, not the scan's.
	details, err := s.opts.Projects.Show(ctx, j.Project)
	if err != nil {
		return "", nil
	}
	if cap := details.Config.MaxParallelRuns; f.project[j.Project] >= cap {
		return fmt.Sprintf("the project %s is at its cap of %s", j.Project, runs(cap)), nil
	}
	name := j.Account
	if name == "" {
		name = details.Config.Account
	}
	if name == "" {
		// A Project that names no Account cannot run at all, which is a
		// refusal for a person rather than a cap that will clear itself.
		return "", nil
	}
	if limits, ok := global.Ceiling(name); ok && limits.MaxParallel > 0 {
		if f.account[accountKey(name)] >= limits.MaxParallel {
			return fmt.Sprintf("account %s is at its cap of %s", name, runs(limits.MaxParallel)), nil
		}
	}
	// And whether that Account has the headroom: Owl never leaves the user
	// without any (ADR-0020). A ceiling nobody can keep is for a person and
	// stops the scan; being over it is a wait, and the Job is passed over.
	return s.underCeiling(ctx, name)
}

// runs is a cap as a person reads it.
func runs(n int) string {
	if n == 1 {
		return "1 run"
	}
	return fmt.Sprintf("%d runs", n)
}

// exhaust moves the pending Jobs that have no attempts left out of the queue,
// so that the scan is choosing between Jobs that could run (ADR-0025).
func (s *Service) exhaust(ctx context.Context) error {
	pending, err := s.opts.Store.ListQueue(ctx, string(queue.StatePending))
	if err != nil {
		return err
	}
	for _, j := range pending {
		if j.TTL > 0 {
			continue
		}
		state, err := s.opts.Store.ReturnJobToQueue(ctx, j.ID,
			string(queue.StatePending), string(queue.StateExhausted))
		if err != nil {
			return err
		}
		if queue.State(state) != queue.StateExhausted {
			return fmt.Errorf("job %d has no attempts left but is %s", j.ID, state)
		}
		s.opts.Logger.Warn("a job out of attempts was still queued", "job", j.ID)
	}
	return nil
}

// PassedOver is the Jobs the scheduler would pass over if it looked now, and
// why. It is asked rather than remembered, so that what `owl status` says is
// true when it is read rather than when something last looked.
func (s *Service) PassedOver(ctx context.Context) ([]Skip, error) {
	_, skipped, _, err := s.scan(ctx)
	return skipped, err
}
