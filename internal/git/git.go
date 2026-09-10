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
	out, _, code, err := run(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("%s is not on a branch; pass --base-branch", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// HasBranch reports whether dir has a local branch of exactly that name.
// It verifies a ref rather than resolving a revision, so `main:path` and
// `main@{1}` are not branches.
func HasBranch(dir, branch string) (bool, error) {
	_, stderr, code, err := run(dir, "show-ref", "--verify", "--quiet", "--", "refs/heads/"+branch)
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

// ShowFile returns the contents of path as it stands on ref. found is false
// when ref carries no blob at that path, which includes the path naming a
// directory. It fails when ref itself cannot be resolved.
func ShowFile(dir, ref, path string) (data []byte, found bool, err error) {
	// --end-of-options keeps a ref that begins with a dash out of option
	// position; -z and --full-tree make the answer machine-readable.
	out, stderr, code, err := run(dir, "ls-tree", "-z", "--full-tree", "--end-of-options", ref, "--", path)
	if err != nil {
		return nil, false, err
	}
	if code != 0 {
		return nil, false, fmt.Errorf("reading %s:%s in %s: %s", ref, path, dir, message(stderr))
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
		return nil, false, fmt.Errorf("reading %s:%s in %s: %s", ref, path, dir, message(stderr))
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
	// them, but they end up in messages users read.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
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

// message trims git's diagnostic for embedding in an error, falling back to
// something rather than nothing when git said nothing at all.
func message(stderr string) string {
	if s := strings.TrimSpace(stderr); s != "" {
		return s
	}
	return "git failed without a message"
}
