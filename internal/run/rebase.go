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

	// And a worktree of the Project's own repository: a directory git can
	// work in at that path could be anybody's checkout put there since the
	// Job's was removed, and replaying what is checked out in it would be
	// rewriting a branch of some other repository (ADR-0016).
	ours, err := git.WorktreeBelongsTo(j.Worktree, details.Path)
	if err != nil {
		return err
	}
	if !ours {
		return s.blocked(j, fmt.Sprintf(
			"%s belongs to another repository, not to project %s at %s, so this job cannot be carried out there",
			j.Worktree, details.Name, details.Path))
	}

	// What the remote knows is what the branch is replayed onto: the local
	// base branch is the user's, is never moved by Owl, and says only what
	// they last pulled. A Project with no remote has only its local base, and
	// a remote that cannot be reached is not a reason to leave a Job
	// unstarted: the rebase is onto the local base then, and says so
	// (ADR-0016). The fetch comes before the worktree is looked at: it can
	// take minutes, and what is checked out is read as close to the rebase
	// as it can be, so nothing has room to change in between.
	onto := git.BranchRef(details.BaseBranch)
	fetched, err := git.FetchBase(ctx, details.Path, details.BaseBranch)
	switch {
	case err != nil:
		s.opts.Logger.Warn("fetching the base branch; rebasing onto the local one",
			"project", details.Name, "branch", details.BaseBranch, "error", err)
	case fetched != "":
		onto = fetched
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

	// Nothing runs in the worktree between Runs, so a lock on its index is one
	// a killed git left - an Agent ended by SIGKILL in the middle of a commit
	// (ADR-0034), or a rebase the daemon stopped. It is cleared rather than
	// letting it block the Job for good, and said so in the log.
	cleared, err := git.ClearStaleLocks(j.Worktree)
	if err != nil {
		return err
	}
	for _, lock := range cleared {
		s.opts.Logger.Warn("cleared a lock a killed git left in a worktree", "job", j.ID, "path", lock)
	}

	// Not under the caller's context: a client that hangs up would otherwise
	// kill git in the middle of rewriting the branch, and what it leaves
	// behind is the one state a Run must never begin on top of (ADR-0016).
	// The daemon stopping still cuts it short, and it is bounded either way.
	rebasing, done := s.untilClosedFrom(ctx)
	defer done()
	conflict, err := git.Rebase(rebasing, j.Worktree, onto)
	switch {
	case err != nil:
		// A rebase that could not be carried out at all is not something the
		// next Run will do better: the Job waits for somebody rather than
		// trying again every time the queue turns. What git said carries the
		// difference between a rebase that would not go through and a worktree
		// that had to be put back by force.
		return s.blocked(j, fmt.Sprintf("rebasing onto %s could not be carried out: %v", onto, err))
	case conflict.InStash:
		return s.blocked(j, fmt.Sprintf(
			"rebasing onto %s conflicts in %s, in changes nobody had committed; git kept them in the repository's stash",
			onto, strings.Join(conflict.Paths, ", ")))
	case conflict.Conflicted():
		return s.blocked(j, fmt.Sprintf("rebasing onto %s conflicts in %s",
			onto, strings.Join(conflict.Paths, ", ")))
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
