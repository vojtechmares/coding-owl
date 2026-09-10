// Package git is the small slice of git Owl needs to read a Project: where a
// repository starts, which branches it has, and what a file looks like on a
// given branch. Reads are always from a ref, never from a working tree
// (ADR-0014).
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Root returns the root of the repository containing dir, with symlinks
// resolved. It fails when dir is not inside a git repository.
func Root(dir string) (string, error) {
	out, _, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	root := strings.TrimSpace(string(out))
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return root, nil
	}
	return resolved, nil
}

// CurrentBranch returns the branch HEAD points at in dir. It fails on a
// detached HEAD, where there is no branch to name.
func CurrentBranch(dir string) (string, error) {
	out, _, err := run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%s is not on a branch; pass --base-branch", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasBranch reports whether dir has a local branch of that name.
func HasBranch(dir, branch string) (bool, error) {
	_, stderr, err := run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	// --quiet turns "no such ref" into a silent exit 1; anything on stderr is
	// a real failure worth reporting.
	if strings.TrimSpace(stderr) != "" {
		return false, fmt.Errorf("checking branch %s in %s: %s", branch, dir, strings.TrimSpace(stderr))
	}
	return false, nil
}

// ShowFile returns the contents of path as it stands on ref. found is false
// when ref carries no blob at that path, which includes the path naming a
// directory. It fails when ref itself cannot be resolved.
func ShowFile(dir, ref, path string) (data []byte, found bool, err error) {
	out, stderr, err := run(dir, "cat-file", "blob", ref+":"+path)
	if err == nil {
		return out, true, nil
	}
	msg := strings.TrimSpace(stderr)
	// git distinguishes a path that is not in the tree, a path that is there
	// but is not a blob ("bad file"), and a ref it cannot resolve. Only the
	// last is an error for us.
	for _, missing := range []string{"does not exist in", "but not in", "bad file"} {
		if strings.Contains(msg, missing) {
			return nil, false, nil
		}
	}
	return nil, false, fmt.Errorf("reading %s:%s in %s: %s", ref, path, dir, msg)
}

// run executes git in dir and returns its stdout and stderr.
func run(dir string, args ...string) (stdout []byte, stderr string, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return out.Bytes(), errb.String(), err
}
