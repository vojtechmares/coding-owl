package skill

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/yamledit"
)

// Service manages a Project's Skills: what it declares, what that resolved to,
// and where the fetched content is.
type Service struct {
	cache *Cache
}

// NewService returns a Service fetching into cache.
func NewService(cache *Cache) *Service { return &Service{cache: cache} }

// Cache is where fetched Skills are kept.
func (s *Service) Cache() *Cache { return s.cache }

// Declared is what a Project's configuration declares, as this package's own
// shape.
func Declare(cfg config.Config) []Declared {
	out := make([]Declared, 0, len(cfg.Skills))
	for _, s := range cfg.Skills {
		out = append(out, Declared{Source: s.Source, Ref: s.Ref, AutoUpdate: s.AutoUpdate})
	}
	return out
}

// Prepare resolves and fetches the Skills a Project declares, ready to be
// placed. A Skill the lock records is fetched at the commit recorded there; one
// the lock does not know, and one that may move on its own, is resolved afresh.
//
// The Lock it returns is what the Run actually used, which is what gets
// recorded against it: the manifest says what was asked for, and this says what
// answered.
func (s *Service) Prepare(ctx context.Context, declared []Declared, locked Lock) ([]Resolved, Lock, error) {
	used := Lock{Skills: map[string]Locked{}}
	out := make([]Resolved, 0, len(declared))
	for _, d := range declared {
		if err := ctx.Err(); err != nil {
			return nil, Lock{}, err
		}
		name := d.Name()
		if err := CheckName(name); err != nil {
			return nil, Lock{}, err
		}
		commit, digest := "", ""
		// Pinned: the lock is the answer, and the source is not asked again
		// (ADR-0024) - but only while the lock is an answer to the question the
		// manifest is asking. A ref somebody deliberately changed is a new
		// question, and answering the old one would record the new ref against
		// the old commit.
		if was, ok := locked.Skills[name]; ok && was.Source == d.Source &&
			sameRef(was.Ref, d.Ref) && !d.AutoUpdate {
			commit, digest = was.Commit, was.Digest
		}
		if commit == "" {
			if !d.AutoUpdate {
				// Resolving it here would make a Skill nobody declared to move
				// move at the start of every Run, and a Run cannot write the
				// lockfile back: it reads the base branch, which is the user's
				// to commit (ADR-0014). Pinning by default means saying so
				// rather than floating (ADR-0024).
				return nil, Lock{}, unlocked(name, d, locked)
			}
			resolved, err := s.cache.Resolve(d.Source, d.Ref)
			if err != nil {
				return nil, Lock{}, err
			}
			commit = resolved
			// A Skill that moved has content of its own; the digest the lock
			// held was of the commit it replaced.
			if was, ok := locked.Skills[name]; ok && was.Commit == commit {
				digest = was.Digest
			}
		}
		got, err := s.cache.Fetch(name, d.Source, d.Ref, commit, digest)
		if err != nil {
			return nil, Lock{}, err
		}
		out = append(out, got)
		used.Skills[name] = got.Locked
	}
	return out, used, nil
}

// unlocked is what to tell a user whose lockfile does not answer for a Skill
// that is not declared to move on its own.
func unlocked(name string, d Declared, locked Lock) error {
	if was, ok := locked.Skills[name]; ok {
		return invalid(
			"the lockfile records %s from %s at %s, and this project asks for %s at %s; "+
				"run `owl skills update %s` and commit the lockfile",
			name, was.Source, orDefaultBranch(was.Ref), d.Source, orDefaultBranch(d.Ref), name)
	}
	return invalid(
		"the lockfile records nothing for %s, and a skill runs at the commit the lockfile records; "+
			"run `owl skills update` and commit the lockfile", name)
}

// orDefaultBranch names a ref as a message does, including the empty one.
func orDefaultBranch(ref string) string {
	if ref == "" {
		return "its source's default branch"
	}
	return ref
}

// sameRef reports whether a locked ref answers the manifest's. An empty
// declared ref means the source's default branch, which is what the lock
// records the name of: the two agree unless the manifest names something else.
func sameRef(locked, declared string) bool {
	return declared == "" || locked == declared
}

// Add resolves a source at a ref, fetches it, and returns what to record. It
// does not write anything: where a Project's files live is the caller's to
// know.
func (s *Service) Add(ctx context.Context, source, ref string, autoUpdate bool) (Resolved, Declared, error) {
	if err := ctx.Err(); err != nil {
		return Resolved{}, Declared{}, err
	}
	d := Declared{Source: strings.TrimSpace(source), Ref: strings.TrimSpace(ref), AutoUpdate: autoUpdate}
	parsed, err := ParseSource(d.Source)
	if err != nil {
		return Resolved{}, Declared{}, err
	}
	if strings.HasPrefix(d.Ref, "-") {
		return Resolved{}, Declared{}, invalid("the ref %q may not start with a dash", d.Ref)
	}
	if err := CheckText("ref", d.Ref); err != nil {
		return Resolved{}, Declared{}, err
	}
	if err := CheckName(parsed.Name()); err != nil {
		return Resolved{}, Declared{}, err
	}
	commit, err := s.cache.Resolve(d.Source, d.Ref)
	if err != nil {
		return Resolved{}, Declared{}, err
	}
	got, err := s.cache.Fetch(parsed.Name(), d.Source, d.Ref, commit, "")
	if err != nil {
		return Resolved{}, Declared{}, err
	}
	// The ref is recorded as it was asked for. An empty one means the source's
	// default branch, and the manifest records which branch that is rather
	// than "HEAD", so that the file says what is actually being followed.
	if d.Ref == "" {
		branch, err := s.cache.DefaultBranch(d.Source)
		if err != nil {
			return Resolved{}, Declared{}, err
		}
		d.Ref = branch
	}
	got.Ref = d.Ref
	return got, d, nil
}

// ReadLock reads a lockfile from disk. A file that is not there is an empty
// lock, which is what a Project that has never had a Skill has.
func ReadLock(path string) (Lock, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Lock{Skills: map[string]Locked{}}, nil
	}
	if err != nil {
		return Lock{}, err
	}
	return ParseLock(path, data)
}

// WriteLock writes a lockfile, replacing what was there.
func WriteLock(path string, lock Lock) error {
	if err := notThroughALink(path); err != nil {
		return err
	}
	data, err := lock.Render()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteManifest writes the `skills` section of a configuration file, leaving
// everything else in it exactly as it was - values, comments and blank lines.
// The file is the user's, and Owl edits one key of it.
func WriteManifest(path string, declared []Declared) error {
	file := yamledit.File{
		Path: path,
		// A Project with no configuration file gets one carrying the
		// apiVersion and its skills.
		Start: "apiVersion: " + APIVersion + "\n",
		Guard: notThroughALink,
	}
	return file.Edit(func(root *yaml.Node) error {
		if len(declared) == 0 {
			yamledit.RemoveKey(root, "skills")
		} else {
			yamledit.SetKey(root, "skills", skillsNode(declared))
		}
		return nil
	})
}

// notThroughALink refuses a path that is a symlink. Both of these files are
// found by discovery inside a repository an Agent works in, and writing
// through a link there is writing wherever the link says.
func notThroughALink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return invalid("%s is a symlink, and Owl writes a project's own files where they stand", path)
	}
	return nil
}

// skillsNode is the `skills` value: one entry per Skill, in the order they were
// declared, so a file two commands wrote reads the way they were typed.
func skillsNode(declared []Declared) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, d := range declared {
		entry := &yaml.Node{Kind: yaml.MappingNode}
		yamledit.SetKey(entry, "git", yamledit.Scalar(d.Source))
		if d.Ref != "" {
			yamledit.SetKey(entry, "ref", yamledit.Scalar(d.Ref))
		}
		if d.AutoUpdate {
			yamledit.SetKey(entry, "auto_update", yamledit.Bool(true))
		}
		seq.Content = append(seq.Content, entry)
	}
	return seq
}

// Names is every Skill in a list of declarations, in order.
func Names(declared []Declared) []string {
	out := make([]string, 0, len(declared))
	for _, d := range declared {
		out = append(out, d.Name())
	}
	sort.Strings(out)
	return out
}
