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
	// Owl rebases its own Job branches and nothing else (ADR-0016). A worktree
	// that is on something else is one an Agent moved, and replaying whatever
	// is checked out there would be rewriting somebody else's history.
	// A worktree on no branch at all answers with an error rather than a name,
	// and means the same thing here: whatever is checked out, it is not the
	// Job's branch.
	on, err := git.CurrentBranch(j.Worktree)
	if err != nil || on != j.Branch {
		return s.blocked(j, fmt.Sprintf(
			"the worktree %s is on %s rather than the job's own branch %s, so there is nothing here to rebase",
			j.Worktree, describe(on), j.Branch))
	}

	// A worktree somebody left mid-rebase is not Owl's to finish or throw
	// away. Saying so is the honest answer; reconciling it belongs to garbage
	// collection (ADR-0015, ADR-0016).
	inProgress, err := git.RebaseInProgress(j.Worktree)
	if err != nil {
		return err
	}
	if inProgress {
		return s.blocked(j, fmt.Sprintf(
			"a rebase is already in progress in %s; finish or abort it before this job runs again", j.Worktree))
	}

	// What the remote knows is worth having before the branch is replayed, but
	// a remote that cannot be reached is not a reason to leave a Job unstarted:
	// the rebase is onto the Project's own base branch either way, and nothing
	// the user has is moved by a fetch.
	if err := git.FetchBase(ctx, details.Path, details.BaseBranch); err != nil {
		s.opts.Logger.Warn("fetching the base branch",
			"project", details.Name, "branch", details.BaseBranch, "error", err)
	}

	conflict, err := git.Rebase(j.Worktree, details.BaseBranch)
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
