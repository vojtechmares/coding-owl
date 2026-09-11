package git

import (
	"fmt"
	"strconv"
	"strings"
)

// MinVersion is the oldest git Owl works with. Rebasing passes
// --no-update-refs, so that a user's rebase.updateRefs cannot make Owl carry
// their branches along with a Job's (ADR-0016), and git learned that flag in
// 2.38, in October 2022. An older git refuses the flag, and with it every
// Run after a Job's first.
var MinVersion = Version{Major: 2, Minor: 38}

// Version is a git release, as far as Owl needs to tell them apart.
type Version struct {
	Major, Minor int
}

func (v Version) String() string { return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) }

// Before reports whether v is older than o.
func (v Version) Before(o Version) bool {
	return v.Major < o.Major || (v.Major == o.Major && v.Minor < o.Minor)
}

// ParseVersion reads what git --version prints: "git version 2.55.0", or
// "git version 2.39.5 (Apple Git-154)" - the first two numbers are the ones
// that matter, and whatever follows them is the vendor's to say.
func ParseVersion(out string) (Version, error) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(out), "git version ")
	if !ok {
		return Version{}, fmt.Errorf("%q is not what git --version prints", strings.TrimSpace(out))
	}
	fields := strings.SplitN(strings.Fields(rest)[0], ".", 3)
	if len(fields) < 2 {
		return Version{}, fmt.Errorf("%q is not a git version", rest)
	}
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		return Version{}, fmt.Errorf("%q is not a git version", rest)
	}
	minor, err := strconv.Atoi(fields[1])
	if err != nil {
		return Version{}, fmt.Errorf("%q is not a git version", rest)
	}
	return Version{Major: major, Minor: minor}, nil
}

// Installed is the version of the git Owl runs.
func Installed() (Version, error) {
	out, stderr, code, err := run(".", "--version")
	if err != nil {
		return Version{}, err
	}
	if code != 0 {
		return Version{}, fmt.Errorf("git --version: %s", message(stderr))
	}
	return ParseVersion(string(out))
}

// CheckVersion refuses a git older than Owl works with, naming both versions
// and what the newer one is needed for, so a daemon that cannot rebase says
// so once at startup rather than at the second Run of every Job.
func CheckVersion() error {
	v, err := Installed()
	if err != nil {
		return fmt.Errorf("finding the installed git: %w", err)
	}
	if v.Before(MinVersion) {
		return fmt.Errorf("git %s is installed, and Owl needs %s or later: rebasing a job's branch passes --no-update-refs, which older versions refuse",
			v, MinVersion)
	}
	return nil
}
