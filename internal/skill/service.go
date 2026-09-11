package skill

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/git"
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
		if was, ok := locked.Skills[name]; ok && was.Source == d.Source && !d.AutoUpdate {
			// Pinned: the lock is the answer, and the source is not asked
			// again (ADR-0024).
			commit, digest = was.Commit, was.Digest
		}
		if commit == "" {
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

// Add resolves a source at a ref, fetches it, and returns what to record. It
// does not write anything: where a Project's files live is the caller's to
// know.
func (s *Service) Add(ctx context.Context, source, ref string, autoUpdate bool) (Resolved, Declared, error) {
	if err := ctx.Err(); err != nil {
		return Resolved{}, Declared{}, err
	}
	d := Declared{Source: strings.TrimSpace(source), Ref: strings.TrimSpace(ref), AutoUpdate: autoUpdate}
	if d.Source == "" {
		return Resolved{}, Declared{}, invalid("a skill needs a source: a repository to fetch it from")
	}
	if strings.HasPrefix(d.Ref, "-") {
		return Resolved{}, Declared{}, invalid("the ref %q may not start with a dash", d.Ref)
	}
	if err := CheckName(d.Name()); err != nil {
		return Resolved{}, Declared{}, err
	}
	commit, err := s.cache.Resolve(d.Source, d.Ref)
	if err != nil {
		return Resolved{}, Declared{}, err
	}
	got, err := s.cache.Fetch(d.Name(), d.Source, d.Ref, commit, "")
	if err != nil {
		return Resolved{}, Declared{}, err
	}
	// The ref is recorded as it was asked for. An empty one means the source's
	// default branch, and the manifest records which branch that is rather
	// than "HEAD", so that the file says what is actually being followed.
	if d.Ref == "" {
		branch, err := git.DefaultBranch(d.Source)
		if err != nil {
			return Resolved{}, Declared{}, &InvalidError{Err: err}
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
// everything else in it exactly as it was. The file is the user's, and Owl
// edits one key of it.
func WriteManifest(path string, declared []Declared) error {
	var doc yaml.Node
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// A Project with no configuration file gets one carrying the
		// apiVersion and its skills.
		data = []byte("apiVersion: " + APIVersion + "\n")
	case err != nil:
		return err
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	root := mappingOf(&doc)
	if root == nil {
		return fmt.Errorf("%s is not a configuration file Owl can edit", path)
	}
	if len(declared) == 0 {
		removeKey(root, "skills")
	} else {
		setKey(root, "skills", skillsNode(declared))
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// skillsNode is the `skills` value: one entry per Skill, in the order they were
// declared, so a file two commands wrote reads the way they were typed.
func skillsNode(declared []Declared) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode}
	for _, d := range declared {
		entry := &yaml.Node{Kind: yaml.MappingNode}
		setKey(entry, "git", scalar(d.Source))
		if d.Ref != "" {
			setKey(entry, "ref", scalar(d.Ref))
		}
		if d.AutoUpdate {
			setKey(entry, "auto_update", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
		}
		seq.Content = append(seq.Content, entry)
	}
	return seq
}

func scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// mappingOf is the mapping a document holds, and nil for anything else.
func mappingOf(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

// setKey sets one key of a mapping, replacing its value where it is already
// there and appending it where it is not.
func setKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalar(key), value)
}

// removeKey takes a key out of a mapping, and does not mind one that is not
// there.
func removeKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
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
