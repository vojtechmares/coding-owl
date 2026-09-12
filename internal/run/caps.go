package run

import (
	"context"
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/project"
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
	look := newLookup()
	for _, j := range pending {
		why, err := s.holds(ctx, j, f, global, look)
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

// holds is what stops this Job starting now, empty when nothing does.
//
// The most specific constraint first, and owl's own cap last. ADR-0021's table
// is three caps rather than a precedence, and its decision is that the
// scheduler takes the minimum that applies - but what it asks `owl status` for
// is which cap is binding, because "I set global to 4 and nothing changed" is
// the support question. A Job told to raise the global cap, and then still
// held by a Project cap nobody mentioned, has been answered twice and helped
// once.
func (s *Service) holds(ctx context.Context, j store.Job, f flight, global config.Global, look *lookup) (string, error) {
	// A Job with no attempts left is not one the scheduler is choosing
	// between; start is what moves it out of the queue (ADR-0025).
	if j.TTL <= 0 {
		return "it has no attempts left", nil
	}
	// What the Project is configured to do is read from its base branch, so an
	// Agent cannot raise its own cap (ADR-0014). A Project Owl cannot read is
	// passed over with that as the reason rather than handed to start: one
	// repository somebody moved would otherwise stop every other Project's
	// Jobs as well as its own.
	details, err := look.project(ctx, s, j.Project)
	if err != nil {
		return fmt.Sprintf("the project %s could not be read: %v", j.Project, err), nil
	}
	if cap := details.Config.MaxParallelRuns; f.project[j.Project] >= cap {
		return fmt.Sprintf("the project %s is at its cap of %s", j.Project, runs(cap)), nil
	}
	name := j.Account
	if name == "" {
		name = details.Config.Account
	}
	if name == "" {
		// A Project that names no Account cannot run at all. That is for a
		// person rather than something that clears itself, and saying so here
		// is what stops it holding up everything behind it.
		return noAccount(details), nil
	}
	if limits, ok := global.Ceiling(name); ok && limits.MaxParallel > 0 {
		if f.account[accountKey(name)] >= limits.MaxParallel {
			return fmt.Sprintf("account %s is at its cap of %s", name, runs(limits.MaxParallel)), nil
		}
	}
	// And whether that Account has the headroom: Owl never leaves the user
	// without any (ADR-0020). A ceiling nobody can keep is for a person and
	// stops the scan; being over it is a wait, and the Job is passed over.
	if waiting, err := look.ceiling(ctx, s, name); err != nil || waiting != "" {
		return waiting, err
	}
	// Owl's own cap last, so that a Job held by something narrower is told
	// about the narrower thing.
	if f.total >= global.MaxParallelRuns {
		return fmt.Sprintf("owl is at its cap of %s", runs(global.MaxParallelRuns)), nil
	}
	return "", nil
}

// lookup is what one scan remembers, so that a queue of fifty Jobs in one
// Project does not read that Project fifty times. `owl status` asks for a scan
// and the desktop app asks every two seconds, and reading a Project forks git
// several times over.
type lookup struct {
	projects map[string]project.Details
	failed   map[string]error
	ceilings map[string]string
}

func newLookup() *lookup {
	return &lookup{
		projects: map[string]project.Details{},
		failed:   map[string]error{},
		ceilings: map[string]string{},
	}
}

func (l *lookup) project(ctx context.Context, s *Service, name string) (project.Details, error) {
	if d, ok := l.projects[name]; ok {
		return d, nil
	}
	if err, ok := l.failed[name]; ok {
		return project.Details{}, err
	}
	d, err := s.opts.Projects.Show(ctx, name)
	if err != nil {
		l.failed[name] = err
		return project.Details{}, err
	}
	l.projects[name] = d
	return d, nil
}

func (l *lookup) ceiling(ctx context.Context, s *Service, account string) (string, error) {
	key := accountKey(account)
	if waiting, ok := l.ceilings[key]; ok {
		return waiting, nil
	}
	waiting, err := s.underCeiling(ctx, account)
	if err != nil {
		return "", err
	}
	l.ceilings[key] = waiting
	return waiting, nil
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
