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
	// halfway through writing the same digest. One daemon owns the cache, so a
	// lock in this process is the whole story.
	fetching sync.Mutex
}

// NewCache returns a Cache under dir, which is made when the first Skill is
// fetched.
func NewCache(dir string) *Cache { return &Cache{dir: dir} }

// Dir is where the cache lives.
func (c *Cache) Dir() string { return c.dir }

// Resolve reads what a ref points at in a source, without fetching anything.
func (c *Cache) Resolve(source, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	commit, err := git.ResolveRef(source, ref)
	if err != nil {
		return "", &InvalidError{Err: err}
	}
	return commit, nil
}

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
			return Resolved{
				Locked: Locked{Name: name, Source: source, Ref: ref, Commit: commit, Digest: want},
				Path:   path,
			}, nil
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

	if err := git.ExportCommit(source, commit, staged); err != nil {
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
	return Resolved{
		Locked: Locked{Name: name, Source: source, Ref: ref, Commit: commit, Digest: digest},
		Path:   path,
	}, nil
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

// keep moves a staged fetch into its place under its digest. A digest already
// there is the same content by definition, so the staged copy is discarded
// rather than replacing it.
func (c *Cache) keep(staged, digest string) (string, error) {
	path := c.pathFor(digest)
	if path == "" {
		return "", fmt.Errorf("the digest %q is not one Owl can make a directory of", digest)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return "", err
	}
	err := os.Rename(staged, path)
	if errors.Is(err, fs.ErrExist) {
		return path, nil
	}
	if err != nil && alreadyThere(path) {
		// Rename onto a directory that exists fails differently on different
		// systems; what matters is that the content is there.
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
