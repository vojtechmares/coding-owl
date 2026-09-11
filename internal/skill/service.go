package skill

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vojtechmares/coding-owl/internal/config"
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
// everything else in it exactly as it was. The file is the user's, and Owl
// edits one key of it.
func WriteManifest(path string, declared []Declared) error {
	if err := notThroughALink(path); err != nil {
		return err
	}
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
	held, blanks := holdBlankLines(data)
	if err := yaml.Unmarshal(held, &doc); err != nil {
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
	rendered, err := render(&doc)
	if err != nil {
		return err
	}
	out := blanks.free(rendered)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if string(out) == string(data) {
		// Nothing about the file changed, so it is left exactly as it is:
		// rewriting it would put a diff in front of the user over lines they
		// did not touch.
		return nil
	}
	return os.WriteFile(path, out, 0o644)
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

// blankLineMarker stands in for a blank line while the document is a tree.
// yaml.v3 keeps comments and drops blank lines, and a configuration file a
// person wrote and has to commit should come back with its paragraphs.
const blankLineMarker = "#owl-kept-this-line-blank"

// blankLines is whether a document's blank lines are being carried through the
// round trip as markers. They are not, whenever carrying them would change
// anything but a blank line: inside a block scalar a blank line is part of a
// value, and a value is not Owl's to edit.
type blankLines struct{ held bool }

// holdBlankLines returns the source to parse, and whether the markers are in
// it. A document that already carries the marker is left alone: freeing it
// afterwards would blank a line the user wrote.
func holdBlankLines(data []byte) ([]byte, blankLines) {
	if bytes.Contains(data, []byte(blankLineMarker)) {
		return data, blankLines{}
	}
	marked := []byte(markBlankLines(string(data)))
	if !sameValues(data, marked) {
		return data, blankLines{}
	}
	return marked, blankLines{held: true}
}

// markBlankLines turns every blank line into a comment nothing else would
// write. The last element is the empty string after the final newline, which
// is not a line at all.
func markBlankLines(in string) string {
	lines := strings.Split(in, "\n")
	for i, ln := range lines[:max(len(lines)-1, 0)] {
		if strings.TrimSpace(ln) == "" {
			lines[i] = blankLineMarker
		}
	}
	return strings.Join(lines, "\n")
}

// free turns the markers back into blank lines, wherever the encoder indented
// them to.
func (b blankLines) free(rendered []byte) []byte {
	if !b.held {
		return rendered
	}
	lines := strings.Split(string(rendered), "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) == blankLineMarker {
			lines[i] = ""
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// sameValues reports whether two documents say the same thing. Comments and
// blank lines are not values, so a marker that changed one is a marker that
// landed inside a value.
func sameValues(a, b []byte) bool {
	var x, y any
	if err := yaml.Unmarshal(a, &x); err != nil {
		return false
	}
	if err := yaml.Unmarshal(b, &y); err != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// render writes a document back out the way it was written: yaml.Marshal
// indents sequences four spaces, and a configuration file a person wrote and
// has to commit should not be reindented by Owl editing one key of it.
func render(doc *yaml.Node) ([]byte, error) {
	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
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
