// Package credential keeps the secrets Owl holds out of its database. An
// Account's token lives in the OS keychain and `owl.db` records only a
// reference to it (ADR-0019), so a copy of the database is not a copy of the
// user's subscriptions.
//
// A reference is Owl's own name for one secret, like `account/work`. What it
// means on disk is the Store's business: an item in a keychain, or an entry in
// a file only its owner can read.
package credential

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// ErrNotFound is returned when a Store holds no secret under that reference.
var ErrNotFound = errors.New("no such credential")

// Store holds secrets under references.
type Store interface {
	// Name says where the secrets are kept, which the daemon logs at startup
	// so nobody has to guess.
	Name() string
	// Set writes a secret under ref, replacing one already there.
	Set(ctx context.Context, ref, secret string) error
	// Get reads one back, or ErrNotFound.
	Get(ctx context.Context, ref string) (string, error)
	// Delete removes one. Removing a secret that is not there is not a
	// failure: the caller wanted it gone, and it is.
	Delete(ctx context.Context, ref string) error
}

// Kind names where secrets are kept.
type Kind string

const (
	// KindKeychain is the OS keychain, which is where they belong (ADR-0019).
	KindKeychain Kind = "keychain"
	// KindFile is a file only its owner can read, for a platform with no
	// keychain Owl can drive.
	KindFile Kind = "file"
)

// Default is where secrets go when the daemon's configuration does not say.
// Owl is a macOS program (ADR-0002, ADR-0010); anywhere else there is no
// keychain it can drive, and a file the user alone can read is the honest
// second best.
func Default() Kind {
	if runtime.GOOS == "darwin" {
		return KindKeychain
	}
	return KindFile
}

// ParseKind reads the configured kind. An empty value is Default.
func ParseKind(s string) (Kind, error) {
	switch Kind(strings.TrimSpace(s)) {
	case "":
		return Default(), nil
	case KindKeychain:
		return KindKeychain, nil
	case KindFile:
		return KindFile, nil
	default:
		return "", fmt.Errorf("credentialStore %q is not one Owl has; it is %q or %q",
			s, KindKeychain, KindFile)
	}
}

// Open returns the Store of that kind. path is where a file store lives, and
// is ignored by the keychain.
func Open(kind Kind, path string) (Store, error) {
	switch kind {
	case KindKeychain:
		return openKeychain()
	case KindFile:
		return NewFile(path), nil
	default:
		return nil, fmt.Errorf("credentialStore %q is not one Owl has", kind)
	}
}

// checkRef refuses a reference a Store could not tell apart from another, or
// could not put in a command line: references are Owl's own, so anything odd
// is a bug rather than a user's mistake.
func checkRef(ref string) error {
	switch {
	case ref == "":
		return errors.New("a credential reference is empty")
	case strings.HasPrefix(ref, "-"):
		return fmt.Errorf("a credential reference may not start with a dash: %q", ref)
	case strings.ContainsAny(ref, "\x00\n"):
		return fmt.Errorf("a credential reference may not hold a newline or a null: %q", ref)
	}
	return nil
}
