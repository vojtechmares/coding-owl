package run

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// StateCount is how many Jobs are in one state.
type StateCount struct {
	State queue.State
	Count int
}

// Running is a Run that has not ended, with the Job it is an attempt at.
type Running struct {
	Run Run
	Job queue.Job
}

// Overview is what owl status reports: what is running, how the Jobs stand,
// and - the morning question - what is waiting for a decision and what is
// blocked (ADR-0015).
type Overview struct {
	Running  []Running
	Counts   []StateCount
	Awaiting []queue.Job
	Blocked  []queue.Job
}

// Accept finishes a Job whose work is wanted: the branch stays where it is and
// the worktree is reclaimed (ADR-0015). Owl never pushes it anywhere.
func (s *Service) Accept(ctx context.Context, id int64, force bool) (queue.Job, error) {
	return s.dispose(ctx, id, force, queue.StateDone, false)
}

// Drop refuses a Job's work: the worktree is reclaimed and the branch goes
// with it.
func (s *Service) Drop(ctx context.Context, id int64, force bool) (queue.Job, error) {
	return s.dispose(ctx, id, force, queue.StateCancelled, true)
}

// dispose is the half accept and drop share. Only a Job waiting for a decision
// can be disposed of: everything else is either still going or already
// finished with.
func (s *Service) dispose(ctx context.Context, id int64, force bool, to queue.State, deleteBranch bool) (queue.Job, error) {
	j, err := s.opts.Store.GetJob(ctx, id)
	if err != nil {
		return queue.Job{}, err
	}
	if queue.State(j.State) != queue.StateReview {
		return queue.Job{}, refused("job %d is %s, not waiting for a decision; only a job in review can be accepted or dropped", id, j.State)
	}
	// The Project is read from the store rather than through its
	// configuration: a Project whose configuration stopped being readable
	// still has Jobs someone has to be able to dispose of.
	p, err := s.opts.Store.GetProject(ctx, j.Project)
	if err != nil {
		return queue.Job{}, err
	}

	if j.Worktree != "" {
		if err := s.reclaim(j.Worktree, p.Path, force); err != nil {
			return queue.Job{}, err
		}
	}
	if deleteBranch && j.Branch != "" {
		if err := git.DeleteBranch(p.Path, j.Branch); err != nil {
			return queue.Job{}, err
		}
	}
	// The workspace fields say what the Job has now, so what has been
	// reclaimed is cleared: a dropped Job keeps no branch, because there is no
	// longer a branch of that name to look at.
	branch := j.Branch
	if deleteBranch {
		branch = ""
	}
	if err := s.opts.Store.SetJobWorkspace(ctx, j.ID, branch, ""); err != nil {
		return queue.Job{}, err
	}
	j.Branch, j.Worktree = branch, ""
	if err := s.opts.Store.SetJobState(ctx, j.ID, string(to)); err != nil {
		return queue.Job{}, err
	}
	j.State = string(to)
	s.opts.Logger.Info("job disposed of", "job", j.ID, "state", to)
	return queue.FromStore(j), nil
}

// reclaim takes a worktree back. A worktree whose directory is already gone -
// removed by hand, or by a disposal that failed halfway - is pruned rather
// than removed, so that disposal can always finish rather than wedging on a
// worktree nobody can use.
func (s *Service) reclaim(worktree, project string, force bool) error {
	if _, err := os.Stat(worktree); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		s.opts.Logger.Warn("the worktree was already gone", "worktree", worktree)
		return git.PruneWorktrees(project)
	}
	if !force {
		clean, err := git.WorktreeIsClean(worktree)
		if err != nil {
			return err
		}
		if !clean {
			return refused(
				"the worktree at %s holds changes nobody has committed; commit or discard them, or pass --force to reclaim it anyway",
				worktree)
		}
	}
	return git.RemoveWorktree(project, worktree, force)
}

// Overview reports where the work stands.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	jobs, err := s.opts.Store.ListAllJobs(ctx)
	if err != nil {
		return Overview{}, err
	}
	byID := make(map[int64]store.Job, len(jobs))
	counts := map[queue.State]int{}
	var out Overview
	for _, j := range jobs {
		byID[j.ID] = j
		state := queue.State(j.State)
		counts[state]++
		switch state {
		case queue.StateReview:
			out.Awaiting = append(out.Awaiting, queue.FromStore(j))
		case queue.StateBlocked:
			out.Blocked = append(out.Blocked, queue.FromStore(j))
		}
	}
	// The states are reported in the order a Job passes through them, so the
	// report reads as a lifecycle rather than as an alphabet.
	for _, state := range []queue.State{
		queue.StatePending, queue.StateActive, queue.StateReview,
		queue.StateBlocked, queue.StateDone, queue.StateCancelled, queue.StateExhausted,
	} {
		if n := counts[state]; n > 0 {
			out.Counts = append(out.Counts, StateCount{State: state, Count: n})
		}
	}

	running, err := s.opts.Store.ListRunsInProgress(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, r := range running {
		j, ok := byID[r.JobID]
		if !ok {
			// A Run whose Job is gone is garbage collection's business
			// (ADR-0015), not this report's.
			s.opts.Logger.Warn("a run in progress has no job", "run", r.ID, "job", r.JobID)
			continue
		}
		out.Running = append(out.Running, Running{Run: toRun(r), Job: queue.FromStore(j)})
	}
	return out, nil
}
