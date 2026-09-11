package skill

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only apiVersion a lockfile may carry. It is the
// configuration's own, so the two files are read by the same rules.
const APIVersion = "codingowl.dev/v1"

// Lock is what a lockfile records: the identity of every Skill a Project
// declares. `skills` is a section rather than the whole file, so that Driver
// versions or container images can be locked later without a second lockfile
// (ADR-0033).
type Lock struct {
	// Skills are the locked Skills, by name.
	Skills map[string]Locked
}

// lockFile is the on-disk shape.
type lockFile struct {
	APIVersion string               `yaml:"apiVersion"`
	Skills     map[string]lockEntry `yaml:"skills"`
}

// lockEntry is one Skill's identity on disk. The name is the key, so it is not
// repeated in the value.
type lockEntry struct {
	Source string `yaml:"source"`
	Ref    string `yaml:"ref"`
	Commit string `yaml:"commit"`
	Digest string `yaml:"digest"`
}

// ParseLock reads a lockfile. source names the file in error messages.
func ParseLock(source string, data []byte) (Lock, error) {
	var f lockFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Lock{}, fmt.Errorf("%s: %w", source, err)
	}
	if f.APIVersion == "" {
		return Lock{}, fmt.Errorf("%s: apiVersion is missing; expected %q", source, APIVersion)
	}
	if f.APIVersion != APIVersion {
		return Lock{}, fmt.Errorf("%s: apiVersion %q is not recognised; expected %q", source, f.APIVersion, APIVersion)
	}
	out := Lock{Skills: map[string]Locked{}}
	for name, e := range f.Skills {
		if err := CheckName(name); err != nil {
			return Lock{}, fmt.Errorf("%s: %w", source, err)
		}
		for _, field := range []struct{ what, value string }{
			{"source", e.Source},
			{"commit", e.Commit},
			{"digest", e.Digest},
		} {
			if strings.TrimSpace(field.value) == "" {
				return Lock{}, fmt.Errorf("%s: skill %q records no %s", source, name, field.what)
			}
		}
		if _, _, ok := splitDigest(e.Digest); !ok {
			return Lock{}, fmt.Errorf("%s: skill %q records the digest %q, which Owl did not write",
				source, name, e.Digest)
		}
		// The commit reaches git as an argument and is printed back to the
		// user, so anything that is not an object name is a lockfile Owl did
		// not write.
		if !commitRE.MatchString(e.Commit) {
			return Lock{}, fmt.Errorf("%s: skill %q records the commit %q, which is not an object name",
				source, name, e.Commit)
		}
		out.Skills[name] = Locked{
			Name: name, Source: e.Source, Ref: e.Ref, Commit: e.Commit, Digest: e.Digest,
		}
	}
	return out, nil
}

// commitRE is what a commit looks like: git's object names are hex, and the
// length varies only with the hash the repository uses.
var commitRE = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// Render writes a lockfile, in a fixed order so that two runs of the same
// commands produce the same file and a diff shows only what changed.
func (l Lock) Render() ([]byte, error) {
	f := lockFile{APIVersion: APIVersion, Skills: map[string]lockEntry{}}
	for name, s := range l.Skills {
		f.Skills[name] = lockEntry{Source: s.Source, Ref: s.Ref, Commit: s.Commit, Digest: s.Digest}
	}
	var out strings.Builder
	out.WriteString("# Written by owl skills. The manifest carries intent; this carries identity.\n")
	data, err := yaml.Marshal(f)
	if err != nil {
		return nil, err
	}
	out.Write(data)
	return []byte(out.String()), nil
}

// Names is every Skill the lock records, in order.
func (l Lock) Names() []string {
	out := make([]string, 0, len(l.Skills))
	for name := range l.Skills {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
