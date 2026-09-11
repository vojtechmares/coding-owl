package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// midRebaseWorktree is a repository and a worktree of it left in the middle
// of a rebase, on no branch, with the commit the branch was at before.
func midRebaseWorktree(t *testing.T) (dir, worktree, before string) {
	t.Helper()
	dir = t.TempDir()
	gitOut(t, dir, "init", "--quiet", "-b", "main")
	write(t, dir, "both.txt", "shared\n")
	gitOut(t, dir, "add", "--", "both.txt")
	gitOut(t, dir, "commit", "-m", "one")
	worktree = filepath.Join(t.TempDir(), "job")
	if err := AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	write(t, worktree, "both.txt", "what the agent wrote\n")
	gitOut(t, worktree, "commit", "-am", "the agent's work")
	before = strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD"))
	write(t, dir, "both.txt", "what the base says\n")
	gitOut(t, dir, "commit", "-am", "the base moves")
	_ = exec.Command("git", "-C", worktree, "rebase", "main").Run()
	if progress, err := RebaseInProgress(worktree); err != nil || !progress {
		t.Fatalf("RebaseInProgress = %v, %v, want a rebase to recover from", progress, err)
	}
	return dir, worktree, before
}

// lockPath is where git keeps a worktree's lock of that name.
func lockPath(t *testing.T, worktree, name string) string {
	t.Helper()
	p := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "--git-path", name))
	if !filepath.IsAbs(p) {
		p = filepath.Join(worktree, p)
	}
	return p
}

func TestAbortRebaseClearsTheLockAKilledGitLeft(t *testing.T) {
	_, worktree, before := midRebaseWorktree(t)
	// What a git killed while writing the index leaves behind: with it there,
	// git rebase --abort and the checkout behind it both refuse.
	lock := lockPath(t, worktree, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := abortRebase(context.Background(), worktree, "owl/job-1", true)

	if err != nil {
		t.Fatalf("abortRebase after a killed git: %v", err)
	}
	if progress, err := RebaseInProgress(worktree); err != nil || progress {
		t.Errorf("RebaseInProgress = %v, %v, want none left", progress, err)
	}
	if _, err := os.Stat(lock); err == nil {
		t.Errorf("%s is still there after the abort", lock)
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != "owl/job-1" {
		t.Errorf("the worktree is on %q, want the branch it was put back on", got)
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where the branch was, %s", got, before)
	}
}

func TestAbortRebaseLeavesALockAloneWhenNothingWasKilled(t *testing.T) {
	_, worktree, _ := midRebaseWorktree(t)
	lock := lockPath(t, worktree, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	err := abortRebase(context.Background(), worktree, "owl/job-1", false)

	// A lock nobody said was stale is not Owl's to remove: the abort refuses
	// on it, and says so, rather than deleting what somebody may hold.
	if err == nil {
		t.Errorf("abortRebase = nil over a lock it was not told was stale, want the refusal reported")
	}
	if _, statErr := os.Stat(lock); statErr != nil {
		t.Errorf("the lock was removed although nothing said it was stale")
	}
}

func TestRebaseClearsAStaleLockBeforeItStarts(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "--quiet", "-b", "main")
	write(t, dir, "base.txt", "one\n")
	gitOut(t, dir, "add", "--", "base.txt")
	gitOut(t, dir, "commit", "-m", "one")
	worktree := filepath.Join(t.TempDir(), "job")
	if err := AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	write(t, worktree, "work.txt", "the agent's work\n")
	gitOut(t, worktree, "add", "--", "work.txt")
	gitOut(t, worktree, "commit", "-m", "the agent's work")
	write(t, dir, "base.txt", "two\n")
	gitOut(t, dir, "commit", "-am", "the base moves")
	// What an Agent's git ended by SIGKILL in the middle of a commit leaves
	// (ADR-0034). Nothing holds it: the Agent is gone.
	lock := lockPath(t, worktree, "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	cleared, err := ClearStaleLocks(worktree)
	if err != nil {
		t.Fatalf("ClearStaleLocks: %v", err)
	}
	conflict, rebaseErr := Rebase(context.Background(), worktree, "main")

	if len(cleared) != 1 || cleared[0] != lock {
		t.Errorf("ClearStaleLocks cleared %v, want %s", cleared, lock)
	}
	if rebaseErr != nil || conflict.Conflicted() {
		t.Fatalf("Rebase = %+v, %v, want it to go through once the lock is gone", conflict, rebaseErr)
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "show", "HEAD:base.txt")); got != "two" {
		t.Errorf("base.txt on the rebased branch = %q, want the base's new content", got)
	}
}

func TestRebaseKilledMidWayLeavesNoRebaseAndNoLock(t *testing.T) {
	dir := t.TempDir()
	gitOut(t, dir, "init", "--quiet", "-b", "main")
	write(t, dir, ".gitattributes", "marked.txt filter=owlslow\n")
	gitOut(t, dir, "add", "--", ".gitattributes")
	gitOut(t, dir, "commit", "-m", "one")
	worktree := filepath.Join(t.TempDir(), "job")
	if err := AddWorktree(dir, worktree, "owl/job-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// A commit the filter is slow over, and a later one taking it away, so
	// git meets the filter while replaying and nowhere else.
	write(t, worktree, "marked.txt", "slow-me\n")
	gitOut(t, worktree, "add", "--", "marked.txt")
	gitOut(t, worktree, "commit", "-m", "a commit the filter is slow over")
	gitOut(t, worktree, "rm", "-q", "--", "marked.txt")
	gitOut(t, worktree, "commit", "-m", "and one that takes it away")
	before := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD"))
	write(t, dir, "from-base.txt", "moved on\n")
	gitOut(t, dir, "add", "--", "from-base.txt")
	gitOut(t, dir, "commit", "-m", "the base moves")
	filter := filepath.Join(t.TempDir(), "filter.sh")
	if err := os.WriteFile(filter, []byte("#!/bin/sh\nt=$(mktemp)\ncat > \"$t\"\n"+
		"if grep -q slow-me \"$t\"; then sleep 10; fi\ncat \"$t\"\nrm -f \"$t\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "config", "filter.owlslow.clean", "cat")
	gitOut(t, dir, "config", "filter.owlslow.smudge", filter)
	gitOut(t, dir, "config", "filter.owlslow.required", "true")
	// Killed while the filter is sleeping: what a rebase that outstays its
	// deadline, or a daemon stopping, does to git.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := Rebase(ctx, worktree, "main")

	if err == nil {
		t.Fatal("Rebase = nil after git was killed, want the interruption reported")
	}
	if progress, err := RebaseInProgress(worktree); err != nil || progress {
		t.Errorf("RebaseInProgress = %v, %v, want no rebase left behind", progress, err)
	}
	if _, err := os.Stat(lockPath(t, worktree, "index.lock")); err == nil {
		t.Errorf("the lock the killed git held is still there")
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "--abbrev-ref", "HEAD")); got != "owl/job-1" {
		t.Errorf("the worktree is on %q, want the job's branch", got)
	}
	if got := strings.TrimSpace(gitOut(t, worktree, "rev-parse", "HEAD")); got != before {
		t.Errorf("the worktree is at %s, want where it was, %s", got, before)
	}
}
