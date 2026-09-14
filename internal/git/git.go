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
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
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

// BranchRef names a local branch as a ref written out in full, which is how a
// rebase is told what to replay onto when nothing was fetched (ADR-0016).
func BranchRef(branch string) string { return branchRef(branch) }

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

// HeadBranch is the branch a worktree has checked out, or empty when it is on
// no branch at all. An error means git could not be asked, which is not the
// same answer.
func HeadBranch(dir string) (string, error) {
	out, stderr, code, err := run(dir, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return "", err
	}
	switch {
	case code == 1:
		// Exactly one: symbolic-ref reports a ref that is not symbolic that
		// way, and keeps the higher statuses for being unable to answer.
		return "", nil
	case code != 0:
		return "", fmt.Errorf("reading what %s has checked out: %s", dir, message(stderr))
	}
	branch, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "refs/heads/")
	if !ok {
		return "", nil
	}
	return branch, nil
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

// blobID reads the object id out of the `ls-tree -z` record, which is
// `<mode> SP <type> SP <oid> TAB <name>`. It reports false when the record is
// absent or names anything but a blob. A literal, wildcard-free pathspec
// matches at most one entry, which GIT_LITERAL_PATHSPECS keeps true.
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

// AddWorktree creates a worktree at path with a new branch cut from the local
// base branch (ADR-0007). The repository's own checkout is untouched: a
// worktree is a second working tree, and the Job's branch is only ever checked
// out in this one.
func AddWorktree(dir, path, branch, base string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	// The base is addressed as a ref for the reason branchRef gives, and
	// --end-of-options keeps a name that begins with a dash out of option
	// position.
	_, stderr, code, err := run(dir, "worktree", "add", "-b", branch,
		"--end-of-options", path, branchRef(base))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("creating a worktree at %s on branch %s from %s in %s: %s",
			path, branch, base, dir, message(stderr))
	}
	return nil
}

// RemoveWorktree takes a Job's worktree back, leaving the branch alone. It
// refuses a worktree holding changes nobody has committed unless force says
// otherwise: reclaiming disk must not destroy work silently (ADR-0015).
func RemoveWorktree(dir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	_, stderr, code, err := run(dir, args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("removing the worktree at %s: %s", path, message(stderr))
	}
	return nil
}

// IsWorktree reports whether path is a worktree git can still work in. A
// directory that is gone, or that has lost the .git file linking it to its
// repository - what an interrupted removal leaves behind, and what git itself
// calls prunable - is not one.
func IsWorktree(path string) (bool, error) {
	// The .git file is checked first: without it, git would resolve upwards
	// and answer about whatever repository happens to be above the directory.
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	out, _, code, err := run(path, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, err
	}
	return code == 0 && strings.TrimSpace(string(out)) == "true", nil
}

// WorktreeBelongsTo reports whether a worktree is one of repo's: whether the
// repository it shares its objects and refs with is the one at repo. A
// directory at a Job's worktree path that git can work in is not enough - it
// could be anybody's checkout put there since - and replaying what is checked
// out in it would be rewriting a branch of some other repository.
func WorktreeBelongsTo(worktree, repo string) (bool, error) {
	mine, err := commonDir(worktree)
	if err != nil {
		return false, err
	}
	theirs, err := commonDir(repo)
	if err != nil {
		return false, err
	}
	return mine == theirs, nil
}

// commonDir is the directory a worktree shares with every other worktree of
// its repository, resolved, so two names for one place compare equal.
func commonDir(dir string) (string, error) {
	out, stderr, code, err := run(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("finding the repository of %s: %s", dir, message(stderr))
	}
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return filepath.Clean(common), nil
	}
	return resolved, nil
}

// WorktreeIsClean reports whether a worktree holds nothing uncommitted -
// neither a change to a tracked file nor a file git does not know about.
func WorktreeIsClean(path string) (bool, error) {
	out, stderr, code, err := run(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, fmt.Errorf("reading the state of %s: %s", path, message(stderr))
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// ResolveRef reads what a ref points at in a repository: the commit, so an
// annotated tag yields the commit it tags rather than the tag object. It is
// how a Skill's ref becomes the identity a lockfile records (ADR-0033).
func ResolveRef(dir, ref string) (string, error) {
	// ^{commit} peels a tag; --end-of-options keeps a ref that begins with a
	// dash out of option position; --verify refuses anything that resolves to
	// more than one thing.
	out, stderr, code, err := run(dir, "rev-parse", "--verify", "--quiet",
		"--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	if code != 0 {
		// rev-parse --quiet says nothing about a ref it could not read, so
		// whether the repository itself is readable is what tells the two
		// apart: a source nobody can read and a ref nobody has are different
		// mistakes.
		if _, _, root, rootErr := runRoot(dir); rootErr != nil || root != 0 {
			return "", fmt.Errorf("%s is not a git repository Owl can read: %s", dir, message(stderr))
		}
		return "", fmt.Errorf("%s has no ref %q", dir, ref)
	}
	return strings.TrimSpace(string(out)), nil
}

// runRoot asks a directory whether it is a repository at all, for the messages
// that have to tell an unreadable source from a ref it does not carry.
func runRoot(dir string) ([]byte, string, int, error) {
	return run(dir, "rev-parse", "--git-dir")
}

// Mirror keeps a bare copy of a remote repository at path, making it the first
// time and bringing it up to date afterwards. It is how Owl fetches a Skill
// source itself rather than shelling out to somebody else's CLI (ADR-0033).
//
// The protocols git may use are restricted to the ones Owl fetches over: a
// source is named in a configuration file, which an Agent could have written,
// and `ext::` in particular makes git run a command of the URL's choosing.
func Mirror(path, url string) error {
	if strings.HasPrefix(url, "-") {
		return fmt.Errorf("a skill source may not start with a dash: %q", url)
	}
	args := append(protocolLimits(), "clone", "--mirror", "--quiet", "--end-of-options", url, path)
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
		// Already mirrored: fetch into it rather than cloning again. --prune
		// so that a ref the source deleted stops resolving here.
		args = append(protocolLimits(), "fetch", "--quiet", "--prune", "--tags", "origin")
		_, stderr, code, err := fetching(path, args...)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("fetching %s: %s", url, message(stderr))
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// The clone runs in the directory above, since the one it makes is not
	// there yet.
	_, stderr, code, err := fetching(filepath.Dir(path), args...)
	if err != nil {
		// A clone that ran out of time leaves a half-made directory behind,
		// which would be taken for a mirror next time.
		_ = os.RemoveAll(path)
		return err
	}
	if code != 0 {
		// A clone that failed leaves a half-made directory, which would be
		// taken for a mirror next time.
		_ = os.RemoveAll(path)
		return fmt.Errorf("cloning %s: %s", url, message(stderr))
	}
	return nil
}

// protocolLimits are the `-c` settings every fetch of somebody else's
// repository carries: no transport that runs a program, and no prompting for
// credentials in a daemon nobody is watching.
func protocolLimits() []string {
	return []string{
		"-c", "protocol.ext.allow=never",
		"-c", "credential.interactive=never",
	}
}

// DefaultBranch is the branch a repository's HEAD points at, which is what a
// Skill added with no ref follows. It is read rather than assumed: a source's
// default branch is its own to choose, and writing `main` into a manifest that
// actually follows `trunk` would be a lie a user reads.
func DefaultBranch(dir string) (string, error) {
	out, stderr, code, err := run(dir, "ls-remote", "--symref", "--end-of-options", dir, "HEAD")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("asking %s which branch it is on: %s", dir, message(stderr))
	}
	for _, ln := range strings.Split(string(out), "\n") {
		rest, ok := strings.CutPrefix(ln, "ref: ")
		if !ok {
			continue
		}
		ref, _, _ := strings.Cut(rest, "\t")
		if branch, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/"); ok {
			return branch, nil
		}
	}
	return "", fmt.Errorf("%s does not say which branch it is on", dir)
}

// ExportCommit writes the tree of a commit into a directory, which is created.
// What lands there is the files as they stood, with no repository of their own:
// a Skill is content, not a checkout (ADR-0033).
func ExportCommit(dir, commit, into string) error {
	if err := os.MkdirAll(into, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", into, err)
	}
	// The archive is read as git writes it, so a tree nobody has vetted never
	// lands on disk whole: what it may hold is bounded as it arrives.
	cmd := exec.Command("git", "archive", "--format=tar", "--end-of-options", commit)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("reading %s out of %s: %w", commit, dir, err)
	}
	unpackErr := untar(out, into)
	// The reader is drained either way, so that git is never left writing into
	// a pipe nobody is reading.
	_, _ = io.Copy(io.Discard, out)
	waitErr := cmd.Wait()
	if unpackErr != nil {
		return unpackErr
	}
	if waitErr != nil {
		return fmt.Errorf("reading %s out of %s: %s", commit, dir, message(stderr.String()))
	}
	return nil
}

// BranchIsIn reports whether branch is contained in base: every commit on it
// is already on the base branch. That is what Owl reads as the user having
// merged a Job's work through their own git tooling, which is an implicit
// accept (ADR-0015).
//
// A branch that never carried a commit of its own is contained trivially,
// which is the right answer: there is nothing on it for the base branch to be
// missing.
func BranchIsIn(dir, branch, base string) (bool, error) {
	// Both are addressed as refs for the reason branchRef gives, and
	// --is-ancestor exits 0 for contained, 1 for not, and something else for a
	// ref it could not read - which is a question that was never answered
	// rather than an answer of no.
	_, stderr, code, err := run(dir, "merge-base", "--is-ancestor",
		"--end-of-options", branchRef(branch), branchRef(base))
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("asking whether %s is in %s in %s: %s",
			branch, base, dir, message(stderr))
	}
}

// WorktreePaths is every worktree git is counting for a repository, apart from
// the repository's own checkout. A path here is one git believes in, whether or
// not the directory is still there.
func WorktreePaths(dir string) ([]string, error) {
	out, stderr, code, err := run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("listing the worktrees of %s: %s", dir, message(stderr))
	}
	root, err := Root(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, ln := range strings.Split(string(out), "\n") {
		path, ok := strings.CutPrefix(ln, "worktree ")
		if !ok {
			continue
		}
		path = strings.TrimSpace(path)
		// The first entry is the repository's own checkout, which is not a
		// worktree anybody asked Owl to make.
		if path == "" || sameDir(path, root) {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// sameDir reports whether two paths name the same directory, following
// symlinks: /tmp and /private/tmp are the same place on macOS, and git answers
// with the resolved one.
func sameDir(a, b string) bool {
	return resolve(a) == resolve(b)
}

// resolve cleans a path and follows symlinks, falling back to the path itself
// when it cannot be resolved - a directory that is gone is still worth
// comparing by name.
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(path)
}

// EnableWorktreeConfig lets a repository carry configuration per worktree,
// which is what makes an exclude file settable for Owl's worktree alone
// (ADR-0033). It writes to the configuration of a repository Owl does not own,
// which is benign - it only enables the mechanism - and is done idempotently.
func EnableWorktreeConfig(dir string) error {
	on, err := worktreeConfigOn(dir)
	if err != nil {
		return err
	}
	if on {
		return nil
	}
	// Turning this on changes what three settings mean: git stops sharing
	// core.bare, core.worktree and core.sparseCheckout between worktrees, and
	// says they must be moved by hand. Only a value that does something is
	// worth stopping for - `git init` writes core.bare=false into every
	// ordinary repository, and that is the default either way.
	for _, setting := range []struct{ name, matters string }{
		{"core.bare", "true"},
		{"core.worktree", ""},
		{"core.sparseCheckout", "true"},
	} {
		out, _, code, err := run(dir, "config", "--get", setting.name)
		if err != nil {
			return err
		}
		if code != 0 {
			continue
		}
		value := strings.TrimSpace(string(out))
		if setting.matters != "" && value != setting.matters {
			continue
		}
		return fmt.Errorf(
			"%s sets %s to %s, which changes meaning once configuration is kept per worktree; "+
				"move it into the main worktree's own configuration and Owl will carry on",
			dir, setting.name, value)
	}
	_, stderr, code, err := run(dir, "config", "extensions.worktreeConfig", "true")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("enabling per-worktree configuration in %s: %s", dir, message(stderr))
	}
	return nil
}

// worktreeConfigOn reports whether a repository already keeps configuration per
// worktree, so that Owl writes to it once rather than on every Run.
func worktreeConfigOn(dir string) (bool, error) {
	out, _, code, err := run(dir, "config", "--get", "extensions.worktreeConfig")
	if err != nil {
		return false, err
	}
	return code == 0 && strings.TrimSpace(string(out)) == "true", nil
}

// SetWorktreeExcludes points one worktree at an exclude file of its own. It
// shadows whatever the repository was told globally, inside that worktree
// alone, which is why the caller has to carry the old setting forward
// (ADR-0033).
func SetWorktreeExcludes(worktree, excludes string) error {
	_, stderr, code, err := run(worktree, "config", "--worktree", "core.excludesFile", excludes)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("setting the exclude file of %s: %s", worktree, message(stderr))
	}
	return nil
}

// GlobalExcludes is the exclude file git would read for a repository, and is
// empty when there is none to read. It is what an Owl exclude file has to
// carry forward, or a user's global ignores silently stop applying (ADR-0033).
//
// It is the effective file, not only the configured one: git falls back to
// `$XDG_CONFIG_HOME/git/ignore`, and `git config --get` never reports that.
// A configured value may also begin with `~`, which git expands and a caller
// reading the file would not.
func GlobalExcludes(dir string) (string, error) {
	// --type=path is what expands a leading `~`, which git does for this
	// setting and a caller reading the file would not.
	out, _, code, err := run(dir, "config", "--type=path", "--get", "core.excludesFile")
	if err != nil {
		return "", err
	}
	if code == 0 {
		if path := strings.TrimSpace(string(out)); path != "" {
			return path, nil
		}
	}
	// Nothing configured, so git reads its own default - which applies until
	// the moment something sets core.excludesFile, as Owl is about to.
	return defaultExcludes(), nil
}

// maxExcludes is as much of an exclude file as Owl will carry forward. A
// repository says which file that is, and a repository is not always the
// user's own: without a bound, a `core.excludesFile` of `/dev/zero` is the
// daemon's memory.
const maxExcludes = 1 << 20

// ReadExcludes is what an exclude file holds, and is empty for one that is not
// there. Anything but an ordinary file is refused, and no more than
// maxExcludes bytes are read.
func ReadExcludes(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the exclude file this repository was told to use: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf(
			"the exclude file this repository was told to use, %s, is not an ordinary file", path)
	}
	if info.Size() > maxExcludes {
		return "", fmt.Errorf(
			"the exclude file this repository was told to use, %s, is %d bytes, and Owl carries forward at most %d",
			path, info.Size(), maxExcludes)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("reading the exclude file this repository was told to use: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxExcludes))
	if err != nil {
		return "", fmt.Errorf("reading the exclude file this repository was told to use: %w", err)
	}
	return string(data), nil
}

// defaultExcludes is the file git reads when core.excludesFile is unset, and is
// empty when there is none.
func defaultExcludes() string {
	var path string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(xdg) {
		path = filepath.Join(xdg, "git", "ignore")
	} else if home, err := os.UserHomeDir(); err == nil {
		path = filepath.Join(home, ".config", "git", "ignore")
	}
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// PruneWorktrees forgets the administrative files of worktrees whose
// directories are no longer there, so that git stops reporting a worktree
// nobody can use.
func PruneWorktrees(dir string) error {
	_, stderr, code, err := run(dir, "worktree", "prune")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("pruning the worktrees of %s: %s", dir, message(stderr))
	}
	return nil
}

// DeleteBranch deletes a local branch, whether or not it has been merged: a
// dropped Job's branch is one nobody wanted (ADR-0015).
func DeleteBranch(dir, branch string) error {
	// -D rather than -d: the point of dropping is to discard unmerged work.
	_, stderr, code, err := run(dir, "branch", "-D", "--", branch)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("deleting the branch %s in %s: %s", branch, dir, message(stderr))
	}
	return nil
}

// RebaseInProgress reports whether a worktree is in the middle of a rebase
// nobody finished. Such a worktree is not Owl's to take over (ADR-0016).
func RebaseInProgress(path string) (bool, error) {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, stderr, code, err := run(path, "rev-parse", "--git-path", name)
		if err != nil {
			return false, err
		}
		if code != 0 {
			return false, fmt.Errorf("reading the state of %s: %s", path, message(stderr))
		}
		dir := strings.TrimSpace(string(out))
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(path, dir)
		}
		if _, err := os.Stat(dir); err == nil {
			return true, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

// Conflict is what stood in the way of a rebase. The zero value is a rebase
// that went through.
type Conflict struct {
	// Paths are the files that could not be replayed, in order.
	Paths []string
	// InStash says the rebase itself went through and what conflicted was
	// putting back changes nobody had committed. Git keeps those in the
	// repository's stash rather than losing them, and the worktree is left
	// holding the conflict for somebody to resolve.
	InStash bool
}

// Conflicted reports whether anything stood in the way.
func (c Conflict) Conflicted() bool { return len(c.Paths) > 0 }

// Rebase replays the branch checked out in a worktree onto a ref, written out
// in full: the remote-tracking ref a fetch brought in, or the local base
// branch as BranchRef names it. A rebase that conflicts is aborted and the
// conflicting paths are reported, so that whoever asked can say what is in
// the way; the worktree is left exactly as it was (ADR-0016).
//
// Changes nobody committed are stashed and put back afterwards: an interrupted
// Run leaves a possibly untidy worktree behind (ADR-0011), and that is not a
// reason to leave a Job unrebased.
func Rebase(ctx context.Context, path, onto string) (Conflict, error) {
	ctx, cancel := context.WithTimeout(ctx, rebaseTimeout)
	defer cancel()

	// Read before anything moves: a rebase detaches HEAD, so this is the only
	// moment the branch to put the worktree back on can be asked for.
	onBranch, err := HeadBranch(path)
	if err != nil {
		return Conflict{}, err
	}
	// The identity is Owl's own: a rebase can need one, and it must not depend
	// on the user having configured one. What the branch is rebased onto is a
	// ref written out in full rather than a bare name, because a tag of the
	// same name would otherwise decide what the Job is rebased onto.
	// --no-update-refs: git will otherwise carry other branches along with the
	// commits it rewrites, and rebase.updateRefs is a setting a user may have
	// on globally - or an Agent may set in the repository the worktree shares.
	// Owl rebases its own Job branch and nothing else (ADR-0016).
	// --no-verify: the pre-rebase hook is somebody else's code deciding
	// whether this may happen, and this runs unattended, exactly as the commit
	// below does. Git's other hooks still run - they are told what happened
	// rather than asked.
	// commit.gpgsign=false: replaying Owl's own Job branch is not the user's
	// signature to give, and a signing program that wants a passphrase would
	// wait for somebody who is not there.
	args := append(append([]string{}, owlIdentity...),
		"-c", "commit.gpgsign=false",
		"rebase", "--no-update-refs", "--no-verify", "--autostash", "--", onto)
	_, stderr, code, err := runWithin(ctx, path, args...)
	if err != nil {
		// git was killed - by the deadline above, or by the daemon stopping -
		// and a killed git leaves what it was holding: the rebase itself, and
		// the lock on the index it was writing. Both are cleared, because the
		// next Run must not begin on top of either (ADR-0016). The abort runs
		// under its own deadline, whatever became of this one.
		if abortErr := abortRebase(ctx, path, onBranch, true); abortErr != nil {
			return Conflict{}, fmt.Errorf("%w; and putting the worktree back: %v", err, abortErr)
		}
		return Conflict{}, err
	}
	if code == 0 {
		// A rebase that went through can still leave the worktree in conflict:
		// git puts back what it stashed afterwards, says so, and exits zero -
		// which is why this asks rather than trusting the status.
		stashed, err := unmergedPaths(path)
		if err != nil {
			return Conflict{}, err
		}
		if len(stashed) > 0 {
			return Conflict{Paths: stashed, InStash: true}, nil
		}
		return Conflict{}, nil
	}
	// What conflicted is read first, because aborting is what clears it - but
	// a read that fails is not a reason to leave the rebase standing, so the
	// abort happens either way. Whatever stopped it, the worktree must not be
	// left in the middle of a rebase for the next Run to trip over.
	conflicts, listErr := unmergedPaths(path)
	abortErr := abortRebase(ctx, path, onBranch, false)
	if listErr != nil {
		return Conflict{}, listErr
	}
	if abortErr != nil {
		return Conflict{}, abortErr
	}
	// Anything that is not a conflict is the caller's to report as it is.
	if len(conflicts) == 0 {
		return Conflict{}, fmt.Errorf("rebasing %s onto %s: %s", path, onto, message(stderr))
	}
	return Conflict{Paths: conflicts}, nil
}

// abortRebase puts a worktree back where it was, when there is a rebase to
// abort at all. It runs whether or not what asked for the rebase is still
// waiting - that is the point of it - but it is bounded, because a filter or
// a hook the abort has to run through can hang as easily as the rebase could.
//
// killed says the git that was rebasing did not finish of its own accord. A
// killed git leaves the lock on the index it was writing, and every git after
// it - the abort, and the force-back behind it - refuses on that lock; so it
// is cleared first, as a lock nobody holds any more.
func abortRebase(ctx context.Context, path, branch string, killed bool) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	if killed {
		if _, err := ClearStaleLocks(path); err != nil {
			return err
		}
	}
	inProgress, err := RebaseInProgress(path)
	if err != nil || !inProgress {
		return err
	}
	_, stderr, code, err := runWithin(ctx, path, "rebase", "--abort")
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	// The abort's own reset can refuse - over a file the rebase wrote that
	// the index does not know about, say. A worktree nobody can use is worse
	// than files nobody asked to keep, so the rebase is dropped and the branch
	// put back by force.
	aborted := message(stderr)
	if branch == "" {
		return fmt.Errorf("aborting the rebase in %s: %s; it was on no branch to put back", path, aborted)
	}
	if err := forceBack(ctx, path, branch); err != nil {
		return fmt.Errorf("aborting the rebase in %s (%s), and putting it back: %w", path, aborted, err)
	}
	return fmt.Errorf("the rebase in %s could not be undone cleanly (%s), so %s was put back by force; anything that was not committed is in the repository's stash",
		path, aborted, branch)
}

// forceBack drops whatever rebase a worktree is in the middle of and puts it
// on branch again, discarding what stands in the way. It is the last resort of
// abortRebase: a worktree nobody can use is worse than files nobody asked to
// keep, and the autostash keeps what was not committed.
func forceBack(ctx context.Context, path, branch string) error {
	if _, stderr, code, err := runWithin(ctx, path, "rebase", "--quit"); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("dropping the rebase: %s", message(stderr))
	}
	// --end-of-options rather than --: after --, git reads the name as a path
	// and never as a branch, which is how a checkout meant to switch branches
	// quietly becomes one that restores a file of that name.
	_, stderr, code, err := runWithin(ctx, path, "checkout", "--force", "--end-of-options", branch, "--")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("checking out %s: %s", branch, message(stderr))
	}
	return nil
}

// staleLocks are the lock files a git that was killed leaves behind in a
// worktree: the one on its index, and the one on its HEAD.
var staleLocks = []string{"index.lock", "HEAD.lock"}

// ClearStaleLocks removes the locks a killed git left in a worktree, and says
// which. Nothing runs in a Job's worktree between Runs - the Agent is gone,
// and Owl does one thing in it at a time - so a lock found there before a
// rebase, or after the git that was rebasing was killed, is a lock nobody
// holds: an Agent's git ended by SIGKILL (ADR-0034) leaves one, and so does a
// rebase the daemon stopped in the middle of. Left alone, every git after it
// refuses, and the Job is blocked for good on a file nobody is using.
func ClearStaleLocks(path string) (cleared []string, err error) {
	for _, name := range staleLocks {
		out, stderr, code, err := run(path, "rev-parse", "--git-path", name)
		if err != nil {
			return cleared, err
		}
		if code != 0 {
			return cleared, fmt.Errorf("finding %s in %s: %s", name, path, message(stderr))
		}
		lock := strings.TrimSpace(string(out))
		if !filepath.IsAbs(lock) {
			lock = filepath.Join(path, lock)
		}
		err = os.Remove(lock)
		switch {
		case err == nil:
			cleared = append(cleared, lock)
		case errors.Is(err, fs.ErrNotExist):
		default:
			return cleared, err
		}
	}
	return cleared, nil
}

// unmergedPaths are the paths a rebase left with conflicts to resolve.
func unmergedPaths(path string) ([]string, error) {
	// -z: a path with a space or a byte outside ASCII would otherwise arrive
	// in git's quoted form, and these are read rather than parsed.
	out, stderr, code, err := run(path, "diff", "--name-only", "-z", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("reading the conflicts in %s: %s", path, message(stderr))
	}
	var paths []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			paths = append(paths, name)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}

// FetchBase updates what the repository knows about base from the remote it
// belongs to - the branch's own remote when it has one, and origin otherwise.
// A repository with no remote has nothing to fetch, which is not a failure.
// The fetch is bounded: a daemon must not wait on a remote for ever.
func FetchBase(ctx context.Context, dir, base string) (ref string, err error) {
	remote, err := remoteFor(dir, base)
	if err != nil || remote == "" {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	// The refspec is written out in full: only the remote-tracking branch is
	// updated, nothing the user has is moved, and neither side can be read as
	// a tag of the same name.
	ref = fmt.Sprintf("refs/remotes/%s/%s", remote, base)
	refspec := "+" + branchRef(base) + ":" + ref
	// protocol.ext.allow=never: a remote URL is configuration, and an ext::
	// one is a command for git to run. It is not the only way a repository can
	// name a program - core.sshCommand and uploadpack are others - but it is
	// the one that needs no transport of its own.
	_, stderr, code, err := runWithin(ctx, dir,
		"-c", "protocol.ext.allow=never", "fetch", "--quiet", "--", remote, refspec)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("fetching %s from %s: %s", base, remote, message(stderr))
	}
	// What was fetched is what a rebase should be onto: the local branch is
	// the user's and is never moved, so it says nothing about what the remote
	// knows (ADR-0016).
	return ref, nil
}

// rebaseTimeout bounds a rebase. Replaying a short-lived branch is quick; this
// only has to be long enough for a large one and short enough that a daemon
// stopping is not held up by one.
const rebaseTimeout = 2 * time.Minute

// fetchTimeout bounds a fetch. A remote that is slow or gone is not a reason to
// leave a Job unstarted, so this only has to be short enough to notice.
const fetchTimeout = 2 * time.Minute

// remoteFor names the remote a branch belongs to: the one it tracks, or origin
// when it tracks none and the repository has an origin. Empty means there is
// nothing to fetch from.
func remoteFor(dir, branch string) (string, error) {
	out, _, code, err := run(dir, "config", "--get", "branch."+branch+".remote")
	if err != nil {
		return "", err
	}
	if code == 0 {
		if remote := strings.TrimSpace(string(out)); remote != "" {
			return remote, nil
		}
	}
	out, _, code, err = run(dir, "remote")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", nil
	}
	for _, name := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(name) == defaultRemote {
			return defaultRemote, nil
		}
	}
	return "", nil
}

// defaultRemote is where a branch that tracks nothing is fetched from.
const defaultRemote = "origin"

// owlIdentity is who Owl commits as. It commits only the handoff, and only
// when the Agent left it uncommitted, so the identity is Owl's own rather than
// the user's - and it does not depend on the user having configured one.
var owlIdentity = []string{
	"-c", "user.name=Coding Owl",
	"-c", "user.email=owl@coding-owl.invalid",
}

// CommitPath commits one path in a worktree, if it has anything to commit.
// committed is false when the path was already committed as it stands, which
// is the ordinary case for an Agent that commits its own work (ADR-0017).
func CommitPath(ctx context.Context, dir, path, msg string) (committed bool, err error) {
	_, stderr, code, err := runWithin(ctx, dir, "add", "--", path)
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, fmt.Errorf("staging %s in %s: %s", path, dir, message(stderr))
	}
	// --quiet exits 1 when something is staged, which is the question here.
	_, stderr, code, err = runWithin(ctx, dir, "diff", "--cached", "--quiet", "--", path)
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return false, nil
	case 1:
	default:
		return false, fmt.Errorf("looking for changes to %s in %s: %s", path, dir, message(stderr))
	}
	// --no-verify: the hooks in this repository are the Agent's to write, and
	// Owl's bookkeeping commit is not the place to run them.
	// commit.gpgsign=false: this is Owl's own commit, not the user's signature
	// to give, and a signing program that wants a passphrase would wait for
	// somebody who is not there - exactly as for the rebase above. Turning
	// signing off covers a key of any format, gpg or ssh.
	args := append(append([]string{}, owlIdentity...),
		"-c", "commit.gpgsign=false",
		"commit", "--no-verify", "-m", msg, "--", path)
	_, stderr, code, err = runWithin(ctx, dir, args...)
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, fmt.Errorf("committing %s in %s: %s", path, dir, message(stderr))
	}
	return true, nil
}

// run executes git in dir and reports its exit status. err is returned only
// when git could not be run at all - a missing directory, a missing binary -
// which is a different thing from git running and saying no.
func run(dir string, args ...string) (stdout []byte, stderr string, code int, err error) {
	return runWithin(context.Background(), dir, args...)
}

// FetchTimeout bounds a git command that talks to somebody else's server. The
// daemon runs Jobs one at a time and nobody is watching it, so a source that
// accepts a connection and then says nothing must end as a failure rather than
// as a queue that never moves again. It is a variable so that a test can lower
// it: what is worth testing is that there is a deadline at all.
var FetchTimeout = 10 * time.Minute

// runWithin is run under a deadline of the caller's choosing.
func runWithin(ctx context.Context, dir string, args ...string) (stdout []byte, stderr string, code int, err error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// A cancelled git is killed, but something it started can hold the pipes
	// open - a filter driver, a hook, ssh - and waiting on those would never
	// return. After this, what is still holding them is let go of.
	cmd.WaitDelay = killDelay
	// A C locale keeps git's diagnostics in one language. Nothing below parses
	// them, but they end up in messages users read. The redirection variables
	// are dropped because they override the repository chosen by cmd.Dir: a
	// daemon started from inside a hook would otherwise read every Project out
	// of whatever repository its environment happened to name. Literal
	// pathspecs settle the rest of that family at once - a candidate path is
	// a path, never a glob and never case-insensitive, so a tree cannot match
	// one of them twice.
	cmd.Env = gitEnv()
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.Bytes(), errb.String(), 0, nil
	case ctx.Err() != nil:
		// git was killed because ctx ended, and a killed git is not git
		// saying no: it was stopped in the middle of something, and may have
		// left that something behind. The caller is told the difference.
		return nil, errb.String(), -1, fmt.Errorf("running git %s in %s: %w", strings.Join(args, " "), dir, ctx.Err())
	case errors.As(err, &exitErr):
		return out.Bytes(), errb.String(), exitErr.ExitCode(), nil
	case errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil:
		// git itself finished; what it started was still holding the pipes
		// open, and has been let go of. Its status is the answer.
		return out.Bytes(), errb.String(), cmd.ProcessState.ExitCode(), nil
	default:
		return nil, errb.String(), -1, fmt.Errorf("running git %s in %s: %w", strings.Join(args, " "), dir, err)
	}
}

// fetching runs a git command that talks to somebody else's server, under the
// deadline such a command needs.
func fetching(dir string, args ...string) (stdout []byte, stderr string, code int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), FetchTimeout)
	defer cancel()
	return runWithin(ctx, dir, args...)
}

// abortTimeout bounds putting a worktree back after a rebase that stopped.
const abortTimeout = 30 * time.Second

// killDelay is how long git has to finish after it is killed before whatever
// it started is stopped waiting for.
const killDelay = 5 * time.Second

// gitRedirection are the environment variables that move git away from the
// directory it was pointed at, or change how the paths it is given are read.
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
	// The pathspec settings are global and mutually exclusive, so leaving one
	// set would make git refuse the literal setting below rather than obey it.
	"GIT_ICASE_PATHSPECS",
	"GIT_GLOB_PATHSPECS",
	"GIT_NOGLOB_PATHSPECS",
	"GIT_LITERAL_PATHSPECS",
}

// gitEnv is the environment every git Owl runs gets.
func gitEnv() []string {
	return append(withoutGitRedirection(os.Environ()),
		"LC_ALL=C", "LANG=C", "GIT_LITERAL_PATHSPECS=1",
		// Nobody is watching a daemon, so git asks nothing: a fetch that needs
		// a password fails rather than waiting for one that never comes.
		"GIT_TERMINAL_PROMPT=0")
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

// DiffFile is one file a branch changed against its base. A binary file is
// reported with no lines.
type DiffFile struct {
	Path                  string
	Insertions, Deletions int
}

// DiffSummary is what a branch changed against its base, per file and in
// total.
type DiffSummary struct {
	Files                 []DiffFile
	Insertions, Deletions int
}

// DiffPatch is what branch changed since it left base, as a patch, cut at max
// bytes. complete is false for a patch that did not fit, so a caller can say
// so rather than passing off half a diff as the whole of one.
//
// The comparison is from the merge base, like DiffStat: what landed on base
// since the branch left it is not the branch's doing.
func DiffPatch(dir, base, branch string, max int) (patch string, complete bool, err error) {
	// No external diff driver and no textconv: what a repository's own
	// .gitattributes asks for is a program to run, and this diff is read by a
	// daemon nobody is watching rather than by the person who wrote that file.
	out, stderr, code, err := run(dir, "diff", "--no-color", "--no-ext-diff", "--no-textconv",
		"--end-of-options", branchRef(base)+"..."+branchRef(branch))
	if err != nil {
		return "", false, err
	}
	if code != 0 {
		return "", false, fmt.Errorf("diffing %s against %s in %s: %s", branch, base, dir, message(stderr))
	}
	if max > 0 && len(out) > max {
		// Cut at the last whole line, so what is carried is a diff as far as
		// it goes rather than one ending mid-rune or mid-hunk.
		cut := out[:max]
		if at := bytes.LastIndexByte(cut, '\n'); at >= 0 {
			cut = cut[:at+1]
		}
		return string(cut), false, nil
	}
	return string(out), true, nil
}

// DiffStat summarises what branch changed since it left base: the files it
// touched and the lines added and removed in each. Both are read as local
// branches, and the comparison is from their merge base, so what landed on
// base since is not counted against the branch.
func DiffStat(dir, base, branch string) (DiffSummary, error) {
	// -z ends each record with NUL, so a path with a newline or a tab in it
	// cannot split a record; --numstat is the machine-readable form.
	out, stderr, code, err := run(dir, "diff", "--numstat", "-z", "--end-of-options",
		branchRef(base)+"..."+branchRef(branch))
	if err != nil {
		return DiffSummary{}, err
	}
	if code != 0 {
		return DiffSummary{}, fmt.Errorf("diffing %s against %s in %s: %s", branch, base, dir, message(stderr))
	}
	return parseNumstat(out)
}

// parseNumstat reads `diff --numstat -z` output. A record is
// `<added> TAB <removed> TAB <path> NUL`, and a rename is
// `<added> TAB <removed> TAB NUL <old> NUL <new> NUL`; binary files carry a
// dash for both counts.
func parseNumstat(out []byte) (DiffSummary, error) {
	var d DiffSummary
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if len(rec) == 0 {
			continue
		}
		parts := bytes.SplitN(rec, []byte{'\t'}, 3)
		if len(parts) != 3 {
			return DiffSummary{}, fmt.Errorf("unreadable numstat record %q", rec)
		}
		f := DiffFile{Path: string(parts[2])}
		if f.Path == "" {
			// A rename: the old and new names follow as records of their
			// own, and the new name is the one to report.
			if i+2 >= len(fields) || len(fields[i+1]) == 0 || len(fields[i+2]) == 0 {
				return DiffSummary{}, fmt.Errorf("truncated rename in numstat output")
			}
			f.Path = string(fields[i+2])
			i += 2
		}
		var err error
		if f.Insertions, err = numstatCount(parts[0]); err != nil {
			return DiffSummary{}, err
		}
		if f.Deletions, err = numstatCount(parts[1]); err != nil {
			return DiffSummary{}, err
		}
		d.Files = append(d.Files, f)
		d.Insertions += f.Insertions
		d.Deletions += f.Deletions
	}
	return d, nil
}

// numstatCount reads one count, where a dash means a binary file.
func numstatCount(b []byte) (int, error) {
	if string(b) == "-" {
		return 0, nil
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return 0, fmt.Errorf("unreadable numstat count %q", b)
	}
	return n, nil
}
