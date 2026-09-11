package gc_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/gc"
	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// fixture is a Service over a temporary database, one registered Project, and
// the worktree directory its Jobs get their worktrees under.
type fixture struct {
	svc         *gc.Service
	store       *store.Store
	repo        string
	worktreeDir string
}

func newFixture(t *testing.T, opts ...func(*gc.Options)) fixture {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gitInit(t, repo)

	st, _, err := store.Open(filepath.Join(root, "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.AddProject(context.Background(), store.Project{
		Name: "api", Path: repo, BaseBranch: "main", Registered: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("AddProject: %v", err)
	}

	o := gc.Options{Store: st, WorktreeDir: filepath.Join(root, "worktrees")}
	for _, apply := range opts {
		apply(&o)
	}
	return fixture{svc: gc.NewService(o), store: st, repo: repo, worktreeDir: o.WorktreeDir}
}

// job puts a Job in the store in that state, with a worktree of its own when
// worktree is true. A Job that has a worktree also has a commit on its branch,
// which is what an Agent leaves behind (ADR-0017): a branch carrying nothing
// of its own is a case of its own, and has a test of its own.
func (f fixture) job(t *testing.T, ref, state string, worktree bool) store.Job {
	t.Helper()
	return f.jobCreated(t, ref, state, worktree, time.Now().UTC())
}

// jobCreated is job with a time of its own, for the scenarios about how long
// something has been where it is.
func (f fixture) jobCreated(t *testing.T, ref, state string, worktree bool, created time.Time) store.Job {
	t.Helper()
	ctx := context.Background()
	j, err := f.store.UpsertJob(ctx, store.Job{
		Source: "local", SourceRef: ref, Project: "api", Prompt: ref,
		State: string(queue.StatePending), TTL: queue.DefaultTTL, Created: created.UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertJob: %v", err)
	}
	if worktree {
		path := filepath.Join(f.worktreeDir, ref)
		branch := "owl/job-" + ref
		if err := git.AddWorktree(f.repo, path, branch, "main"); err != nil {
			t.Fatalf("AddWorktree: %v", err)
		}
		if err := f.store.SetJobWorkspace(ctx, j.ID, branch, path); err != nil {
			t.Fatalf("SetJobWorkspace: %v", err)
		}
		if err := os.WriteFile(filepath.Join(path, "work.txt"), []byte("the job's work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, path, "add", "--", "work.txt")
		gitIn(t, path, "commit", "-m", "the job's work")
		j.Branch, j.Worktree = branch, path
	}
	if state != string(queue.StatePending) {
		if err := f.store.SetJobState(ctx, j.ID, state); err != nil {
			t.Fatalf("SetJobState: %v", err)
		}
		j.State = state
	}
	return j
}

// collect runs one collection and fails the test if it could not.
func (f fixture) collect(t *testing.T) gc.Report {
	t.Helper()
	report, err := f.svc.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return report
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "--", "README.md")
	gitIn(t, dir, "commit", "-m", "initial commit")
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// there reports whether a path is still on disk.
func there(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestCollectReclaimsTheWorktreeOfAFinishedJob(t *testing.T) {
	for _, state := range []queue.State{queue.StateDone, queue.StateCancelled} {
		t.Run(string(state), func(t *testing.T) {
			f := newFixture(t)
			j := f.job(t, "a", string(state), true)

			report := f.collect(t)

			if there(j.Worktree) {
				t.Errorf("the worktree of a %s job is still at %s", state, j.Worktree)
			}
			if len(report.Reclaimed) != 1 || report.Reclaimed[0].Job != j.ID {
				t.Fatalf("reclaimed = %+v, want the job's worktree", report.Reclaimed)
			}
			if !strings.Contains(report.Reclaimed[0].Why, string(state)) {
				t.Errorf("the report says %q, want it to say why", report.Reclaimed[0].Why)
			}
			after, err := f.store.GetJob(context.Background(), j.ID)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if after.Worktree != "" {
				t.Errorf("the job still records a worktree at %s", after.Worktree)
			}
			if after.Branch != j.Branch {
				t.Errorf("branch = %q, want the branch left where it was", after.Branch)
			}
		})
	}
}

func TestCollectReclaimsWhatWasKeptBesideTheWorktree(t *testing.T) {
	// Owl keeps the exclude file that hides a Job's Skills outside the
	// worktree, one directory per Job (ADR-0033). It has nothing left to hide
	// once the worktree is gone.
	root := t.TempDir()
	beside := filepath.Join(root, "worktree-config")
	f := newFixture(t, func(o *gc.Options) { o.WorktreeConfigDir = beside })
	j := f.job(t, "a", string(queue.StateDone), true)
	owned := filepath.Join(beside, strconv.FormatInt(j.ID, 10))
	if err := os.MkdirAll(owned, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "excludes"), []byte("/.claude/skills/\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	f.collect(t)

	if there(owned) {
		t.Errorf("%s is still there after the worktree it belonged to went", owned)
	}
}

func TestCollectLeavesTheWorktreeOfAJobThatIsNotFinished(t *testing.T) {
	for _, state := range []queue.State{queue.StatePending, queue.StateActive, queue.StateReview, queue.StateBlocked} {
		t.Run(string(state), func(t *testing.T) {
			f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
			j := f.job(t, "a", string(state), true)

			report := f.collect(t)

			if !there(j.Worktree) {
				t.Errorf("the worktree of a %s job was reclaimed", state)
			}
			if len(report.Reclaimed) != 0 {
				t.Errorf("reclaimed = %+v, want nothing", report.Reclaimed)
			}
		})
	}
}

func TestCollectReclaimsAWorktreeBelongingToNoJob(t *testing.T) {
	f := newFixture(t)
	stray := filepath.Join(f.worktreeDir, "999")
	if err := git.AddWorktree(f.repo, stray, "owl/stray", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	report := f.collect(t)

	if there(stray) {
		t.Errorf("a worktree belonging to no job is still at %s", stray)
	}
	if len(report.Reclaimed) != 1 || report.Reclaimed[0].Path != stray {
		t.Fatalf("reclaimed = %+v, want the stray worktree", report.Reclaimed)
	}
	if report.Reclaimed[0].Job != 0 {
		t.Errorf("the report attributes the stray worktree to job %d", report.Reclaimed[0].Job)
	}
}

func TestCollectLeavesADirectoryThatIsNotAWorktree(t *testing.T) {
	f := newFixture(t)
	mine := filepath.Join(f.worktreeDir, "notes")
	if err := os.MkdirAll(mine, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mine, "mine.txt"), []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := f.collect(t)

	if !there(mine) {
		t.Error("a directory that is not a worktree was taken")
	}
	if !report.Empty() {
		t.Errorf("the report is %+v, want nothing said about somebody else's directory", report)
	}
}

func TestCollectReportsAWorktreeHoldingUncommittedChanges(t *testing.T) {
	f := newFixture(t)
	j := f.job(t, "a", string(queue.StateDone), true)
	if err := os.WriteFile(filepath.Join(j.Worktree, "stray.txt"), []byte("unfinished\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := f.collect(t)

	if !there(j.Worktree) {
		t.Fatal("a worktree holding uncommitted changes was reclaimed")
	}
	if len(report.Reclaimed) != 0 {
		t.Errorf("reclaimed = %+v, want nothing", report.Reclaimed)
	}
	if len(report.Unfinished) != 1 || report.Unfinished[0].Reason != gc.ReasonUncommitted {
		t.Fatalf("unfinished = %+v, want the worktree reported", report.Unfinished)
	}
	if report.Unfinished[0].Path != j.Worktree {
		t.Errorf("the report names %q, want the worktree", report.Unfinished[0].Path)
	}
}

func TestCollectAcceptsAJobWhoseBranchIsInTheBaseBranch(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	j := f.job(t, "a", string(queue.StateReview), true)
	gitIn(t, f.repo, "merge", "--no-ff", "-m", "merge the job", j.Branch)

	report := f.collect(t)

	after, err := f.store.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateDone {
		t.Errorf("state = %q, want a job whose work is merged to be done", after.State)
	}
	if len(report.Accepted) != 1 || report.Accepted[0].Job != j.ID {
		t.Fatalf("accepted = %+v, want the merged job", report.Accepted)
	}
	if report.Accepted[0].Base != "main" {
		t.Errorf("the report says the work is in %q, want the base branch", report.Accepted[0].Base)
	}
	// Accepting it in this collection is what makes its worktree reclaimable
	// in this collection rather than the next.
	if there(j.Worktree) {
		t.Errorf("the worktree of an accepted job is still at %s", j.Worktree)
	}
}

func TestCollectLeavesAJobWhoseBranchIsNotMerged(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	j := f.job(t, "a", string(queue.StateReview), true)

	report := f.collect(t)

	after, err := f.store.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateReview {
		t.Errorf("state = %q, want a job whose work is not merged left in review", after.State)
	}
	if len(report.Accepted) != 0 {
		t.Errorf("accepted = %+v, want nothing", report.Accepted)
	}
	if !there(j.Worktree) {
		t.Error("the worktree of a job still in review was reclaimed")
	}
}

func TestCollectAcceptsAJobWhoseBranchCarriesNothingOfItsOwn(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	// No commit on the branch, so everything on it is already in the base
	// branch: there is nothing there the base is missing, which is what being
	// merged means. This is the reading every git tool takes of
	// `git branch --merged`, and it is what Owl takes too.
	j := f.job(t, "a", string(queue.StateReview), false)
	if err := f.store.SetJobWorkspace(context.Background(), j.ID, "owl/job-empty", ""); err != nil {
		t.Fatalf("SetJobWorkspace: %v", err)
	}
	gitIn(t, f.repo, "branch", "owl/job-empty")
	gitIn(t, f.repo, "commit", "--allow-empty", "-m", "the base branch moved on")

	report := f.collect(t)

	after, err := f.store.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateDone {
		t.Errorf("state = %q, want a job with nothing on its branch to be done", after.State)
	}
	if len(report.Accepted) != 1 {
		t.Errorf("accepted = %+v, want the job", report.Accepted)
	}
}

func TestCollectDoesNotAcceptAJobThatIsNotInReview(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	// A blocked Job is stuck on something wrong with the work, and merging its
	// branch is not what unsticks it: that is a decision, not a collection
	// (ADR-0013).
	j := f.job(t, "a", string(queue.StateBlocked), true)
	gitIn(t, f.repo, "merge", "--no-ff", "-m", "merge the job", j.Branch)

	report := f.collect(t)

	after, err := f.store.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateBlocked {
		t.Errorf("state = %q, want the blocked job left alone", after.State)
	}
	if len(report.Accepted) != 0 {
		t.Errorf("accepted = %+v, want nothing", report.Accepted)
	}
}

func TestCollectPrunesStaleWorktreeEntries(t *testing.T) {
	f := newFixture(t)
	j := f.job(t, "a", string(queue.StateReview), true)
	if err := os.RemoveAll(j.Worktree); err != nil {
		t.Fatal(err)
	}

	report := f.collect(t)

	if got := gitIn(t, f.repo, "worktree", "list"); strings.Contains(got, j.Worktree) {
		t.Errorf("git still counts a worktree nobody can use:\n%s", got)
	}
	if len(report.Pruned) != 1 || report.Pruned[0] != "api" {
		t.Errorf("pruned = %v, want the project", report.Pruned)
	}
}

func TestCollectPrunesNothingWhenThereIsNothingStale(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	f.job(t, "a", string(queue.StateReview), true)

	report := f.collect(t)

	if len(report.Pruned) != 0 {
		t.Errorf("pruned = %v, want nothing: a collection with nothing to do says so", report.Pruned)
	}
}

func TestCollectReportsAJobWaitingTooLongForADecision(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Nanosecond })
	j := f.job(t, "a", string(queue.StateReview), false)

	report := f.collect(t)

	if len(report.Unfinished) != 1 || report.Unfinished[0].Job != j.ID {
		t.Fatalf("unfinished = %+v, want the waiting job", report.Unfinished)
	}
	if report.Unfinished[0].Reason != gc.ReasonWaiting {
		t.Errorf("reason = %q, want it to say it is waiting for a decision", report.Unfinished[0].Reason)
	}
	after, err := f.store.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateReview {
		t.Errorf("state = %q, want reporting a job to leave it where it was", after.State)
	}
}

func TestCollectLeavesAJobThatHasNotWaitedLongEnough(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	f.job(t, "a", string(queue.StateReview), false)

	report := f.collect(t)

	if len(report.Unfinished) != 0 {
		t.Errorf("unfinished = %+v, want nothing: the job has barely been waiting", report.Unfinished)
	}
}

func TestCollectReportsAJobLeftActiveByADeadDaemon(t *testing.T) {
	f := newFixture(t)
	j := f.job(t, "a", string(queue.StateActive), false)

	report := f.collect(t)

	if len(report.Unfinished) != 1 || report.Unfinished[0].Job != j.ID {
		t.Fatalf("unfinished = %+v, want the abandoned job", report.Unfinished)
	}
	if report.Unfinished[0].Reason != gc.ReasonAbandoned {
		t.Errorf("reason = %q, want it to say nothing is running it", report.Unfinished[0].Reason)
	}
}

func TestCollectLeavesAJobWhoseRunIsInProgress(t *testing.T) {
	f := newFixture(t)
	j := f.job(t, "a", string(queue.StateActive), false)
	if _, err := f.store.StartRun(context.Background(), store.Run{
		JobID: j.ID, Started: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	report := f.collect(t)

	if len(report.Unfinished) != 0 {
		t.Errorf("unfinished = %+v, want nothing: a daemon is running that job", report.Unfinished)
	}
}

func TestCollectWithNothingToDoReportsNothing(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })

	report := f.collect(t)

	if !report.Empty() {
		t.Errorf("the report is %+v, want nothing at all to say", report)
	}
}

func TestCollectWithNoWorktreeDirectoryYetIsNotAFailure(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.WorktreeDir = filepath.Join(t.TempDir(), "never-made") })

	if _, err := f.svc.Collect(context.Background()); err != nil {
		t.Errorf("Collect before any worktree exists = %v, want nothing to do", err)
	}
}

func TestCollectLeavesAWorktreeNamedAfterAJobThatHasNotRecordedItYet(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	// A Run creates the worktree and then records it, so for a moment a live
	// worktree belongs to a Job that does not yet say so. It is named after
	// the Job (ADR-0014), which is what says whose it is.
	j := f.job(t, "a", string(queue.StateActive), false)
	path := filepath.Join(f.worktreeDir, strconv.FormatInt(j.ID, 10))
	if err := git.AddWorktree(f.repo, path, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	report := f.collect(t)

	if !there(path) {
		t.Error("a collection took the worktree of a run that was starting")
	}
	if len(report.Reclaimed) != 0 {
		t.Errorf("reclaimed = %+v, want nothing", report.Reclaimed)
	}
}

func TestUnfinishedReportsWithoutCollecting(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	done := f.job(t, "a", string(queue.StateDone), true)
	if err := os.WriteFile(filepath.Join(done.Worktree, "stray.txt"), []byte("unfinished\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reclaimable := f.job(t, "b", string(queue.StateDone), true)
	abandoned := f.job(t, "c", string(queue.StateActive), false)

	got, err := f.svc.Unfinished(context.Background())

	if err != nil {
		t.Fatalf("Unfinished: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Unfinished = %+v, want the stray worktree and the abandoned job", got)
	}
	if got[0].Path != done.Worktree || got[0].Reason != gc.ReasonUncommitted {
		t.Errorf("Unfinished[0] = %+v, want the worktree holding uncommitted changes", got[0])
	}
	if got[1].Job != abandoned.ID || got[1].Reason != gc.ReasonAbandoned {
		t.Errorf("Unfinished[1] = %+v, want the abandoned job", got[1])
	}
	// Asking what is unfinished collects nothing: the worktree that is there
	// to be reclaimed is still there afterwards.
	if !there(reclaimable.Worktree) {
		t.Error("asking what is unfinished reclaimed a worktree")
	}
}

func TestCollectLeavesAWorktreeMadeWhileItWasThinking(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	if err := os.MkdirAll(f.worktreeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(f.worktreeDir, "1")
	// A Run creates its worktree after the collection has read the worktree
	// directory and before it reads the database. Nothing the collection did
	// not see is its business, however little the database says about it.
	f.svc.Interleave(func() {
		if err := git.AddWorktree(f.repo, stray, "owl/job-1", "main"); err != nil {
			t.Errorf("AddWorktree: %v", err)
		}
	})

	report := f.collect(t)

	if !there(stray) {
		t.Error("a collection took a worktree that was made while it was thinking")
	}
	if len(report.Reclaimed) != 0 {
		t.Errorf("reclaimed = %+v, want nothing", report.Reclaimed)
	}
}

func TestCollectLeavesAJobTheDaemonIsCarrying(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) {
		// The daemon has a Run going for job 1, and has not yet written down
		// how it ended - which is exactly the moment two tables would say
		// nothing is running it.
		o.Carrying = func(jobID int64) bool { return jobID == 1 }
	})
	j := f.job(t, "a", string(queue.StateActive), false)

	report := f.collect(t)

	if j.ID != 1 {
		t.Fatalf("the job is %d, and this test is about job 1", j.ID)
	}
	if len(report.Unfinished) != 0 {
		t.Errorf("unfinished = %+v, want nothing: the daemon is carrying that job", report.Unfinished)
	}
}

func TestCollectMeasuresWaitingFromWhenTheJobsRunEnded(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	// A Job produced a month ago whose Run ended a moment ago has been waiting
	// for a decision for a moment, not for as long as it has existed: a Job
	// can queue for weeks before anything runs it.
	j := f.jobCreated(t, "a", string(queue.StateReview), false, time.Now().Add(-30*24*time.Hour))
	ctx := context.Background()
	r, err := f.store.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC()})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := f.store.FinishRun(ctx, r.ID, time.Now().UTC(), "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	report := f.collect(t)

	if len(report.Unfinished) != 0 {
		t.Errorf("unfinished = %+v, want nothing: the job has been in review for a moment", report.Unfinished)
	}
}

func TestCollectMeasuresAnAbandonedJobFromWhenItsRunStarted(t *testing.T) {
	f := newFixture(t)
	// A Run that started three days ago and was ended by a daemon starting a
	// moment ago: nothing has been running the Job for three days, which is
	// what the report has to say rather than "for a moment".
	j := f.jobCreated(t, "a", string(queue.StateActive), false, time.Now().Add(-3*24*time.Hour))
	ctx := context.Background()
	r, err := f.store.StartRun(ctx, store.Run{
		JobID: j.ID, Started: time.Now().Add(-3 * 24 * time.Hour).UTC(),
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if err := f.store.FinishRun(ctx, r.ID, time.Now().UTC(), "interrupted", "the daemon stopped", -1); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	report := f.collect(t)

	if len(report.Unfinished) != 1 || report.Unfinished[0].Reason != gc.ReasonAbandoned {
		t.Fatalf("unfinished = %+v, want the abandoned job", report.Unfinished)
	}
	if got := report.Unfinished[0].Since; got < 3*24*time.Hour {
		t.Errorf("nothing has been running it for %s, want about three days", got)
	}
}

func TestCollectReportsHowLongAJobHasBeenWaiting(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	j := f.job(t, "a", string(queue.StateReview), false)
	ctx := context.Background()
	r, err := f.store.StartRun(ctx, store.Run{JobID: j.ID, Started: time.Now().UTC()})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	ended := time.Now().Add(-3 * time.Hour).UTC()
	if err := f.store.FinishRun(ctx, r.ID, ended, "succeeded", "", 0); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	report := f.collect(t)

	if len(report.Unfinished) != 1 {
		t.Fatalf("unfinished = %+v, want the job that has been waiting three hours", report.Unfinished)
	}
	if got := report.Unfinished[0].Since; got < 3*time.Hour || got > 4*time.Hour {
		t.Errorf("it has been waiting %s, want about three hours", got)
	}
}

func TestCollectDoesNotAcceptAJobSomebodyElseDecidedAbout(t *testing.T) {
	f := newFixture(t, func(o *gc.Options) { o.ReviewAfter = time.Hour })
	j := f.job(t, "a", string(queue.StateReview), true)
	gitIn(t, f.repo, "merge", "--no-ff", "-m", "merge the job", j.Branch)
	// Somebody drops the Job after the collection has read it and before it
	// decides anything, so that the state it read is no longer the state it is
	// acting on.
	f.svc.BeforeDeciding(func() {
		if err := f.store.DequeueJob(context.Background(), j.ID, string(queue.StateCancelled), ""); err != nil {
			t.Errorf("DequeueJob: %v", err)
		}
	})

	report := f.collect(t)

	after, err := f.store.GetJob(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if queue.State(after.State) != queue.StateCancelled {
		t.Errorf("state = %q, want the decision somebody else took", after.State)
	}
	if len(report.Accepted) != 0 {
		t.Errorf("accepted = %+v, want nothing: the job was decided about first", report.Accepted)
	}
}
