// Package gc is garbage collection: the task that keeps the worktrees honest
// (ADR-0015). It reclaims what nothing needs any more, prunes what git is
// still counting, accepts a Job whose work the user has merged, and reports -
// rather than removes - anything that looks like work somebody has not
// finished with.
//
// No Agent is ever involved. This is plain Go over the database and git.
package gc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// DefaultInterval is how often the task runs when nobody says otherwise. Disk
// fills slowly and the point of the task is as much to report as to reclaim,
// so hourly is often enough to keep a night's worth of finished Jobs from
// piling up and rare enough to be invisible.
const DefaultInterval = time.Hour

// DefaultReviewAfter is how long a Job may wait for a decision before it is
// reported as unfinished work. A week is a working cycle: long enough that a
// Job finished on Friday is not nagged about on Monday, short enough that a
// Job nobody ever looked at surfaces while its branch still means something.
const DefaultReviewAfter = 7 * 24 * time.Hour

// Reason says what is unfinished about a piece of unfinished work.
type Reason string

const (
	// ReasonUncommitted is a worktree holding changes nobody has committed.
	// Reclaiming it would destroy them, so it is reported instead.
	ReasonUncommitted Reason = "holds changes nobody has committed"
	// ReasonWaiting is a Job that has waited for a decision longer than the
	// threshold.
	ReasonWaiting Reason = "has been waiting for a decision"
	// ReasonAbandoned is a Job left active by a daemon that died: no Run of it
	// is in progress, and no daemon is going to finish it.
	ReasonAbandoned Reason = "is active but no daemon is running it"
)

// Unfinished is one piece of work garbage collection will not touch and the
// user has to decide about.
type Unfinished struct {
	// Job is the Job it belongs to, and is zero for a worktree belonging to
	// none.
	Job int64
	// Project is the Project it belongs to, empty when nothing says.
	Project string
	// Path is the worktree it is about, empty when it is about a Job rather
	// than a directory.
	Path string
	// Reason says what is unfinished about it.
	Reason Reason
	// Since is how long it has been that way, for the work that has a clock on
	// it; zero otherwise.
	Since time.Duration
}

// Reclaimed is one worktree garbage collection took back.
type Reclaimed struct {
	// Job is the Job it belonged to, zero for a worktree belonging to none.
	Job int64
	// Path is where it was.
	Path string
	// Why says what made it reclaimable, in the words a user reads.
	Why string
}

// Accepted is one Job garbage collection finished on the user's behalf,
// because its work is already in the Project's base branch (ADR-0015).
type Accepted struct {
	Job    int64
	Branch string
	Base   string
}

// Report is what one collection did.
type Report struct {
	// Reclaimed is the worktrees it took back.
	Reclaimed []Reclaimed
	// Accepted is the Jobs whose work turned out to be merged.
	Accepted []Accepted
	// Pruned is the Projects whose stale worktree entries it cleared.
	Pruned []string
	// Unfinished is what it reported rather than touched.
	Unfinished []Unfinished
}

// Empty reports whether the collection had nothing at all to say.
func (r Report) Empty() bool {
	return len(r.Reclaimed) == 0 && len(r.Accepted) == 0 &&
		len(r.Pruned) == 0 && len(r.Unfinished) == 0
}

// Options is what a Service needs to collect.
type Options struct {
	// Store holds the Jobs, the Runs and the Projects.
	Store *store.Store
	// WorktreeDir holds one worktree per Job (ADR-0014). Every directory in it
	// is a candidate; nothing outside it is ever touched.
	WorktreeDir string
	// ReviewAfter is how long a Job may wait for a decision before it is
	// reported. Zero is DefaultReviewAfter.
	ReviewAfter time.Duration
	// Logger receives what a collection nobody asked for has to say.
	Logger *slog.Logger
}

// Service collects.
type Service struct {
	opts Options
	now  func() time.Time
	// interleave runs between reading the worktree directory and reading the
	// database. It is the seam the tests use to put a Run's worktree exactly
	// where a collection must not see it, which is the only way to exercise
	// the ordering those two reads depend on. It is nil in a real Service.
	interleave func()
}

// NewService returns a Service that collects over opts.
func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.ReviewAfter <= 0 {
		opts.ReviewAfter = DefaultReviewAfter
	}
	return &Service{opts: opts, now: time.Now}
}

// Collect runs one collection and reports what it did. An error is something
// that stopped it from finishing: what it could not do to one Project or one
// worktree is logged and the rest goes on, because a single unreadable
// repository must not stop the task that keeps the disk bounded.
func (s *Service) Collect(ctx context.Context) (Report, error) {
	// The directory is read first, and the Jobs after it. A Job exists before
	// its worktree does, so anything listed here belongs to a Job the read
	// below is certain to see - which is what keeps a collection from taking
	// the worktree of a Run that started while it was thinking.
	candidates, err := s.candidates()
	if err != nil {
		return Report{}, err
	}
	if s.interleave != nil {
		s.interleave()
	}
	projects, err := s.opts.Store.ListProjects(ctx)
	if err != nil {
		return Report{}, err
	}
	jobs, running, err := s.opts.Store.JobsAndRunsInProgress(ctx)
	if err != nil {
		return Report{}, err
	}

	var report Report
	// Merged Jobs first: accepting one makes its worktree reclaimable, so the
	// reconciling below takes it in the same collection rather than the next.
	jobs = s.accept(ctx, projects, jobs, &report)
	s.reconcile(ctx, projects, jobs, candidates, &report)
	s.prune(projects, &report)
	s.report(jobs, running, &report)
	return report, nil
}

// Unfinished is what a collection would report and not touch, without
// collecting anything. owl status asks this so that it answers about now
// rather than about the last collection (ADR-0015).
func (s *Service) Unfinished(ctx context.Context) ([]Unfinished, error) {
	candidates, err := s.candidates()
	if err != nil {
		return nil, err
	}
	jobs, running, err := s.opts.Store.JobsAndRunsInProgress(ctx)
	if err != nil {
		return nil, err
	}
	var report Report
	s.worktrees(jobs, candidates, &report)
	s.report(jobs, running, &report)
	return report.Unfinished, nil
}

// candidates is every directory under the worktree directory, by path. It is
// read before anything is asked of the database, so that a worktree made while
// a collection is under way is simply not one of this collection's candidates.
func (s *Service) candidates() ([]string, error) {
	entries, err := os.ReadDir(s.opts.WorktreeDir)
	if errors.Is(err, fs.ErrNotExist) {
		// No worktree has ever been made, which is not a problem to report.
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the worktree directory %s: %w", s.opts.WorktreeDir, err)
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			paths = append(paths, filepath.Join(s.opts.WorktreeDir, e.Name()))
		}
	}
	return paths, nil
}

// worktrees reports the worktrees holding changes nobody has committed,
// touching nothing. It is the reading half of reconcile, which is why both go
// through it.
func (s *Service) worktrees(jobs []store.Job, candidates []string, report *Report) {
	s.walk(jobs, candidates, func(path string, j store.Job, owned bool) {
		clean, err := git.WorktreeIsClean(path)
		if err != nil {
			s.opts.Logger.Warn("reading the state of a worktree", "path", path, "error", err)
			return
		}
		if !clean {
			report.Unfinished = append(report.Unfinished, Unfinished{
				Job: j.ID, Project: j.Project, Path: path, Reason: ReasonUncommitted,
			})
		}
	})
}

// walk calls visit for every worktree under the worktree directory that
// nothing has a use for any more: one whose Job is finished with, and one
// belonging to no Job at all. A directory that is not a worktree git can work
// in is never visited - it is somebody else's, or what a half-finished removal
// left for pruning.
func (s *Service) walk(jobs []store.Job, candidates []string, visit func(path string, j store.Job, owned bool)) {
	byPath := jobsByWorktree(jobs)
	byID := jobsByID(jobs)
	for _, path := range candidates {
		j, owned := byPath[resolve(path)]
		if !owned {
			// A worktree here is named after the Job it belongs to (ADR-0014),
			// and the Job exists before its worktree does. Reading the name is
			// what keeps a collection from taking a worktree in the moment
			// between git making it and the Job recording it.
			j, owned = byID[jobID(filepath.Base(path))]
		}
		if owned && !finishedWith(j) {
			// The Job still has a use for it.
			continue
		}
		live, err := git.IsWorktree(path)
		if err != nil {
			s.opts.Logger.Warn("reading a directory under the worktree directory", "path", path, "error", err)
			continue
		}
		if !live {
			continue
		}
		visit(path, j, owned)
	}
}

// accept finishes the Jobs whose branches are already in their Project's base
// branch, and returns the Jobs as they now stand. Merging through ordinary git
// tooling is an accept: Owl does not ask for a second gesture (ADR-0015).
func (s *Service) accept(ctx context.Context, projects []store.Project, jobs []store.Job, report *Report) []store.Job {
	byName := projectsByName(projects)
	out := make([]store.Job, 0, len(jobs))
	for _, j := range jobs {
		if queue.State(j.State) != queue.StateReview || j.Branch == "" {
			out = append(out, j)
			continue
		}
		p, ok := byName[j.Project]
		if !ok {
			out = append(out, j)
			continue
		}
		merged, err := git.BranchIsIn(p.Path, j.Branch, p.BaseBranch)
		if err != nil {
			// A branch nobody can read is not a branch that is merged, and one
			// unreadable repository must not stop the collection.
			s.opts.Logger.Warn("asking whether a job's work is merged",
				"job", j.ID, "branch", j.Branch, "error", err)
			out = append(out, j)
			continue
		}
		if !merged {
			out = append(out, j)
			continue
		}
		if err := s.opts.Store.SetJobState(ctx, j.ID, string(queue.StateDone)); err != nil {
			s.opts.Logger.Error("accepting a job whose work is merged", "job", j.ID, "error", err)
			out = append(out, j)
			continue
		}
		s.opts.Logger.Info("job accepted: its work is in the base branch",
			"job", j.ID, "branch", j.Branch, "base", p.BaseBranch)
		report.Accepted = append(report.Accepted, Accepted{
			Job: j.ID, Branch: j.Branch, Base: p.BaseBranch,
		})
		j.State = string(queue.StateDone)
		out = append(out, j)
	}
	return out
}

// reconcile takes back the worktrees nothing needs any more. A worktree
// holding uncommitted changes is reported rather than reclaimed, whatever its
// Job says: reclaiming disk must never destroy work silently (ADR-0015).
func (s *Service) reconcile(ctx context.Context, projects []store.Project, jobs []store.Job, candidates []string, report *Report) {
	byName := projectsByName(projects)
	s.walk(jobs, candidates, func(path string, j store.Job, owned bool) {
		clean, err := git.WorktreeIsClean(path)
		if err != nil {
			s.opts.Logger.Warn("reading the state of a worktree", "path", path, "error", err)
			return
		}
		if !clean {
			report.Unfinished = append(report.Unfinished, Unfinished{
				Job: j.ID, Project: j.Project, Path: path, Reason: ReasonUncommitted,
			})
			return
		}
		why := "it belongs to no job"
		if owned {
			why = "its job is " + j.State
		}
		if err := s.remove(path, byName, j, owned); err != nil {
			s.opts.Logger.Warn("reclaiming a worktree", "path", path, "error", err)
			return
		}
		if owned && j.Worktree != "" {
			// The Job no longer has a worktree, and saying so is what keeps
			// the next collection from looking for it.
			if err := s.opts.Store.SetJobWorkspace(ctx, j.ID, j.Branch, ""); err != nil {
				s.opts.Logger.Error("recording that a job's worktree went", "job", j.ID, "error", err)
			}
		}
		s.opts.Logger.Info("worktree reclaimed", "path", path, "job", j.ID, "why", why)
		report.Reclaimed = append(report.Reclaimed, Reclaimed{Job: j.ID, Path: path, Why: why})
	})
}

// remove takes a worktree back through the repository that owns it, so that
// git forgets it rather than being left counting a directory that is gone.
// A worktree whose Project cannot be found is removed as a directory, which is
// what is left to do for one nothing claims.
func (s *Service) remove(path string, byName map[string]store.Project, j store.Job, owned bool) error {
	if owned {
		if p, ok := byName[j.Project]; ok {
			return git.RemoveWorktree(p.Path, path, false)
		}
	}
	// A worktree belonging to no Job still belongs to some repository, and
	// git knows which: the .git file in it points at one.
	if root, err := git.Root(path); err == nil {
		return git.RemoveWorktree(root, path, false)
	}
	return os.RemoveAll(path)
}

// prune clears the administrative entries of worktrees whose directories are
// gone, per Project, so git stops reporting a worktree nobody can use.
func (s *Service) prune(projects []store.Project, report *Report) {
	for _, p := range projects {
		stale, err := s.stale(p)
		if err != nil {
			s.opts.Logger.Warn("looking for stale worktree entries", "project", p.Name, "error", err)
			continue
		}
		if len(stale) == 0 {
			continue
		}
		if err := git.PruneWorktrees(p.Path); err != nil {
			s.opts.Logger.Warn("pruning the worktrees of a project", "project", p.Name, "error", err)
			continue
		}
		s.opts.Logger.Info("stale worktree entries pruned", "project", p.Name, "entries", len(stale))
		report.Pruned = append(report.Pruned, p.Name)
	}
}

// stale is the worktrees a Project is still counting whose directories are not
// there. Pruning is only reported when there was something to prune, so that a
// collection with nothing to do says so.
func (s *Service) stale(p store.Project) ([]string, error) {
	paths, err := git.WorktreePaths(p.Path)
	if err != nil {
		return nil, err
	}
	var stale []string
	for _, path := range paths {
		live, err := git.IsWorktree(path)
		if err != nil {
			return nil, err
		}
		if !live {
			stale = append(stale, path)
		}
	}
	return stale, nil
}

// report adds the Jobs nothing is going to resolve on its own: one that has
// waited too long for a decision, and one left active by a daemon that died.
func (s *Service) report(jobs []store.Job, running []store.Run, report *Report) {
	inProgress := map[int64]bool{}
	for _, r := range running {
		inProgress[r.JobID] = true
	}
	now := s.now()
	for _, j := range jobs {
		switch queue.State(j.State) {
		case queue.StateReview:
			// A Job's own clock is when it was produced: Owl records no time
			// of entering review, and the difference only matters for a Job
			// nobody has looked at for a working cycle.
			if waited := now.Sub(j.Created); waited >= s.opts.ReviewAfter {
				report.Unfinished = append(report.Unfinished, Unfinished{
					Job: j.ID, Project: j.Project, Reason: ReasonWaiting, Since: waited,
				})
			}
		case queue.StateActive:
			// Every Agent is a child of the daemon (ADR-0012), so an active
			// Job with no Run in progress is one whose daemon died: nothing is
			// carrying it, and nothing will.
			if !inProgress[j.ID] {
				report.Unfinished = append(report.Unfinished, Unfinished{
					Job: j.ID, Project: j.Project, Reason: ReasonAbandoned,
					Since: now.Sub(j.Created),
				})
			}
		}
	}
	sort.SliceStable(report.Unfinished, func(a, b int) bool {
		return report.Unfinished[a].Job < report.Unfinished[b].Job
	})
}

// finishedWith reports whether a Job has no further use for its worktree:
// it is done with, cancelled, or gone from the database entirely.
func finishedWith(j store.Job) bool {
	switch queue.State(j.State) {
	case queue.StateDone, queue.StateCancelled:
		return true
	default:
		return false
	}
}

// jobsByWorktree indexes the Jobs that have a worktree, by where it is.
func jobsByWorktree(jobs []store.Job) map[string]store.Job {
	out := make(map[string]store.Job, len(jobs))
	for _, j := range jobs {
		if j.Worktree != "" {
			out[resolve(j.Worktree)] = j
		}
	}
	return out
}

// jobsByID indexes every Job by its id, so that a worktree can be matched to
// the Job it is named after.
func jobsByID(jobs []store.Job) map[int64]store.Job {
	out := make(map[int64]store.Job, len(jobs))
	for _, j := range jobs {
		out[j.ID] = j
	}
	return out
}

// jobID reads the Job a worktree directory is named after, and is zero for a
// name that is not one - which no Job ever has, so it matches nothing.
func jobID(name string) int64 {
	id, err := strconv.ParseInt(name, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func projectsByName(projects []store.Project) map[string]store.Project {
	out := make(map[string]store.Project, len(projects))
	for _, p := range projects {
		out[p.Name] = p
	}
	return out
}

// resolve cleans a path and follows symlinks, so that a worktree recorded as
// /tmp/... and read back as /private/tmp/... is one worktree.
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(path)
}

// Describe renders one piece of unfinished work as a sentence, which is what
// owl gc and owl status both print.
func (u Unfinished) Describe() string {
	what := u.Path
	if what == "" {
		what = fmt.Sprintf("job %d", u.Job)
	}
	line := fmt.Sprintf("%s %s", what, u.Reason)
	if u.Since > 0 {
		line += " for " + humanDuration(u.Since)
	}
	return line
}

// humanDuration renders how long something has been waiting the way a person
// says it, rather than as a duration with three units in it.
func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	default:
		return "less than a minute"
	}
}
