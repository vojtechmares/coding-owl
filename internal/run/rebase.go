package run

import (
	"context"
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// rebase puts the Job's branch on top of its Project's base branch, so that
// what the Agent does and what Verification says are both about current code
// (ADR-0016). A Job whose branch cannot be replayed is blocked with what is in
// the way, and nothing else in the Project is touched.
func (s *Service) rebase(ctx context.Context, j store.Job, details project.Details) error {
	// A worktree that is not there any more - cleared by hand, or left behind
	// by a disposal that failed partway - is a Job nothing can carry out. It
	// is blocked rather than reported as an error, or every later owl start
	// would fail on it and nothing behind it in the queue would ever run.
	live, err := git.IsWorktree(j.Worktree)
	if err != nil {
		return err
	}
	if !live {
		return s.blocked(j, fmt.Sprintf(
			"%s is not a worktree git can work in any more, so this job cannot be carried out where it was", j.Worktree))
	}

	// A worktree somebody left mid-rebase is not Owl's to finish or throw
	// away. Saying so is the honest answer; reconciling it belongs to garbage
	// collection (ADR-0015, ADR-0016). It is asked first: a rebase leaves HEAD
	// detached, which the branch check below would otherwise answer for.
	inProgress, err := git.RebaseInProgress(j.Worktree)
	if err != nil {
		return err
	}
	if inProgress {
		return s.blocked(j, fmt.Sprintf(
			"a rebase is already in progress in %s; finish or abort it before this job runs again", j.Worktree))
	}

	// Owl rebases its own Job branches and nothing else (ADR-0016). A worktree
	// that is on something else is one an Agent moved, and replaying whatever
	// is checked out there would be rewriting somebody else's history.
	// A worktree on no branch answers with an empty name, which means the same
	// thing here: whatever is checked out, it is not the Job's branch. Being
	// unable to ask at all is a different answer, and is reported as one.
	on, err := git.HeadBranch(j.Worktree)
	if err != nil {
		return err
	}
	if on != j.Branch {
		return s.blocked(j, fmt.Sprintf(
			"the worktree %s is on %s and not on the job's own branch %s, so there is nothing here to rebase",
			j.Worktree, describe(on), j.Branch))
	}

	// What the remote knows is worth having before the branch is replayed, but
	// a remote that cannot be reached is not a reason to leave a Job unstarted:
	// the rebase is onto the Project's own base branch either way, and nothing
	// the user has is moved by a fetch.
	if err := git.FetchBase(ctx, details.Path, details.BaseBranch); err != nil {
		s.opts.Logger.Warn("fetching the base branch",
			"project", details.Name, "branch", details.BaseBranch, "error", err)
	}

	// Not under the caller's context: a client that hangs up would otherwise
	// kill git in the middle of rewriting the branch, and what it leaves
	// behind is the one state a Run must never begin on top of (ADR-0016).
	// The daemon stopping still cuts it short, and it is bounded either way.
	rebasing, done := s.untilClosedFrom(ctx)
	defer done()
	conflict, err := git.Rebase(rebasing, j.Worktree, details.BaseBranch)
	switch {
	case err != nil:
		// A rebase that could not be carried out at all is not something the
		// next Run will do better: the Job waits for somebody rather than
		// trying again every time the queue turns.
		return s.blocked(j, fmt.Sprintf("rebasing onto %s could not be carried out: %v",
			details.BaseBranch, err))
	case conflict.InStash:
		return s.blocked(j, fmt.Sprintf(
			"rebasing onto %s conflicts in %s, in changes nobody had committed; git kept them in the repository's stash",
			details.BaseBranch, strings.Join(conflict.Paths, ", ")))
	case conflict.Conflicted():
		return s.blocked(j, fmt.Sprintf("rebasing onto %s conflicts in %s",
			details.BaseBranch, strings.Join(conflict.Paths, ", ")))
	}
	return nil
}

// describe names what a worktree is on, for one that is on no branch at all.
func describe(branch string) string {
	if branch == "" {
		return "no branch"
	}
	return branch
}
