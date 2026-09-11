// Package queue is the local work queue: the Jobs a user has asked for and
// the order they will run in. It owns the Source interface of ADR-0008, the
// upsert-on-reference rule of ADR-0032, and the FIFO order of ADR-0025.
package queue

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// State is where a Job is in its lifecycle. The set is closed (ADR-0025).
type State string

const (
	// StatePending is queued and waiting to be run.
	StatePending State = "pending"
	// StateActive is being run right now.
	StateActive State = "active"
	// StateBlocked is stuck on something wrong with the work.
	StateBlocked State = "blocked"
	// StateReview is finished and waiting for a decision.
	StateReview State = "review"
	// StateDone is finished and accepted.
	StateDone State = "done"
	// StateCancelled was taken out of the queue by hand.
	StateCancelled State = "cancelled"
	// StateExhausted ran out of attempts.
	StateExhausted State = "exhausted"
)

// DefaultTTL is how many Runs a Job may take when nobody says otherwise. It is
// generous on purpose: under ADR-0011 a Run the user interrupts by opening
// their laptop costs an attempt exactly as a failure does, so a Job that makes
// real progress every night still has room (ADR-0025).
const DefaultTTL = 10

// Source is a producer of Jobs (ADR-0008). The local queue is the first
// implementation; later ones read a forge or a schedule and feed this same
// queue rather than opening a second path into the scheduler.
type Source interface {
	// Name is stored on every Job the Source produces.
	Name() string
	// Ref is the reference of one piece of work, unique within the Source.
	// Producing the same reference twice yields one Job (ADR-0032).
	Ref() (string, error)
}

// Local is the Source of the Jobs a user queues with owl add. It has nothing
// to derive a reference from - one prompt is not the same work as the same
// prompt typed again - so it generates a ulid.
type Local struct{}

// Name is the value stored in a Job's source column.
func (Local) Name() string { return "local" }

// Ref generates a fresh ulid.
func (Local) Ref() (string, error) { return ulid.Make().String(), nil }

// InvalidError marks a failure the user can fix by asking for something
// different: an empty prompt, a directory belonging to no Project, a position
// the queue does not have.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

func invalid(format string, a ...any) error {
	return &InvalidError{Err: fmt.Errorf(format, a...)}
}

// Job is one standing intent to do a piece of work in one Project
// (ADR-0027).
type Job struct {
	// ID identifies the Job and is what the owl queue commands take.
	ID int64
	// Source names the producer the Job came from.
	Source string
	// SourceRef is the Job's reference within that Source.
	SourceRef string
	// Project is the name of the Project the Job is queued against.
	Project string
	// Prompt is the work to do.
	Prompt string
	// State is where the Job is in its lifecycle.
	State State
	// Branch is the branch the Job's work lands on, empty until it has one.
	Branch string
	// Worktree is where that branch is checked out, empty until it has one.
	Worktree string
	// Planned is whether the Job is planned before it is executed (ADR-0026).
	Planned bool
	// Plan is what its planning Run decided, empty until there is one.
	Plan string
	// Model and Effort are the Job's own overrides, empty when it has none.
	Model  string
	Effort string
	// Reason is why the Job is where it is when no Run explains it.
	Reason string
	// TTL is how many Runs the Job may still take (ADR-0025).
	TTL int
	// Account is the Account the Job ran on, empty until it has run
	// (ADR-0023).
	Account string
	// Position is the Job's place in the queue, counting from one, and zero
	// for a Job that is not in the queue.
	Position int
	// Created is when the Job was first produced.
	Created time.Time
}

// Service is the queue half of the daemon's state.
type Service struct {
	store  *store.Store
	source Source
	now    func() time.Time
}

// NewService returns a Service storing Jobs in st and producing them through
// src.
func NewService(st *store.Store, src Source) *Service {
	return &Service{store: st, source: src, now: time.Now}
}

// AddRequest is what owl add carries.
type AddRequest struct {
	// Project names the Project to queue against. Empty means the Project
	// WorkingDir is in.
	Project string
	// Prompt is the work to do.
	Prompt string
	// WorkingDir is the absolute path the caller ran owl add in.
	WorkingDir string
	// Planned is whether the Job is planned before it is executed. Planning is
	// the default (ADR-0026).
	Planned bool
	// Model and Effort override what every phase of this Job runs at
	// (ADR-0028). Empty leaves it to the Project and the defaults.
	Model  string
	Effort string
	// TTL is how many Runs the Job may take. Zero asks for no particular
	// number and takes DefaultTTL.
	TTL int
}

// Add produces a Job through the Service's Source and queues it behind
// everything already waiting.
func (s *Service) Add(ctx context.Context, req AddRequest) (Job, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return Job{}, invalid("the prompt is empty; say what the job should do")
	}
	name, err := s.resolveProject(ctx, req)
	if err != nil {
		return Job{}, err
	}
	ref, err := s.source.Ref()
	if err != nil {
		return Job{}, fmt.Errorf("producing a reference for source %s: %w", s.source.Name(), err)
	}
	if err := usableSetting("model", req.Model); err != nil {
		return Job{}, err
	}
	if err := usableSetting("effort", req.Effort); err != nil {
		return Job{}, err
	}
	ttl := req.TTL
	switch {
	case ttl < 0:
		return Job{}, invalid("attempts count from one; %d is not a number of runs a job can take", ttl)
	case ttl == 0:
		ttl = DefaultTTL
	}
	j, err := s.store.UpsertJob(ctx, store.Job{
		Source:    s.source.Name(),
		SourceRef: ref,
		Project:   name,
		Prompt:    prompt,
		State:     string(StatePending),
		Planned:   req.Planned,
		Model:     req.Model,
		Effort:    req.Effort,
		TTL:       ttl,
		Created:   s.now().UTC(),
	})
	if err != nil {
		return Job{}, err
	}
	return FromStore(j), nil
}

// usableSetting refuses a model or effort that a tool would read as an option
// rather than as a value.
func usableSetting(what, value string) error {
	if strings.HasPrefix(value, "-") {
		return invalid("%s %q may not start with a dash", what, value)
	}
	return nil
}

// resolveProject names the Project a request is for: the one it asks for by
// name, or the one its working directory sits in.
func (s *Service) resolveProject(ctx context.Context, req AddRequest) (string, error) {
	if req.Project != "" {
		p, err := s.store.GetProject(ctx, req.Project)
		if err != nil {
			return "", err
		}
		return p.Name, nil
	}
	if req.WorkingDir == "" {
		return "", invalid("no project was named and there is no working directory to take one from; pass --project")
	}
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return "", err
	}
	p, ok := containing(req.WorkingDir, projects)
	if !ok {
		return "", invalid("%s is not inside a registered project; run owl add from a project directory, or name one with --project", req.WorkingDir)
	}
	return p.Name, nil
}

// containing returns the Project whose directory holds dir - the innermost
// one, when a Project is registered inside another. Both sides are resolved
// through symlinks first, so /tmp and /private/tmp name the same Project.
func containing(dir string, projects []store.Project) (store.Project, bool) {
	target := resolve(dir)
	var best store.Project
	var bestRoot string
	var found bool
	for _, p := range projects {
		root := resolve(p.Path)
		if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
			continue
		}
		if !found || len(root) > len(bestRoot) {
			best, bestRoot, found = p, root, true
		}
	}
	return best, found
}

// resolve cleans path and follows symlinks, falling back to the path itself
// when it cannot be resolved - a Project whose repository has moved away is
// still worth comparing by name.
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(path)
}

// List returns the queue in order. With all, every Job follows it whatever
// its state, so a Job that has left the queue can still be seen.
func (s *Service) List(ctx context.Context, all bool) ([]Job, error) {
	rows, err := s.jobs(ctx, all)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, FromStore(r))
	}
	return out, nil
}

// jobs reads the queue, or every Job. The queue is the Jobs still waiting: a
// Job being run keeps its position (ADR-0025) but is not waiting.
func (s *Service) jobs(ctx context.Context, all bool) ([]store.Job, error) {
	if all {
		return s.store.ListAllJobs(ctx)
	}
	return s.store.ListQueue(ctx, string(StatePending))
}

// FromStore reads a Job out of the store's shape. It is exported because the
// Runs a Job produces are carried out elsewhere, and one conversion that
// forgets a field is one too many.
func FromStore(j store.Job) Job {
	return Job{
		ID:        j.ID,
		Source:    j.Source,
		SourceRef: j.SourceRef,
		Project:   j.Project,
		Prompt:    j.Prompt,
		State:     State(j.State),
		Branch:    j.Branch,
		Worktree:  j.Worktree,
		Planned:   j.Planned,
		Plan:      j.Plan,
		Model:     j.Model,
		Effort:    j.Effort,
		Reason:    j.Reason,
		TTL:       j.TTL,
		Account:   j.Account,
		Position:  j.Position,
		Created:   j.Created,
	}
}

// Cancel takes a pending Job out of the queue. Nothing is deleted: the Job
// keeps its id and its history and is reported as cancelled.
func (s *Service) Cancel(ctx context.Context, id int64) (Job, error) {
	if _, err := s.pending(ctx, id); err != nil {
		return Job{}, err
	}
	// A Job the user cancelled needs no note: they know why.
	if err := s.store.DequeueJob(ctx, id, string(StateCancelled), ""); err != nil {
		return Job{}, err
	}
	j, err := s.store.GetJob(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return FromStore(j), nil
}

// Extend gives a Job add more attempts, and returns it to the queue if it had
// run out (ADR-0025). Zero asks for no particular number and adds DefaultTTL.
// Extending is adding rather than setting: a Job that still has attempts keeps
// the ones it has.
func (s *Service) Extend(ctx context.Context, id int64, add int) (Job, error) {
	switch {
	case add < 0:
		return Job{}, invalid("attempts count from one; %d is not a number of runs to add", add)
	case add == 0:
		add = DefaultTTL
	}
	j, err := s.store.ExtendJob(ctx, id, add, string(StatePending), string(StateExhausted))
	if err != nil {
		return Job{}, err
	}
	return FromStore(j), nil
}

// Reorder moves a pending Job to position, counting from one. The Jobs it
// passes shift to make room; this is the only way to express urgency, since
// there is no priority (ADR-0025).
func (s *Service) Reorder(ctx context.Context, id int64, position int) (Job, error) {
	if _, err := s.pending(ctx, id); err != nil {
		return Job{}, err
	}
	n, err := s.store.CountQueued(ctx)
	if err != nil {
		return Job{}, err
	}
	if position < 1 || position > n {
		return Job{}, invalid("position %d is outside the queue; positions are between 1 and %d", position, n)
	}
	if err := s.store.MoveJob(ctx, id, position); err != nil {
		return Job{}, err
	}
	j, err := s.store.GetJob(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return FromStore(j), nil
}

// pending returns the Job of that id, refusing one that has left the queue.
// Reordering or cancelling a Job that is being run, or is already finished,
// is a different operation with different consequences (ADR-0027).
func (s *Service) pending(ctx context.Context, id int64) (Job, error) {
	j, err := s.store.GetJob(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if State(j.State) != StatePending {
		return Job{}, invalid("job %d is not pending, it is %s", id, j.State)
	}
	return FromStore(j), nil
}
