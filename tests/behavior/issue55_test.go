package behavior_test

// Behavior tests for issue #55. TestS4 maps to scenario S4 in
// tests/behavior/issue-55.md; S1 to S3 live with the git package, whose own
// API they exercise. This one drives the built owl binary against a daemon on
// a Project whose repository asks for every commit to be signed, with the stub
// agent leaving the handoff for Owl to commit.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS4APlanningRunCommitsTheHandoffInAProjectThatAsksForSignedCommits(t *testing.T) {
	l, _ := writingLayout(t, map[string]string{handoffPath: planText}, false, agentScript)
	daemonUp(t, l)
	r := plannedJob(t, l, "work")
	// The repository asks for every commit to be signed by a program that
	// records it was run and then fails, standing in for a signing tool that
	// would ask for a passphrase nobody is there to type.
	marker := filepath.Join(l.root, "signer-ran")
	signer := filepath.Join(l.root, "signer")
	if err := os.WriteFile(signer, []byte("#!/bin/sh\necho ran > "+marker+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("config", "commit.gpgsign", "true")
	r.git("config", "gpg.program", signer)

	_, job := phase(t, l)

	out := mustOwl(t, l, "jobs", "show", job).stdout
	branch := line(t, out, "branch")
	if got := committedHandoff(t, r, branch); got != planText {
		t.Errorf("the handoff on %s = %q, want the plan owl committed for it", branch, got)
	}
	if got := line(t, out, "state"); got != "pending" {
		t.Errorf("state = %q, want the job pending again for its execution run", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the signing program was run for owl's own commit of the handoff")
	}
	worktree := line(t, out, "worktree")
	if status := gitIn(t, r, worktree, "status", "--porcelain"); strings.Contains(status, "HANDOFF.md") {
		t.Errorf("the worktree still reports the handoff as uncommitted:\n%s", status)
	}
}
