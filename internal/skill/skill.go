// Package skill is the Skills a Project gives every Agent that works in it
// (ADR-0024). It resolves a source and a ref to a commit, fetches that commit
// into a content-addressed cache, and places it in the Driver's own skills
// directory inside a Job's worktree (ADR-0033).
//
// The manifest carries intent - a source and a ref - and the lockfile carries
// identity: the commit that ref resolved to and the digest of what was fetched.
// Owl fetches in Go rather than shelling out to the ecosystem's CLI: the daemon
// runs under launchd, where the user's interactive PATH does not apply, and
// that CLI's update semantics contradict pinning.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/config"
)

// FileName is the file a Skill must carry, with `name` and `description` in
// its frontmatter. It is the ecosystem's convention, adopted whole.
const FileName = "SKILL.md"

// LockName is the lockfile, discovered in the same places as a Project's
// configuration and read from the base branch (ADR-0033).
const LockName = ".coding-owl.lock.yaml"

// dirMode is what a cached Skill's directories are created with. A Skill is
// instructions an Agent reads, and nobody else's business.
const dirMode fs.FileMode = 0o700

// nameRE is what a Skill may be called. It becomes a directory inside a
// worktree, so it is kept to what every filesystem and every git reads the
// same way.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CheckText refuses a value that would print as something other than itself.
// A source and a ref come out of files a Project carries, which a merged pull
// request can change, and both are printed back to a terminal by `owl skills
// list` and `owl jobs show`.
func CheckText(what, value string) error {
	// The rule lives in internal/config, which reads the manifest these values
	// come from: this package reads that one's output, so the check goes there
	// and is borrowed here rather than written twice.
	if err := config.CheckText(what, value); err != nil {
		return &InvalidError{Err: err}
	}
	return nil
}

// InvalidError marks a failure the user can fix by asking for something
// different: a source that is not a repository, a ref it does not carry, a
// directory with no SKILL.md in it.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

func invalid(format string, a ...any) error {
	return &InvalidError{Err: fmt.Errorf(format, a...)}
}

// Declared is one Skill as a Project's configuration declares it: where it
// comes from, which ref it follows, and whether it may move on its own.
type Declared struct {
	// Source is the repository it comes from: a local path, a git URL, or the
	// `owner/repo` shorthand.
	Source string
	// Ref is the branch or tag it follows. Empty means the source's default
	// branch.
	Ref string
	// AutoUpdate is whether it is re-resolved at the start of every Run. It is
	// opt-in per Skill, because pinning is the default (ADR-0024).
	AutoUpdate bool
}

// Name is what the Skill is called, and what its directory inside a worktree
// is named: the last element of its source, without a `.git` suffix.
func (d Declared) Name() string { return NameOf(d.Source) }

// NameOf is the Skill name a source yields. `owner/repo`, a git URL and a
// local path all name the Skill after the repository.
func NameOf(source string) string {
	s := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(source), "/"), ".git")
	// A URL's last path element, which is also a path's last element.
	if i := strings.LastIndexAny(s, "/:"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// CheckName refuses a name Owl could not make a directory of.
func CheckName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return invalid("a skill needs a name, and its source yields none")
	case !nameRE.MatchString(name):
		return invalid("the skill name %q is not one Owl can use: it becomes a directory inside a worktree, "+
			"so it must start with a letter or a digit and hold only letters, digits, dots, dashes and underscores", name)
	}
	return nil
}

// Locked is one Skill as the lockfile records it: what its ref resolved to,
// and what was fetched.
type Locked struct {
	// Name identifies the Skill, and is the key of the lockfile entry.
	Name string
	// Source is where it came from, recorded so that a lockfile read without
	// its manifest still says what it is about.
	Source string
	// Ref is the ref it was resolved from.
	Ref string
	// Commit is what that ref resolved to.
	Commit string
	// Digest is of the content fetched at that commit, and is what names its
	// place in the cache.
	Digest string
}

// Resolved is a Skill ready to be placed: what the lockfile says about it, and
// where in the cache it is.
type Resolved struct {
	Locked
	// Path is the cached directory holding the Skill's files.
	Path string
}

// Digest is the content digest of a directory: every file's path and contents,
// hashed in a fixed order, so that the same tree always yields the same digest
// and any change yields another.
func Digest(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file, which a skill may not hold", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)

	sum := sha256.New()
	for _, rel := range paths {
		// The path and the length go in before the contents, so that two trees
		// cannot hash the same by moving a boundary between files.
		f, err := os.Open(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return "", err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", filepath.ToSlash(rel), info.Size())
		if _, err := io.Copy(sum, f); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

// CheckSkill refuses a directory that is not a Skill: the ecosystem's contract
// is a SKILL.md carrying a name and a description (ADR-0033).
func CheckSkill(dir, source string) error {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return invalid("%s carries no %s, so it is not a skill", source, FileName)
	}
	if err != nil {
		return err
	}
	front, ok := frontmatter(string(data))
	if !ok {
		return invalid("the %s in %s has no frontmatter, so it declares no name or description", FileName, source)
	}
	for _, field := range []string{"name", "description"} {
		if !hasField(front, field) {
			return invalid("the %s in %s declares no %s", FileName, source, field)
		}
	}
	return nil
}

// frontmatter is the block between the leading `---` fences of a SKILL.md.
func frontmatter(body string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimPrefix(body, "\ufeff"), "---\n")
	if !ok {
		return "", false
	}
	front, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", false
	}
	return front, true
}

// splitDigest reads a digest into the algorithm that made it and the hex it
// yielded, refusing anything that is not one of Owl's own: a digest names a
// directory in the cache, so it must be two plain words.
func splitDigest(digest string) (algorithm, hex string, ok bool) {
	algorithm, hex, ok = strings.Cut(digest, ":")
	if !ok || !digestPartRE.MatchString(algorithm) || !hexRE.MatchString(hex) {
		return "", "", false
	}
	return algorithm, hex, true
}

var (
	digestPartRE = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
	hexRE        = regexp.MustCompile(`^[0-9a-f]{32,128}$`)
)

// hasField reports whether a frontmatter block sets a field to something.
func hasField(front, field string) bool {
	for _, ln := range strings.Split(front, "\n") {
		value, ok := strings.CutPrefix(strings.TrimSpace(ln), field+":")
		if ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
