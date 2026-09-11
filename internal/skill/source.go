package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Source is where a Skill comes from, resolved into something git can fetch
// from: the ecosystem's shorthands, adopted whole (ADR-0033).
type Source struct {
	// Declared is what the manifest says, unchanged, so that messages name
	// what the user wrote.
	Declared string
	// URL is what git is given: a local path, or a remote to clone.
	URL string
	// Local is whether it is a directory on this machine, which is fetched
	// from where it stands rather than mirrored.
	Local bool
}

// shorthandRE is the `owner/repo` form: two path elements and nothing else.
var shorthandRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// scpRE is the `git@host:owner/repo` form git accepts, which is not a URL.
var scpRE = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:[^\s]+$`)

// schemes are the URL schemes Owl fetches over. Anything else - `ext::`
// especially, which runs a command git is told to trust - is refused: a Skill
// source is named in a file an Agent could have written (ADR-0024).
var schemes = []string{"https://", "http://", "ssh://", "git://", "file://"}

// defaultHost is where an `owner/repo` shorthand comes from, which is what the
// ecosystem's own CLI assumes.
const defaultHost = "https://github.com/"

// ParseSource reads a declared source. It refuses what git would read as
// something other than a repository: a relative path, which would resolve
// against the daemon's own directory rather than anybody's, and a scheme that
// makes git run a program.
func ParseSource(declared string) (Source, error) {
	s := strings.TrimSpace(declared)
	if s == "" {
		return Source{}, invalid("a skill needs a source: a repository to fetch it from")
	}
	switch {
	case filepath.IsAbs(s):
		return Source{Declared: declared, URL: s, Local: true}, nil
	case shorthandRE.MatchString(s):
		return Source{Declared: declared, URL: defaultHost + s + ".git"}, nil
	case scpRE.MatchString(s):
		return Source{Declared: declared, URL: s}, nil
	case hasScheme(s, schemes):
		return Source{Declared: declared, URL: s}, nil
	case strings.Contains(s, "://"):
		return Source{}, invalid(
			"%s is not a source Owl fetches from; it fetches over %s, and over ssh as user@host:path",
			declared, strings.Join(trimmed(schemes), ", "))
	case strings.HasPrefix(s, "."), strings.Contains(s, "/"):
		// A relative path would resolve against the daemon's own working
		// directory, which is nobody's: under launchd it is the root.
		return Source{}, invalid(
			"%s is a relative path, and a skill source is fetched by the daemon rather than from where you typed; "+
				"give an absolute path, a git URL, or an owner/repo", declared)
	default:
		return Source{}, invalid(
			"%s is not a source Owl recognises; it takes owner/repo, a git URL, or an absolute path", declared)
	}
}

// Name is the Skill name a source yields: the last path element, without a
// `.git` suffix.
func (s Source) Name() string { return NameOf(s.URL) }

// mirrorName is where a remote source is mirrored in the cache: a digest of
// the URL, so that two sources cannot collide and a URL of any shape becomes a
// directory name.
func (s Source) mirrorName() string {
	sum := sha256.Sum256([]byte(s.URL))
	return hex.EncodeToString(sum[:])
}

func hasScheme(s string, schemes []string) bool {
	for _, scheme := range schemes {
		if strings.HasPrefix(s, scheme) {
			return true
		}
	}
	return false
}

// trimmed is the schemes without their separators, for a message a person
// reads.
func trimmed(schemes []string) []string {
	out := make([]string, 0, len(schemes))
	for _, scheme := range schemes {
		out = append(out, strings.TrimSuffix(scheme, "://"))
	}
	return out
}

// describe is a source as a message names it.
func (s Source) describe() string {
	if s.Declared != "" && s.Declared != s.URL {
		return fmt.Sprintf("%s (%s)", s.Declared, s.URL)
	}
	return s.URL
}
