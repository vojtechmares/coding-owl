// Package git is the small slice of git Owl needs to read a Project: where a
// repository starts, which branches it has, and what a file looks like on a
// given branch. Reads are always from a ref, never from a working tree
// (ADR-0014).
//
// Every decision here is made from an exit status or from machine-readable
// output. git's human-readable messages are translated, so they are reported
// but never parsed.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Root returns the root of the repository containing dir, with symlinks
// resolved. It fails when dir is not inside a git repository.
func Root(dir string) (string, error) {
	out, _, code, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	if code != 0 {
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
	// Not --short: it shortens to the shortest unambiguous name, so a tag
	// sharing the branch's name turns `main` into `heads/main`.
	out, _, code, err := run(dir, "symbolic-ref", "HEAD")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("%s is not on a branch; pass --base-branch", dir)
	}
	ref := strings.TrimSpace(string(out))
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok {
		return "", fmt.Errorf("%s has HEAD at %s, which is not a local branch; pass --base-branch", dir, ref)
	}
	return branch, nil
}

// branchRef is the full ref name of a local branch. Every read addresses a
// branch this way: git resolves a bare name against tags before heads, so a
// tag sharing a branch's name would otherwise shadow it - and pushing a tag
// is not the same permission as pushing a protected branch.
func branchRef(branch string) string { return "refs/heads/" + branch }

// HasBranch reports whether dir has a local branch of exactly that name.
// It verifies a ref rather than resolving a revision, so `main:path` and
// `main@{1}` are not branches.
func HasBranch(dir, branch string) (bool, error) {
	_, stderr, code, err := run(dir, "show-ref", "--verify", "--quiet", "--", branchRef(branch))
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("checking branch %s in %s: %s", branch, dir, message(stderr))
	}
}

// ShowFileOnBranch returns the contents of path as it stands on the local
// branch. found is false when the branch carries no blob at that path, which
// includes the path naming a directory. It fails when the branch cannot be
// resolved.
func ShowFileOnBranch(dir, branch, path string) (data []byte, found bool, err error) {
	ref := branchRef(branch)
	// --end-of-options keeps a ref that begins with a dash out of option
	// position; -z and --full-tree make the answer machine-readable.
	out, stderr, code, err := run(dir, "ls-tree", "-z", "--full-tree", "--end-of-options", ref, "--", path)
	if err != nil {
		return nil, false, err
	}
	if code != 0 {
		return nil, false, fmt.Errorf("reading %s:%s in %s: %s", branch, path, dir, message(stderr))
	}
	oid, ok := blobID(out)
	if !ok {
		return nil, false, nil
	}
	// The object id is what ls-tree just printed, so it is hex, not input.
	blob, stderr, code, err := run(dir, "cat-file", "blob", oid)
	if err != nil {
		return nil, false, err
	}
	if code != 0 {
		return nil, false, fmt.Errorf("reading %s:%s in %s: %s", branch, path, dir, message(stderr))
	}
	return blob, true, nil
}

// blobID reads the object id out of the first `ls-tree -z` record, which is
// `<mode> SP <type> SP <oid> TAB <name>`. It reports false when the record is
// absent or names anything but a blob.
func blobID(lsTree []byte) (string, bool) {
	record, _, _ := bytes.Cut(lsTree, []byte{0})
	head, _, ok := bytes.Cut(record, []byte{'\t'})
	if !ok {
		return "", false
	}
	fields := strings.Fields(string(head))
	if len(fields) != 3 || fields[1] != "blob" {
		return "", false
	}
	return fields[2], true
}

// run executes git in dir and reports its exit status. err is returned only
// when git could not be run at all - a missing directory, a missing binary -
// which is a different thing from git running and saying no.
func run(dir string, args ...string) (stdout []byte, stderr string, code int, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// A C locale keeps git's diagnostics in one language. Nothing below parses
	// them, but they end up in messages users read. The redirection variables
	// are dropped because they override the repository chosen by cmd.Dir: a
	// daemon started from inside a hook would otherwise read every Project out
	// of whatever repository its environment happened to name.
	cmd.Env = append(withoutGitRedirection(os.Environ()), "LC_ALL=C", "LANG=C")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.Bytes(), errb.String(), 0, nil
	case errors.As(err, &exitErr):
		return out.Bytes(), errb.String(), exitErr.ExitCode(), nil
	default:
		return nil, errb.String(), -1, fmt.Errorf("running git %s in %s: %w", args[0], dir, err)
	}
}

// gitRedirection are the environment variables that move git away from the
// directory it was pointed at.
var gitRedirection = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_INDEX_FILE",
	"GIT_NAMESPACE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_CEILING_DIRECTORIES",
	"GIT_DISCOVERY_ACROSS_FILESYSTEM",
}

// withoutGitRedirection returns env with those variables removed.
func withoutGitRedirection(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		if slices.Contains(gitRedirection, name) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// message trims git's diagnostic for embedding in an error, falling back to
// something rather than nothing when git said nothing at all.
func message(stderr string) string {
	if s := strings.TrimSpace(stderr); s != "" {
		return s
	}
	return "git failed without a message"
}
