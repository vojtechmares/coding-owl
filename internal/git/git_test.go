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

func TestShowFileReadsFromTheRefNotTheWorktree(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml", "committed\n")
	if err := os.WriteFile(filepath.Join(dir, ".coding-owl.yaml"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, found, err := git.ShowFile(dir, "main", ".coding-owl.yaml")
	if err != nil || !found {
		t.Fatalf("ShowFile = _, %v, %v; want found, nil", found, err)
	}
	if string(data) != "committed\n" {
		t.Errorf("ShowFile = %q, want the committed contents", data)
	}
}

func TestShowFileReportsMissingPath(t *testing.T) {
	dir := newRepo(t)

	_, found, err := git.ShowFile(dir, "main", ".coding-owl.yaml")
	if err != nil {
		t.Fatalf("ShowFile: %v", err)
	}
	if found {
		t.Error("ShowFile found a file that was never committed")
	}
}

func TestShowFileTreatsADirectoryAsMissing(t *testing.T) {
	dir := newRepo(t)
	commit(t, dir, ".coding-owl.yaml/inner", "not a config\n")

	_, found, err := git.ShowFile(dir, "main", ".coding-owl.yaml")
	if err != nil {
		t.Fatalf("ShowFile: %v", err)
	}
	if found {
		t.Error("ShowFile treated a directory as a config file")
	}
}

func TestShowFileFailsOnUnknownRef(t *testing.T) {
	dir := newRepo(t)

	if _, _, err := git.ShowFile(dir, "nope", ".coding-owl.yaml"); err == nil {
		t.Fatal("ShowFile accepted an unknown ref")
	}
}
