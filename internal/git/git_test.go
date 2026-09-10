package git_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/git"
)

// newRepo initialises a repository on branch main with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-b", "main")
	commit(t, dir, "README.md", "# test\n")
	return dir
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func commit(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "add", "--", path)
	run(t, dir, "commit", "-m", "add "+path)
}

func TestRootReturnsRepositoryRoot(t *testing.T) {
	dir := newRepo(t)

	got, err := git.Root(dir)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("Root = %q, want %q", got, want)
	}
}

func TestRootRefusesNonRepository(t *testing.T) {
	dir := t.TempDir()

	_, err := git.Root(dir)
	if err == nil {
		t.Fatal("Root accepted a directory that is not a repository")
	}
	if !strings.Contains(err.Error(), "not a git repository") || !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not say the path is not a git repository", err)
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := newRepo(t)

	got, err := git.CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("CurrentBranch = %q, want %q", got, "main")
	}

	run(t, dir, "checkout", "-q", "-b", "develop")
	if got, err = git.CurrentBranch(dir); err != nil || got != "develop" {
		t.Errorf("CurrentBranch after checkout = %q, %v; want %q, nil", got, err, "develop")
	}
}

func TestCurrentBranchOnDetachedHead(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "checkout", "-q", "--detach")

	if _, err := git.CurrentBranch(dir); err == nil {
		t.Fatal("CurrentBranch accepted a detached HEAD")
	}
}

func TestHasBranch(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "branch", "develop")

	for _, tc := range []struct {
		branch string
		want   bool
	}{{"main", true}, {"develop", true}, {"nope", false}} {
		got, err := git.HasBranch(dir, tc.branch)
		if err != nil {
			t.Fatalf("HasBranch(%q): %v", tc.branch, err)
		}
		if got != tc.want {
			t.Errorf("HasBranch(%q) = %v, want %v", tc.branch, got, tc.want)
		}
	}
}

func TestShowFileOnBranchReadsFromTheRefNotTheWorktree(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml", "committed\n")
	if err := os.WriteFile(filepath.Join(dir, ".coding-owl.yaml"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml")
	if err != nil || !found {
		t.Fatalf("ShowFile = _, %v, %v; want found, nil", found, err)
	}
	if string(data) != "committed\n" {
		t.Errorf("ShowFile = %q, want the committed contents", data)
	}
}

func TestShowFileOnBranchReportsMissingPath(t *testing.T) {
	dir := newRepo(t)

	_, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml")
	if err != nil {
		t.Fatalf("ShowFile: %v", err)
	}
	if found {
		t.Error("ShowFile found a file that was never committed")
	}
}

func TestShowFileOnBranchTreatsADirectoryAsMissing(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml/inner", "not a config\n")

	_, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml")
	if err != nil {
		t.Fatalf("ShowFile: %v", err)
	}
	if found {
		t.Error("ShowFile treated a directory as a config file")
	}
}

func TestShowFileOnBranchFailsOnUnknownRef(t *testing.T) {
	dir := newRepo(t)

	if _, _, err := git.ShowFileOnBranch(dir, "nope", ".coding-owl.yaml"); err == nil {
		t.Fatal("ShowFile accepted an unknown ref")
	}
}

func TestHasBranchRejectsRevisionExpressions(t *testing.T) {
	dir := newRepo(t)

	// These all resolve as revisions but none of them is a branch, and
	// accepting one would let a Project's base branch address an arbitrary
	// tree inside the repository.
	for _, expr := range []string{"main:README.md", "main@{0}", "main^{tree}", "HEAD"} {
		got, err := git.HasBranch(dir, expr)
		if err != nil {
			t.Fatalf("HasBranch(%q): %v", expr, err)
		}
		if got {
			t.Errorf("HasBranch(%q) = true, want false: it is a revision, not a branch", expr)
		}
	}
}

func TestHasBranchReportsAMissingDirectory(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")

	got, err := git.HasBranch(gone, "main")
	if err == nil {
		t.Fatalf("HasBranch on a missing directory = %v, nil; want an error", got)
	}
	if !strings.Contains(err.Error(), gone) {
		t.Errorf("error %q does not name the missing directory", err)
	}
}

func TestShowFileOnBranchReportsAMissingDirectory(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")

	if _, _, err := git.ShowFileOnBranch(gone, "main", ".coding-owl.yaml"); err == nil {
		t.Fatal("ShowFile on a missing directory returned no error")
	}
}

func TestRootReportsAMissingDirectory(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")

	if _, err := git.Root(gone); err == nil {
		t.Fatal("Root on a missing directory returned no error")
	}
}

func TestReadsAreLocaleIndependent(t *testing.T) {
	// git translates its diagnostics, so nothing here may depend on reading
	// them. The package forces a C locale for its own invocations; this pins
	// that the caller's locale cannot change any answer.
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	t.Setenv("LANG", "de_DE.UTF-8")
	dir := newRepo(t)
	commit(t, dir, ".meta/.coding-owl.yaml", "last candidate\n")

	if _, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml"); err != nil || found {
		t.Errorf("missing path = found %v, err %v; want not found, nil", found, err)
	}
	data, found, err := git.ShowFileOnBranch(dir, "main", ".meta/.coding-owl.yaml")
	if err != nil || !found {
		t.Fatalf("present path = found %v, err %v; want found, nil", found, err)
	}
	if string(data) != "last candidate\n" {
		t.Errorf("contents = %q", data)
	}
	if ok, err := git.HasBranch(dir, "nope"); err != nil || ok {
		t.Errorf("HasBranch(nope) = %v, %v; want false, nil", ok, err)
	}
}

func TestShowFileOnBranchIsNotShadowedByATag(t *testing.T) {
	// git resolves a bare name against refs/tags before refs/heads, so a tag
	// sharing the base branch's name could otherwise decide a Project's
	// configuration - and pushing a tag is not the permission that pushing a
	// protected branch is.
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml", "from the branch\n")
	run(t, dir, "checkout", "-q", "-b", "other")
	commit(t, dir, ".coding-owl.yaml", "from the tag\n")
	run(t, dir, "tag", "main", "other")
	run(t, dir, "checkout", "-q", "main")

	data, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml")

	if err != nil || !found {
		t.Fatalf("ShowFileOnBranch = found %v, err %v; want found, nil", found, err)
	}
	if string(data) != "from the branch\n" {
		t.Errorf("read %q, want the branch's contents; a tag shadowed the branch", data)
	}
}

func TestReadsIgnoreGitRedirectionInTheEnvironment(t *testing.T) {
	// GIT_DIR and friends override the repository chosen by the working
	// directory, so a daemon started from inside a hook would otherwise read
	// every Project out of whatever repository its environment named.
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml", "the right repository\n")
	elsewhere := newRepo(t)
	commit(t, elsewhere, ".coding-owl.yaml", "the wrong repository\n")
	t.Setenv("GIT_DIR", filepath.Join(elsewhere, ".git"))
	t.Setenv("GIT_WORK_TREE", elsewhere)

	data, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml")
	if err != nil || !found {
		t.Fatalf("ShowFileOnBranch = found %v, err %v; want found, nil", found, err)
	}
	if string(data) != "the right repository\n" {
		t.Errorf("read %q; the environment redirected the read", data)
	}

	root, err := git.Root(dir)
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if root != want {
		t.Errorf("Root = %q, want %q", root, want)
	}
}

func TestCurrentBranchWithATagOfTheSameName(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "tag", "main")

	got, err := git.CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("CurrentBranch = %q, want %q: an ambiguous name was not shortened away", got, "main")
	}
}

func TestReadsIgnorePathspecSettingsInTheEnvironment(t *testing.T) {
	// The pathspec settings are global and mutually exclusive: git refuses a
	// literal pathspec outright when another is set, which would make every
	// Project unreadable rather than merely differently read.
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml", "the config\n")

	for _, env := range []string{"GIT_ICASE_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "1")

			data, found, err := git.ShowFileOnBranch(dir, "main", ".coding-owl.yaml")
			if err != nil || !found {
				t.Fatalf("with %s set: found %v, err %v; want found, nil", env, found, err)
			}
			if string(data) != "the config\n" {
				t.Errorf("read %q, want the committed contents", data)
			}
		})
	}
}

func TestAddWorktreeCutsABranchFromTheBaseBranch(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")

	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	if got := strings.TrimSpace(run(t, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != "owl/job-1" {
		t.Errorf("worktree is on %q, want owl/job-1", got)
	}
	head := strings.TrimSpace(run(t, worktree, "rev-parse", "HEAD"))
	if base := strings.TrimSpace(run(t, dir, "rev-parse", "refs/heads/main")); head != base {
		t.Errorf("worktree HEAD = %s, want main's %s", head, base)
	}
	if list := run(t, dir, "worktree", "list"); !strings.Contains(list, worktree) {
		t.Errorf("git worktree list does not report %s:\n%s", worktree, list)
	}
	if got := strings.TrimSpace(run(t, dir, "rev-parse", "--abbrev-ref", "HEAD")); got != "main" {
		t.Errorf("the original checkout moved to %q, want main", got)
	}
}

func TestAddWorktreeRefusesABranchThatExists(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "branch", "owl/job-1")

	err := git.AddWorktree(dir, filepath.Join(t.TempDir(), "job-1"), "owl/job-1", "main")

	if err == nil {
		t.Fatal("AddWorktree onto an existing branch = nil, want an error")
	}
	if !strings.Contains(err.Error(), "owl/job-1") {
		t.Errorf("error %q does not name the branch", err)
	}
}

func TestAddWorktreeReportsAMissingBaseBranch(t *testing.T) {
	dir := newRepo(t)

	err := git.AddWorktree(dir, filepath.Join(t.TempDir(), "job-1"), "owl/job-1", "nope")

	if err == nil {
		t.Fatal("AddWorktree from a missing branch = nil, want an error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the base branch", err)
	}
}

func TestRemoveWorktreeLeavesTheBranchAlone(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	if err := git.RemoveWorktree(dir, worktree, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree is still on disk")
	}
	if list := run(t, dir, "worktree", "list"); strings.Contains(list, worktree) {
		t.Errorf("git still reports the worktree:\n%s", list)
	}
	if ok, err := git.HasBranch(dir, "owl/job-1"); err != nil || !ok {
		t.Errorf("HasBranch = %v, %v; removing a worktree keeps its branch", ok, err)
	}
}

func TestRemoveWorktreeRefusesUncommittedWorkUnlessForced(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "stray.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := git.RemoveWorktree(dir, worktree, false); err == nil {
		t.Fatal("RemoveWorktree threw away uncommitted work")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the worktree is gone after a refused removal: %v", err)
	}

	if err := git.RemoveWorktree(dir, worktree, true); err != nil {
		t.Fatalf("forced RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(worktree); err == nil {
		t.Errorf("the worktree survived a forced removal")
	}
}

func TestWorktreeIsCleanSeesWhatHasNotBeenCommitted(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	clean, err := git.WorktreeIsClean(worktree)
	if err != nil || !clean {
		t.Fatalf("WorktreeIsClean on a fresh worktree = %v, %v, want clean", clean, err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "stray.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	clean, err = git.WorktreeIsClean(worktree)

	if err != nil {
		t.Fatalf("WorktreeIsClean: %v", err)
	}
	if clean {
		t.Error("a worktree holding a file git does not know about is reported clean")
	}
}

func TestDeleteBranchRemovesEvenUnmergedWork(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "the agent's work\n")
	if err := git.RemoveWorktree(dir, worktree, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if err := git.DeleteBranch(dir, "owl/job-1"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	if ok, err := git.HasBranch(dir, "owl/job-1"); err != nil || ok {
		t.Errorf("HasBranch = %v, %v, want the branch gone", ok, err)
	}
	if err := git.DeleteBranch(dir, "owl/job-1"); err == nil {
		t.Error("deleting a branch that is not there reported success")
	}
}

func TestPruneWorktreesForgetsAWorktreeWhoseDirectoryIsGone(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}

	if err := git.PruneWorktrees(dir); err != nil {
		t.Fatalf("PruneWorktrees: %v", err)
	}

	if list := run(t, dir, "worktree", "list"); strings.Contains(list, worktree) {
		t.Errorf("git still reports a worktree that is not there:\n%s", list)
	}
	if ok, err := git.HasBranch(dir, "owl/job-1"); err != nil || !ok {
		t.Errorf("HasBranch = %v, %v; pruning keeps the branch", ok, err)
	}
}

func TestIsWorktreeSaysNoToAHalfRemovedWorktree(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	if live, err := git.IsWorktree(worktree); err != nil || !live {
		t.Fatalf("IsWorktree on a worktree = %v, %v, want true", live, err)
	}
	// What an interrupted removal leaves behind: the directory, without the
	// file linking it to its repository. git calls such a worktree prunable.
	if err := os.Remove(filepath.Join(worktree, ".git")); err != nil {
		t.Fatal(err)
	}

	live, err := git.IsWorktree(worktree)

	if err != nil {
		t.Fatalf("IsWorktree: %v", err)
	}
	if live {
		t.Error("a directory that has lost its .git file is reported as a worktree")
	}
	if live, err := git.IsWorktree(filepath.Join(t.TempDir(), "never-existed")); err != nil || live {
		t.Errorf("IsWorktree on a directory that is not there = %v, %v, want false", live, err)
	}
}

func TestBranchIsInSaysWhenABranchHasBeenMerged(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "checkout", "-q", "-b", "owl/job-1")
	commit(t, dir, "work.txt", "the job's work\n")
	run(t, dir, "checkout", "-q", "main")

	before, err := git.BranchIsIn(dir, "owl/job-1", "main")
	if err != nil {
		t.Fatalf("BranchIsIn: %v", err)
	}
	run(t, dir, "merge", "--no-ff", "-m", "merge the job", "owl/job-1")
	after, err := git.BranchIsIn(dir, "owl/job-1", "main")
	if err != nil {
		t.Fatalf("BranchIsIn: %v", err)
	}

	if before {
		t.Error("a branch carrying its own commit is reported as merged before it was")
	}
	if !after {
		t.Error("a branch that was merged is not reported as merged")
	}
}

func TestBranchIsInCountsABranchThatAddedNothing(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "branch", "owl/job-1")
	commit(t, dir, "later.txt", "the base branch moved on\n")

	// A branch with no commits of its own is already contained in the base
	// branch: there is nothing on it that the base does not have.
	got, err := git.BranchIsIn(dir, "owl/job-1", "main")

	if err != nil {
		t.Fatalf("BranchIsIn: %v", err)
	}
	if !got {
		t.Error("a branch carrying nothing of its own is reported as not merged")
	}
}

func TestBranchIsInReportsABranchThatIsNotThere(t *testing.T) {
	dir := newRepo(t)

	_, err := git.BranchIsIn(dir, "owl/gone", "main")

	if err == nil {
		t.Fatal("BranchIsIn on a branch that is not there = nil, want an error")
	}
	if !strings.Contains(err.Error(), "owl/gone") {
		t.Errorf("the error %q does not name the branch", err)
	}
}

func TestBranchIsInReportsABaseBranchThatIsNotThere(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "branch", "owl/job-1")

	_, err := git.BranchIsIn(dir, "owl/job-1", "nowhere")

	if err == nil {
		t.Fatal("BranchIsIn against a base that is not there = nil, want an error")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("the error %q does not name the base branch", err)
	}
}

func TestWorktreePathsListsWhatGitIsCounting(t *testing.T) {
	dir := newRepo(t)
	first := filepath.Join(t.TempDir(), "job-1")
	second := filepath.Join(t.TempDir(), "job-2")
	if err := git.AddWorktree(dir, first, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := git.AddWorktree(dir, second, "owl/job-2", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	got, err := git.WorktreePaths(dir)

	if err != nil {
		t.Fatalf("WorktreePaths: %v", err)
	}
	// The repository's own checkout is a worktree too, and is not one of the
	// Job worktrees a caller is asking about.
	for _, want := range []string{first, second} {
		if !hasPath(got, want) {
			t.Errorf("WorktreePaths = %v, want it to hold %s", got, want)
		}
	}
	if hasPath(got, dir) {
		t.Errorf("WorktreePaths = %v, want the repository's own checkout left out", got)
	}
}

// hasPath reports whether paths holds one naming the same directory, through
// symlinks: a temporary directory on macOS is reached by two names.
func hasPath(paths []string, want string) bool {
	resolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		resolved = want
	}
	for _, p := range paths {
		if p == want || p == resolved {
			return true
		}
	}
	return false
}

func TestResolveRefReadsWhatARefPointsAt(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "tag", "-a", "v1.0.0", "-m", "v1.0.0")
	commit(t, dir, "later.txt", "the branch moved on\n")
	head := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
	tagged := strings.TrimSpace(run(t, dir, "rev-parse", "v1.0.0^{commit}"))

	gotHead, err := git.ResolveRef(dir, "main")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	gotTag, err := git.ResolveRef(dir, "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}

	if gotHead != head {
		t.Errorf("ResolveRef(main) = %s, want %s", gotHead, head)
	}
	// An annotated tag is an object of its own, and what a Skill is fetched at
	// is the commit it points at.
	if gotTag != tagged {
		t.Errorf("ResolveRef(v1.0.0) = %s, want the commit the tag points at %s", gotTag, tagged)
	}
	if gotTag == gotHead {
		t.Error("the tag and the branch resolved to the same commit, so this proves nothing")
	}
}

func TestResolveRefReportsARefThatIsNotThere(t *testing.T) {
	dir := newRepo(t)

	_, err := git.ResolveRef(dir, "nope")

	if err == nil {
		t.Fatal("ResolveRef on a ref that is not there = nil, want an error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("the error %q does not name the ref", err)
	}
}

func TestResolveRefReportsASourceThatIsNotARepository(t *testing.T) {
	dir := t.TempDir()

	_, err := git.ResolveRef(dir, "main")

	if err == nil {
		t.Fatal("ResolveRef on something that is not a repository = nil, want an error")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("the error %q does not name the source", err)
	}
}

func TestExportCommitWritesTheTreeAsItWasAtThatCommit(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "SKILL.md", "first\n")
	first := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
	commit(t, dir, "SKILL.md", "second\n")
	into := filepath.Join(t.TempDir(), "export")

	if err := git.ExportCommit(dir, first, into); err != nil {
		t.Fatalf("ExportCommit: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(into, "SKILL.md"))
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}
	if string(got) != "first\n" {
		t.Errorf("the export holds %q, want the content at that commit", got)
	}
	if _, err := os.Stat(filepath.Join(into, ".git")); err == nil {
		t.Error("the export carries a .git, which is a repository rather than a tree")
	}
}

func TestExportCommitReportsACommitThatIsNotThere(t *testing.T) {
	dir := newRepo(t)

	err := git.ExportCommit(dir, "0000000000000000000000000000000000000000", filepath.Join(t.TempDir(), "export"))

	if err == nil {
		t.Fatal("ExportCommit for a commit that is not there = nil, want an error")
	}
}

func TestEnableWorktreeConfigIsIdempotent(t *testing.T) {
	dir := newRepo(t)

	for range 2 {
		if err := git.EnableWorktreeConfig(dir); err != nil {
			t.Fatalf("EnableWorktreeConfig: %v", err)
		}
	}

	if got := strings.TrimSpace(run(t, dir, "config", "--get", "extensions.worktreeConfig")); got != "true" {
		t.Errorf("extensions.worktreeConfig = %q, want true", got)
	}
}

func TestSetWorktreeExcludesHidesAPathInThatWorktreeAlone(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job-1")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	excludes := filepath.Join(t.TempDir(), "excludes")
	if err := os.WriteFile(excludes, []byte(".claude/skills/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(worktree, ".claude", "skills", "go-review"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := git.EnableWorktreeConfig(dir); err != nil {
		t.Fatalf("EnableWorktreeConfig: %v", err)
	}
	if err := git.SetWorktreeExcludes(worktree, excludes); err != nil {
		t.Fatalf("SetWorktreeExcludes: %v", err)
	}

	if got := runIn(t, worktree, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Errorf("the worktree reports what it was told to ignore:\n%s", got)
	}
	// The user's own checkout is left exactly as it was, which is the point of
	// setting this per worktree (ADR-0033).
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills", "go-review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "skills", "go-review", "SKILL.md"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := run(t, dir, "status", "--porcelain"); !strings.Contains(got, ".claude") {
		t.Errorf("the user's own checkout stopped reporting their own files:\n%s", got)
	}
}

func TestGlobalExcludesReadsWhatTheRepositoryWasAlreadyTold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := newRepo(t)

	before, err := git.GlobalExcludes(dir)
	if err != nil {
		t.Fatalf("GlobalExcludes: %v", err)
	}
	run(t, dir, "config", "core.excludesFile", "/somewhere/theirs")
	after, err := git.GlobalExcludes(dir)
	if err != nil {
		t.Fatalf("GlobalExcludes: %v", err)
	}

	if before != "" {
		t.Errorf("GlobalExcludes with none configured and no default file = %q, want nothing", before)
	}
	if after != "/somewhere/theirs" {
		t.Errorf("GlobalExcludes = %q, want what the repository was told", after)
	}
}

func TestGlobalExcludesFindsTheFileGitWouldHaveRead(t *testing.T) {
	// git falls back to $XDG_CONFIG_HOME/git/ignore when nothing is
	// configured, and `git config --get` never reports that: pointing a
	// worktree somewhere else would stop it applying without anyone being told.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	theirs := filepath.Join(home, ".config", "git", "ignore")
	if err := os.MkdirAll(filepath.Dir(theirs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(theirs, []byte("secrets.env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := newRepo(t)

	got, err := git.GlobalExcludes(dir)

	if err != nil {
		t.Fatalf("GlobalExcludes: %v", err)
	}
	if got != theirs {
		t.Errorf("GlobalExcludes = %q, want the file git would have read %q", got, theirs)
	}
}

func TestMirrorGivesUpOnASourceThatNeverAnswers(t *testing.T) {
	// A source is named in a file, and a file can name a server that accepts a
	// connection and then says nothing. Without a deadline that is a queue
	// that never moves again.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})
	defer func(was time.Duration) { git.FetchTimeout = was }(git.FetchTimeout)
	git.FetchTimeout = 2 * time.Second

	done := make(chan error, 1)
	go func() {
		done <- git.Mirror(filepath.Join(t.TempDir(), "mirror"), "git://"+ln.Addr().String()+"/repo.git")
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("Mirror of a source that never answers = nil, want it given up on")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Mirror of a source that never answers had not given up")
	}
}

func TestReadExcludesRefusesWhatIsNotAnOrdinaryFile(t *testing.T) {
	// core.excludesFile is whatever a repository says, and a repository is not
	// always the user's own: /dev/zero or a fifo there is the daemon's memory
	// or the daemon's life.
	theirs := t.TempDir()

	got, err := git.ReadExcludes(theirs)

	if err == nil {
		t.Fatalf("ReadExcludes of a directory = %q, want it refused", got)
	}
	if !strings.Contains(err.Error(), "ordinary file") {
		t.Errorf("the error %q does not say why", err)
	}
}

func TestReadExcludesRefusesAFileLargerThanItWillCarry(t *testing.T) {
	theirs := filepath.Join(t.TempDir(), "excludes")
	if err := os.WriteFile(theirs, make([]byte, (1<<20)+1), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := git.ReadExcludes(theirs)

	if err == nil {
		t.Fatalf("ReadExcludes of an oversized file = %d bytes, want it refused", len(got))
	}
}

func TestReadExcludesIsEmptyForAFileThatIsNotThere(t *testing.T) {
	got, err := git.ReadExcludes(filepath.Join(t.TempDir(), "nothing"))

	if err != nil {
		t.Fatalf("ReadExcludes of a file that is not there: %v", err)
	}
	if got != "" {
		t.Errorf("ReadExcludes = %q, want nothing", got)
	}
}

func TestGlobalExcludesExpandsAHomeRelativePath(t *testing.T) {
	// The canonical snippet is `core.excludesfile = ~/.gitignore_global`, and
	// a caller reading that path literally finds nothing.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir := newRepo(t)
	run(t, dir, "config", "core.excludesFile", "~/.gitignore_global")

	got, err := git.GlobalExcludes(dir)

	if err != nil {
		t.Fatalf("GlobalExcludes: %v", err)
	}
	if got != filepath.Join(home, ".gitignore_global") {
		t.Errorf("GlobalExcludes = %q, want it expanded to the home directory", got)
	}
}

func TestEnableWorktreeConfigRefusesARepositoryItWouldChange(t *testing.T) {
	// Enabling it stops git sharing these between worktrees, and git says they
	// must be moved by hand. Owl does not quietly reconfigure such a
	// repository.
	for _, setting := range [][2]string{
		{"core.bare", "true"},
		{"core.worktree", "/somewhere"},
		{"core.sparseCheckout", "true"},
	} {
		dir := newRepo(t)
		run(t, dir, "config", setting[0], setting[1])

		err := git.EnableWorktreeConfig(dir)

		if err == nil {
			t.Errorf("EnableWorktreeConfig on a repository setting %s = nil, want it refused", setting[0])
			continue
		}
		if !strings.Contains(err.Error(), setting[0]) {
			t.Errorf("the error %q does not name the setting", err)
		}
	}
}

func TestEnableWorktreeConfigDoesNotMindTheDefaults(t *testing.T) {
	// `git init` writes core.bare=false into every ordinary repository, which
	// is the default either way.
	dir := newRepo(t)

	if err := git.EnableWorktreeConfig(dir); err != nil {
		t.Errorf("EnableWorktreeConfig on an ordinary repository = %v", err)
	}
}

// runIn runs git in a directory that is not the repository root.
func runIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return run(t, dir, args...)
}

func TestDefaultBranchIsWhatHeadPointsAt(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "init", "-b", "trunk")
	commit(t, dir, "README.md", "# test\n")

	got, err := git.DefaultBranch(dir)

	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	// The source's own choice, not an assumption about what it is called.
	if got != "trunk" {
		t.Errorf("DefaultBranch = %q, want trunk", got)
	}
}

// onBranch cuts a branch in a worktree and commits a file on it.
func onBranch(t *testing.T, dir, branch, path, body string) {
	t.Helper()
	run(t, dir, "checkout", "-q", "-b", branch)
	commit(t, dir, path, body)
}

func TestRebaseReplaysABranchOntoAMovedBase(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "the agent's work\n")
	commit(t, dir, "from-base.txt", "moved on\n")

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil || conflict.Conflicted() {
		t.Fatalf("Rebase = %+v, %v, want a clean rebase", conflict, err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "from-base.txt")); err != nil {
		t.Errorf("the base's new file is not in the worktree: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(worktree, "work.txt")); err != nil || string(body) != "the agent's work\n" {
		t.Errorf("what was on the branch reads %q, %v", body, err)
	}
	if out := run(t, worktree, "merge-base", "--is-ancestor", "main", "HEAD"); out != "" {
		t.Errorf("the base is not an ancestor of the branch: %s", out)
	}
}

func TestRebaseAbortsAConflictAndNamesThePaths(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "both.txt", "what the agent wrote\n")
	before := strings.TrimSpace(run(t, worktree, "rev-parse", "HEAD"))
	commit(t, dir, "both.txt", "what the base says\n")

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	if strings.Join(conflict.Paths, ",") != "both.txt" {
		t.Errorf("conflicts = %v, want both.txt", conflict.Paths)
	}
	if conflict.InStash {
		t.Errorf("the conflict is reported as one in the stash, and it is in the commits")
	}
	if progress, err := git.RebaseInProgress(worktree); err != nil || progress {
		t.Errorf("RebaseInProgress = %v, %v; an aborted rebase leaves none", progress, err)
	}
	if got := strings.TrimSpace(run(t, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where it was, %s", got, before)
	}
	if body, err := os.ReadFile(filepath.Join(worktree, "both.txt")); err != nil || string(body) != "what the agent wrote\n" {
		t.Errorf("the file reads %q, %v; an aborted rebase leaves the work alone", body, err)
	}
}

func TestRebaseKeepsWhatNobodyCommitted(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "committed\n")
	// What an interrupted Run leaves behind: a tracked file changed and not
	// committed (ADR-0011).
	if err := os.WriteFile(filepath.Join(worktree, "work.txt"), []byte("committed\nand more\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, dir, "from-base.txt", "moved on\n")

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil || conflict.Conflicted() {
		t.Fatalf("Rebase = %+v, %v, want a clean rebase", conflict, err)
	}
	if body, err := os.ReadFile(filepath.Join(worktree, "work.txt")); err != nil || string(body) != "committed\nand more\n" {
		t.Errorf("the uncommitted change reads %q, %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "from-base.txt")); err != nil {
		t.Errorf("the base's new file is not in the worktree: %v", err)
	}
}

func TestRebaseReportsABaseThatIsNotThere(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	conflict, err := git.Rebase(context.Background(), worktree, "no-such-branch")

	if err == nil {
		t.Fatalf("Rebase onto a branch that is not there reported %+v and no error", conflict)
	}
	if !strings.Contains(err.Error(), "no-such-branch") {
		t.Errorf("error %q does not name the base", err)
	}
}

func TestRebaseInProgressSeesOneNobodyFinished(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "both.txt", "what the agent wrote\n")
	commit(t, dir, "both.txt", "what the base says\n")
	// Started by hand and left, which is what git does on a conflict.
	if out := runAllowingFailure(t, worktree, "rebase", "main"); !strings.Contains(out, "CONFLICT") {
		t.Fatalf("the rebase did not conflict:\n%s", out)
	}

	progress, err := git.RebaseInProgress(worktree)

	if err != nil || !progress {
		t.Errorf("RebaseInProgress = %v, %v, want a rebase in progress", progress, err)
	}
	run(t, worktree, "rebase", "--abort")
	if progress, err := git.RebaseInProgress(worktree); err != nil || progress {
		t.Errorf("RebaseInProgress after an abort = %v, %v, want none", progress, err)
	}
}

func TestFetchBaseUpdatesWhatIsKnownWithoutMovingAnything(t *testing.T) {
	origin := filepath.Join(t.TempDir(), "origin.git")
	dir := newRepo(t)
	run(t, dir, "init", "--bare", origin)
	run(t, dir, "remote", "add", "origin", origin)
	run(t, dir, "push", "--quiet", "origin", "main")
	// Somebody else pushes to it.
	other := filepath.Join(t.TempDir(), "other")
	run(t, dir, "clone", "--quiet", "--branch", "main", origin, other)
	commit(t, other, "theirs.txt", "pushed by somebody else\n")
	run(t, other, "push", "--quiet", "origin", "main")
	pushed := strings.TrimSpace(run(t, other, "rev-parse", "HEAD"))
	mine := strings.TrimSpace(run(t, dir, "rev-parse", "main"))

	if err := git.FetchBase(context.Background(), dir, "main"); err != nil {
		t.Fatalf("FetchBase: %v", err)
	}

	if got := strings.TrimSpace(run(t, dir, "rev-parse", "refs/remotes/origin/main")); got != pushed {
		t.Errorf("the remote-tracking branch is at %s, want %s", got, pushed)
	}
	if got := strings.TrimSpace(run(t, dir, "rev-parse", "main")); got != mine {
		t.Errorf("the local branch moved to %s, want it left at %s", got, mine)
	}
}

func TestFetchBaseWithNoRemoteIsNothingToDo(t *testing.T) {
	dir := newRepo(t)

	if err := git.FetchBase(context.Background(), dir, "main"); err != nil {
		t.Errorf("FetchBase on a repository with no remote: %v", err)
	}
}

func TestFetchBaseReportsARemoteItCannotReach(t *testing.T) {
	dir := newRepo(t)
	run(t, dir, "remote", "add", "origin", filepath.Join(t.TempDir(), "not-there.git"))

	err := git.FetchBase(context.Background(), dir, "main")

	if err == nil {
		t.Fatal("FetchBase reported success for a remote that is not there")
	}
	if !strings.Contains(err.Error(), "main") {
		t.Errorf("error %q does not name the branch it was fetching", err)
	}
}

// runAllowingFailure runs git and returns its output whether or not it
// succeeded, for a command a test expects to fail.
func runAllowingFailure(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestRebaseIsOntoTheBranchNotATagOfTheSameName(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "the agent's work\n")
	// A tag with the base branch's name, which git resolves before the branch
	// when a bare name is used. An Agent can plant one: the worktree shares
	// the repository's tags.
	run(t, worktree, "tag", "main", "HEAD")
	commit(t, dir, "from-base.txt", "moved on\n")

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil || conflict.Conflicted() {
		t.Fatalf("Rebase = %+v, %v, want a clean rebase", conflict, err)
	}
	if _, err := os.Stat(filepath.Join(worktree, "from-base.txt")); err != nil {
		t.Errorf("the branch was rebased onto the tag rather than the base branch: %v", err)
	}
}

func TestRebaseReportsAConflictInWhatNobodyCommitted(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, "both.txt", "shared\n")
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// What the branch committed replays cleanly; what nobody committed is
	// where the conflict is.
	commit(t, worktree, "other.txt", "the agent's work\n")
	commit(t, dir, "both.txt", "shared\nfrom the base\n")
	if err := os.WriteFile(filepath.Join(worktree, "both.txt"), []byte("shared\nfrom the agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	if !conflict.InStash {
		t.Errorf("Rebase = %+v; the conflict is in putting back what nobody committed", conflict)
	}
	if strings.Join(conflict.Paths, ",") != "both.txt" {
		t.Errorf("conflicts = %v, want both.txt", conflict.Paths)
	}
	// Nothing was lost: git keeps what it stashed.
	if out := run(t, worktree, "stash", "list"); strings.TrimSpace(out) == "" {
		t.Errorf("the changes nobody committed are not in the stash")
	}
}

func TestRebaseLeavesNoRebaseInProgressWhenItCannotStart(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "the agent's work\n")
	before := strings.TrimSpace(run(t, worktree, "rev-parse", "HEAD"))
	// A file nobody put in git's hands, which the base branch then commits:
	// git refuses to overwrite it and stops before it has begun.
	if err := os.WriteFile(filepath.Join(worktree, "from-base.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit(t, dir, "from-base.txt", "moved on\n")

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err == nil {
		t.Fatalf("Rebase = %+v and no error, want the failure reported", conflict)
	}
	if progress, err := git.RebaseInProgress(worktree); err != nil || progress {
		t.Errorf("RebaseInProgress = %v, %v; a rebase that stopped is aborted whatever stopped it", progress, err)
	}
	if got := strings.TrimSpace(run(t, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where it was, %s", got, before)
	}
	if got := strings.TrimSpace(run(t, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != "owl/job-1" {
		t.Errorf("the worktree is on %q, want the job's branch", got)
	}
}

func TestRebaseCarriesNoOtherBranchWithIt(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "the agent's work\n")
	// A branch of somebody else's on one of the commits about to be rewritten,
	// and the setting that tells git to carry it along. An Agent can set it:
	// the worktree shares the repository's configuration.
	run(t, worktree, "branch", "someone-else", "HEAD")
	elsewhere := strings.TrimSpace(run(t, worktree, "rev-parse", "someone-else"))
	run(t, dir, "config", "rebase.updateRefs", "true")
	commit(t, dir, "from-base.txt", "moved on\n")

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil || conflict.Conflicted() {
		t.Fatalf("Rebase = %+v, %v, want a clean rebase", conflict, err)
	}
	if got := strings.TrimSpace(run(t, worktree, "rev-parse", "someone-else")); got != elsewhere {
		t.Errorf("somebody else's branch was rewritten to %s, want it left at %s", got, elsewhere)
	}
}

func TestRebaseDoesNotRunARepositorysHooks(t *testing.T) {
	dir := newRepo(t)
	worktree := filepath.Join(t.TempDir(), "job")
	if err := git.AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	commit(t, worktree, "work.txt", "the agent's work\n")
	commit(t, dir, "from-base.txt", "moved on\n")
	// A hook in the repository the worktree shares, which an Agent can write.
	hooks := strings.TrimSpace(run(t, dir, "rev-parse", "--git-path", "hooks"))
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(dir, hooks)
	}
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(t.TempDir(), "ran")
	if err := os.WriteFile(filepath.Join(hooks, "pre-rebase"),
		[]byte("#!/bin/sh\ntouch "+ran+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	conflict, err := git.Rebase(context.Background(), worktree, "main")

	if err != nil || conflict.Conflicted() {
		t.Fatalf("Rebase = %+v, %v; a hook of somebody else's refused it", conflict, err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Errorf("the repository's pre-rebase hook was run")
	}
}

func TestHeadBranchTellsADetachedWorktreeFromAFailure(t *testing.T) {
	dir := newRepo(t)

	if got, err := git.HeadBranch(dir); err != nil || got != "main" {
		t.Errorf("HeadBranch = %q, %v, want main", got, err)
	}
	run(t, dir, "checkout", "--quiet", "--detach", "HEAD")
	if got, err := git.HeadBranch(dir); err != nil || got != "" {
		t.Errorf("HeadBranch on a detached worktree = %q, %v, want no branch and no error", got, err)
	}
	// Somewhere that is not a repository at all is a different answer.
	if _, err := git.HeadBranch(t.TempDir()); err == nil {
		t.Error("HeadBranch reported success outside a repository")
	}
}
