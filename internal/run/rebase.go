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
	if err := git.FetchBase(details.Path, details.BaseBranch); err != nil {
		s.opts.Logger.Warn("fetching the base branch",
			"project", details.Name, "branch", details.BaseBranch, "error", err)
	}

	conflicts, err := git.Rebase(j.Worktree, details.BaseBranch)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return s.blocked(j, fmt.Sprintf("rebasing onto %s conflicts in %s",
			details.BaseBranch, strings.Join(conflicts, ", ")))
	}
	return nil
}
