package behavior_test

// Behavior tests for issue #53. TestS3, TestS4 and TestS5 map to scenarios S3
// to S5 in tests/behavior/issue-53.md; S1 lives with the git package and S2
// with the run package, whose own APIs they exercise. These drive the built
// owl binary against a daemon on the rebasing Job of issue #12.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// tracking gives the rebasing Job's Project a bare repository to track, which
// somebody else then pushes a commit to. It returns that commit.
func tracking(t *testing.T, rb *rebasing) (pushed string) {
	t.Helper()
	bare := filepath.Join(rb.l.root, "origin.git")
	rb.repo.git("init", "--bare", bare)
	rb.repo.git("remote", "add", "origin", bare)
	rb.repo.git("push", "--quiet", "origin", "main")
	other := filepath.Join(rb.l.root, "other")
	rb.repo.git("clone", "--quiet", "--branch", "main", bare, other)
	gitIn(t, rb.repo, other, "-c", "user.name=Someone", "-c", "user.email=someone@example.com",
		"commit", "--allow-empty", "-m", "pushed by somebody else")
	gitIn(t, rb.repo, other, "push", "--quiet", "origin", "HEAD:refs/heads/main")
	return strings.TrimSpace(gitIn(t, rb.repo, other, "rev-parse", "HEAD"))
}

// ancestor reports whether one commit is an ancestor of another in the
// Project's repository. git answers with its exit status and says nothing
// either way, so the status is what is read.
func ancestor(t *testing.T, rb *rebasing, commit, of string) bool {
	t.Helper()
	cmd := exec.Command("git", "merge-base", "--is-ancestor", commit, of)
	cmd.Dir = rb.repo.dir
	cmd.Env = rb.repo.env
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false
	}
	if err != nil {
		t.Fatalf("git merge-base --is-ancestor %s %s: %v", commit, of, err)
	}
	return true
}

func TestS3ACommitPushedSinceTheCloneIsUnderTheJobBranchEndToEnd(t *testing.T) {
	rb := rebasingJob(t, nil)
	pushed := tracking(t, rb)
	local := strings.TrimSpace(rb.repo.git("rev-parse", "main"))

	mustOwl(t, rb.l, "start")

	if branch := rb.branch(t); !ancestor(t, rb, pushed, branch) {
		t.Errorf("the commit pushed since the clone, %s, is not under the job's branch %s", pushed, branch)
	}
	if got := strings.TrimSpace(rb.repo.git("rev-parse", "main")); got != local {
		t.Errorf("owl moved the project's own base branch to %s, want it left at %s", got, local)
	}
	rb.stub.let(t)
}

func TestS4AProjectWithNoRemoteRebasesOntoTheLocalBase(t *testing.T) {
	rb := rebasingJob(t, nil)
	if remotes := strings.TrimSpace(rb.repo.git("remote")); remotes != "" {
		t.Fatalf("the project has remotes: %q", remotes)
	}
	rb.moveBase(t, "from-base.txt", "added while the job was waiting\n")
	local := strings.TrimSpace(rb.repo.git("rev-parse", "main"))

	mustOwl(t, rb.l, "start")

	if branch := rb.branch(t); !ancestor(t, rb, local, branch) {
		t.Errorf("the local base's tip %s is not under the job's branch %s", local, branch)
	}
	if got := strings.TrimSpace(rb.repo.git("rev-parse", "main")); got != local {
		t.Errorf("owl moved the project's own base branch to %s, want it left at %s", got, local)
	}
	rb.stub.let(t)
}

func TestS5ADR0016NamesTheRefTheRebaseTargets(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoDir, "docs", "adr", "0016-rebase-job-branch-each-run.md"))
	if err != nil {
		t.Fatalf("reading ADR-0016: %v", err)
	}

	for _, want := range []string{"refs/remotes/<remote>/<base>", "refs/heads/<base>"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("ADR-0016 does not name %s as a ref the rebase targets", want)
		}
	}
}
