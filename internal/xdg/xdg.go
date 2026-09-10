// Package xdg resolves Coding Owl's directories and socket path following the
// XDG base directory layout recorded in ADR-0014.
package xdg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// appDir is the per-application directory name under each XDG base.
const appDir = "coding-owl"

// socketName is the daemon's unix socket file name.
const socketName = "owld.sock"

// maxSocketPath is the size of sun_path on macOS, including the trailing NUL.
// Linux allows 108; the smaller limit is used everywhere so a layout that
// works on one platform works on the other.
const maxSocketPath = 104

// Paths is the resolved filesystem layout.
type Paths struct {
	// ConfigDir holds daemon settings and per-Project fallback configuration.
	ConfigDir string
	// DataDir holds the database, worktrees, Account directories and skills.
	DataDir string
	// StateDir holds the socket (unless XDG_RUNTIME_DIR is set) and Run logs.
	StateDir string
	// SocketPath is where the daemon listens.
	SocketPath string
}

// Resolve reads the layout from the process environment.
func Resolve() (Paths, error) {
	return ResolveFrom(os.Getenv)
}

// ResolveFrom resolves the layout using getenv for HOME and the XDG variables.
func ResolveFrom(getenv func(string) string) (Paths, error) {
	home := getenv("HOME")
	if home == "" {
		return Paths{}, errors.New("HOME is not set")
	}
	// The XDG spec says a relative value is invalid and must be ignored.
	abs := func(envVar string) string {
		if v := getenv(envVar); filepath.IsAbs(v) {
			return v
		}
		return ""
	}
	base := func(envVar, fallback string) string {
		if v := abs(envVar); v != "" {
			return filepath.Join(v, appDir)
		}
		return filepath.Join(home, fallback, appDir)
	}
	p := Paths{
		ConfigDir: base("XDG_CONFIG_HOME", ".config"),
		DataDir:   base("XDG_DATA_HOME", filepath.Join(".local", "share")),
		StateDir:  base("XDG_STATE_HOME", filepath.Join(".local", "state")),
	}
	if rt := abs("XDG_RUNTIME_DIR"); rt != "" {
		p.SocketPath = filepath.Join(rt, appDir, socketName)
	} else {
		p.SocketPath = filepath.Join(p.StateDir, socketName)
	}
	return p, nil
}

// CheckSocketPath reports an error when path cannot fit in a unix socket
// address on the platforms Owl targets.
func CheckSocketPath(path string) error {
	if len(path)+1 > maxSocketPath {
		return fmt.Errorf("socket path too long (%d bytes, max %d): %s", len(path), maxSocketPath-1, path)
	}
	return nil
}
