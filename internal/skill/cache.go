package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/vojtechmares/coding-owl/internal/git"
)

// Cache holds the fetched Skills, each under its own content digest
// (ADR-0033). The same content fetched twice is one directory, and a Skill
// whose content changed is another, so what an Agent read is always knowable.
type Cache struct {
	dir string
	// fetching serialises fetches, so two Runs starting at once cannot both be
	// halfway through writing the same digest, and mirroring serialises the
	// bare mirrors for the same reason. One daemon owns the cache, so a lock in
	// this process is the whole story.
	fetching  sync.Mutex
	mirroring sync.Mutex
}

// NewCache returns a Cache under dir, which is made when the first Skill is
// fetched.
func NewCache(dir string) *Cache { return &Cache{dir: dir} }

// Dir is where the cache lives.
func (c *Cache) Dir() string { return c.dir }

// Resolve reads what a ref points at in a source. A remote source is mirrored
// into the cache first, and brought up to date: what a ref points at is the
// source's to say, and the answer has to be today's.
func (c *Cache) Resolve(source, ref string) (string, error) {
	from, err := c.at(source)
	if err != nil {
		return "", err
	}
	if ref == "" {
		ref = "HEAD"
	}
	commit, err := git.ResolveRef(from, ref)
	if err != nil {
		return "", &InvalidError{Err: err}
	}
	return commit, nil
}

// DefaultBranch is the branch a source's HEAD points at, which is what a Skill
// added with no ref follows.
func (c *Cache) DefaultBranch(source string) (string, error) {
	from, err := c.at(source)
	if err != nil {
		return "", err
	}
	branch, err := git.DefaultBranch(from)
	if err != nil {
		return "", &InvalidError{Err: err}
	}
	return branch, nil
}

// at is the repository on this machine to read a source out of: the source
// itself when it is a local path, and a mirror of it in the cache when it is
// not. The mirror is fetched every time, so that a ref resolves to what it
// points at now.
func (c *Cache) at(source string) (string, error) {
	s, err := ParseSource(source)
	if err != nil {
		return "", err
	}
	if s.Local {
		return s.URL, nil
	}
	c.mirroring.Lock()
	defer c.mirroring.Unlock()
	mirror := filepath.Join(c.dir, sourcesDir, s.mirrorName())
	if err := os.MkdirAll(filepath.Dir(mirror), dirMode); err != nil {
		return "", fmt.Errorf("creating the skill source cache: %w", err)
	}
	if err := git.Mirror(mirror, s.URL); err != nil {
		return "", &InvalidError{Err: fmt.Errorf("fetching the skill source %s: %w", s.describe(), err)}
	}
	return mirror, nil
}

// sourcesDir holds one bare mirror per remote source, under the cache.
const sourcesDir = "sources"

// Fetch puts a source's commit in the cache and returns it, resolved. A commit
// already cached under the digest recorded for it is not fetched again.
//
// want is the digest the lockfile records, and is empty when there is none yet.
// A fetch whose content does not match a digest that was recorded is refused:
// the lockfile is what says which content ran, and content that has changed
// under a commit is not a thing Owl quietly accepts.
func (c *Cache) Fetch(name, source, ref, commit, want string) (Resolved, error) {
	if err := CheckName(name); err != nil {
		return Resolved{}, err
	}
	c.fetching.Lock()
	defer c.fetching.Unlock()

	if want != "" {
		if path, ok := c.held(want); ok {
			// What is in the cache is checked rather than taken on trust. An
			// Agent runs as the same user and reaches the cache through the
			// link in its worktree, so "the directory is named after the
			// digest" is not the same as "its content digests to that"
			// (ADR-0024).
			if err := c.verify(path, want); err == nil {
				return Resolved{
					Locked: Locked{Name: name, Source: source, Ref: ref, Commit: commit, Digest: want},
					Path:   path,
				}, nil
			}
			// Something changed it, so the fetch below replaces it with the
			// content that really is that digest.
		}
	}
	staged, err := os.MkdirTemp(c.dir, ".fetching-")
	if err != nil {
		if err := os.MkdirAll(c.dir, dirMode); err != nil {
			return Resolved{}, fmt.Errorf("creating the skills cache: %w", err)
		}
		if staged, err = os.MkdirTemp(c.dir, ".fetching-"); err != nil {
			return Resolved{}, err
		}
	}
	defer func() { _ = os.RemoveAll(staged) }()

	from, err := c.at(source)
	if err != nil {
		return Resolved{}, err
	}
	if err := git.ExportCommit(from, commit, staged); err != nil {
		return Resolved{}, &InvalidError{Err: err}
	}
	if err := CheckSkill(staged, source); err != nil {
		return Resolved{}, err
	}
	digest, err := Digest(staged)
	if err != nil {
		return Resolved{}, err
	}
	if want != "" && digest != want {
		return Resolved{}, invalid(
			"skill %s at %s is %s, but the lockfile records %s; the source changed what that commit holds",
			name, commit, digest, want)
	}
	path, err := c.keep(staged, digest)
	if err != nil {
		return Resolved{}, err
	}
	// keep leaves a directory that was already there, which is the same
	// content by definition - unless something changed it since.
	if err := c.verify(path, digest); err != nil {
		return Resolved{}, err
	}
	return Resolved{
		Locked: Locked{Name: name, Source: source, Ref: ref, Commit: commit, Digest: digest},
		Path:   path,
	}, nil
}

// verify reports whether what is at a path really is that digest. It is what
// turns the cache from a name into a claim anybody can check.
func (c *Cache) verify(path, digest string) error {
	got, err := Digest(path)
	if err != nil {
		return err
	}
	if got != digest {
		return invalid("the cached skill at %s is %s, not the %s it is filed under; something changed it",
			path, got, digest)
	}
	return nil
}

// held reports whether the cache already holds that digest.
func (c *Cache) held(digest string) (string, bool) {
	path := c.pathFor(digest)
	if path == "" {
		return "", false
	}
	info, err := os.Stat(path)
	return path, err == nil && info.IsDir()
}

// keep moves a staged fetch into its place under its digest. A directory
// already there holds the same content by definition - unless something
// changed it, in which case what was fetched replaces it: the cache is filed
// by content, and a directory that is not its own content is not a cache
// entry.
//
// The entry is not made read-only. An Agent runs as the same user and could
// undo that anyway, it would stop the user and garbage collection reclaiming
// the cache, and what actually defends the property ADR-0024 asks for is
// checking the digest of what is read, which every use does.
func (c *Cache) keep(staged, digest string) (string, error) {
	path := c.pathFor(digest)
	if path == "" {
		return "", fmt.Errorf("the digest %q is not one Owl can make a directory of", digest)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return "", err
	}
	if alreadyThere(path) {
		if err := c.verify(path, digest); err == nil {
			return path, nil
		}
		if err := os.RemoveAll(path); err != nil {
			return "", err
		}
	}
	err := os.Rename(staged, path)
	if errors.Is(err, fs.ErrExist) || (err != nil && alreadyThere(path)) {
		// Another fetch of the same content got there first, which is the same
		// directory either way.
		return path, nil
	}
	return path, err
}

// pathFor is where a digest lives in the cache, and is empty for a digest that
// could not be a directory name.
func (c *Cache) pathFor(digest string) string {
	algorithm, hex, ok := splitDigest(digest)
	if !ok {
		return ""
	}
	return filepath.Join(c.dir, algorithm, hex)
}

func alreadyThere(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
