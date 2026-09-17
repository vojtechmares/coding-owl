package behavior_test

// Behavior tests for issue #66. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-66.md; S1 to S5 live with the git package, whose own
// API they exercise. These drive the built owl binary against a daemon on a
// Project whose repository asks git to stop mentioning untracked files, with
// the stub agent of issue #5 standing in for Claude Code.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hidesUntracked asks a Project's repository to stop mentioning untracked
// files, which is what made a worktree holding nothing else read as clean.
func hidesUntracked(r *repo) {
	r.git("config", "status.showUntrackedFiles", "no")
}

func TestS6AcceptRefusesUntrackedWorkTheProjectHides(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, job, worktree, _ := reviewed(t, l)
	hidesUntracked(r)
	stray := filepath.Join(worktree, "stray.txt")
	if err := os.WriteFile(stray, []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "jobs", "accept", job)

	if res.code == 0 {
		t.Fatalf("accept threw away untracked work the project hides\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "commit") {
		t.Errorf("stderr does not say the worktree has uncommitted changes:\n%s", res.stderr)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the untracked file is gone after a refused accept: %v", err)
	}
}

func TestS7DropRefusesUntrackedWorkTheProjectHides(t *testing.T) {
	l, _ := committingLayout(t)
	daemonUp(t, l)
	r, job, worktree, _ := reviewed(t, l)
	hidesUntracked(r)
	stray := filepath.Join(worktree, "stray.txt")
	if err := os.WriteFile(stray, []byte("not committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := runOwl(t, l, "jobs", "drop", job)

	if res.code == 0 {
		t.Fatalf("drop threw away untracked work the project hides\nstdout:\n%s", res.stdout)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("the worktree is gone after a refused drop: %v", err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("the untracked file is gone after a refused drop: %v", err)
	}
}

func TestS8GCReportsAWorktreeHoldingUntrackedWorkTheProjectHides(t *testing.T) {
	l, _ := gcLayout(t)
	daemonUp(t, l)
	r, _, worktree := disposedWithItsWorktreeBack(t, l, "accept")
	hidesUntracked(r)
	stray := filepath.Join(worktree, "not-committed.txt")
	if err := os.WriteFile(stray, []byte("unfinished\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := gcReport(t, l)

	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("garbage collection destroyed work the project only hid: %v", err)
	}
	unfinished := section(t, out, "unfinished work:")
	if !strings.Contains(unfinished, worktree) {
		t.Errorf("owl gc does not report the worktree as unfinished work:\n%s", out)
	}
	if !strings.Contains(unfinished, "committed") {
		t.Errorf("owl gc does not say what is unfinished about it:\n%s", out)
	}
}
