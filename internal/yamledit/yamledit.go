// Package yamledit edits one key of a YAML file somebody else wrote.
//
// Owl owns a handful of settings inside files that are the user's: a
// Project's `skills` list, the daemon's `claudePath`. Rewriting such a file
// from a struct would put a diff in front of its owner over every line they
// wrote and Owl did not touch - their comments, their paragraphs, their
// indentation. So the file is parsed into a node tree, one key of that tree is
// changed, and the tree is written back.
//
// yaml.v3 keeps comments through that round trip and drops blank lines, so
// blank lines are carried across as a comment nothing else would write and
// turned back afterwards. When that would change anything but a blank line -
// inside a block scalar a blank line is part of a value, and a value is not
// Owl's to edit - they are left to be dropped rather than risking the value.
package yamledit

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is a YAML file to edit in place.
type File struct {
	// Path is the file. It is written only when the edit changed something.
	Path string
	// Start is what to edit when the file is not there yet. An empty Start
	// makes an empty mapping.
	Start string
	// DirMode and FileMode are what the file and the directories above it are
	// made with. Zero is 0o755 and 0o644, which is what a file inside a
	// repository wants; a file under the config home carrying anything
	// private wants 0o700 and 0o600.
	DirMode  fs.FileMode
	FileMode fs.FileMode
	// Guard refuses a path before anything is written to it, and is how a
	// caller says what it will not write through in its own words. The
	// default refuses a symlink.
	Guard func(path string) error
}

// Edit applies fn to the mapping at the top of the file and writes the result.
// A file that comes back the same is left alone entirely, so that an edit
// which changed nothing does not show up as a modification.
func (f File) Edit(fn func(root *yaml.Node) error) error {
	guard := f.Guard
	if guard == nil {
		guard = NotThroughALink
	}
	if err := guard(f.Path); err != nil {
		return err
	}
	data, err := os.ReadFile(f.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = []byte(f.Start)
	case err != nil:
		return err
	}
	held, blanks := holdBlankLines(data)
	var doc yaml.Node
	if err := yaml.Unmarshal(held, &doc); err != nil {
		return fmt.Errorf("%s: %w", f.Path, err)
	}
	root := Mapping(&doc)
	if root == nil {
		return fmt.Errorf("%s is not a configuration file Owl can edit", f.Path)
	}
	if err := fn(root); err != nil {
		return err
	}
	rendered, err := render(&doc)
	if err != nil {
		return err
	}
	out := blanks.free(rendered)
	if string(out) == string(data) {
		return nil
	}
	dirMode, fileMode := f.DirMode, f.FileMode
	if dirMode == 0 {
		dirMode = 0o755
	}
	if fileMode == 0 {
		fileMode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), dirMode); err != nil {
		return err
	}
	return os.WriteFile(f.Path, out, fileMode)
}

// SetScalar sets one key of the file to a string value.
func (f File) SetScalar(key, value string) error {
	return f.Edit(func(root *yaml.Node) error {
		SetKey(root, key, Scalar(value))
		return nil
	})
}

// NotThroughALink refuses a path that is a symlink: writing through a link is
// writing wherever the link says, and these files are found by discovery in
// directories Owl did not make.
func NotThroughALink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink, and Owl writes these files where they stand", path)
	}
	return nil
}

// Mapping is the mapping a document holds, and nil for anything else.
func Mapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	// An empty document parses to nothing at all, which is the mapping a first
	// key goes into.
	if doc.Kind == 0 {
		doc.Kind = yaml.MappingNode
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

// SetKey sets one key of a mapping, replacing its value where it is already
// there and appending it where it is not.
func SetKey(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, Scalar(key), value)
}

// RemoveKey takes a key out of a mapping, and does not mind one that is not
// there.
func RemoveKey(mapping *yaml.Node, key string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

// Scalar is a string value.
func Scalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// Bool is a true or false value.
func Bool(value bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(value)}
}

// blankLineMarker stands in for a blank line while the document is a tree.
const blankLineMarker = "#owl-kept-this-line-blank"

// blankLines is whether a document's blank lines are being carried through the
// round trip as markers.
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
