package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// forceBack is the last resort of abortRebase, and the state it is for - an
// abort that refused - cannot be arranged reliably from outside the package,
// so it is exercised here on the state it has to recover from: a worktree left
// in the middle of a rebase, on no branch.
func TestForceBackPutsAWorktreeOnItsBranchAgain(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "--quiet", "-b", "main")
	write(t, dir, "both.txt", "shared\n")
	gitOut(t, dir, "add", "--", "both.txt")
	gitOut(t, dir, "commit", "-m", "one")
	worktree := filepath.Join(t.TempDir(), "job")
	if err := AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	write(t, worktree, "both.txt", "what the agent wrote\n")
	gitOut(t, worktree, "commit", "-am", "the agent's work")
	before := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD"))
	write(t, dir, "both.txt", "what the base says\n")
	gitOut(t, dir, "commit", "-am", "the base moves")
	// Left in the middle of a rebase, on no branch, which is what an abort
	// that refused leaves behind.
	_ = exec.Command("git", "-C", worktree, "rebase", "main").Run()
	if progress, err := RebaseInProgress(worktree); err != nil || !progress {
		t.Fatalf("RebaseInProgress = %v, %v, want a rebase to recover from", progress, err)
	}

	if err := forceBack(context.Background(), worktree, "owl/job-1"); err != nil {
		t.Fatalf("forceBack: %v", err)
	}

	if progress, err := RebaseInProgress(worktree); err != nil || progress {
		t.Errorf("RebaseInProgress = %v, %v, want none left", progress, err)
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != "owl/job-1" {
		t.Errorf("the worktree is on %q, want the branch it was put back on", got)
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where the branch was, %s", got, before)
	}
}

func write(t *testing.T, dir, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
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
