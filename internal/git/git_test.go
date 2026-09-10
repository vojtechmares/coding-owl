package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
