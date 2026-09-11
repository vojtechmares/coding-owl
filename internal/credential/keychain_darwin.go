package credential

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// service is the keychain service every Owl item is filed under, so that the
// user can find Owl's items among their own.
const service = "coding-owl"

// keychainTimeout bounds one `security` call. The daemon reads a credential
// before every Run and nobody is awake to answer a keychain that has wedged,
// so a call that does not return is a failure rather than a wait.
const keychainTimeout = 30 * time.Second

// notFoundStatus is what `security` exits with when the item is not there.
const notFoundStatus = 44

// Keychain keeps secrets in the OS keychain through the `security` command,
// which is the interface macOS gives a program that is not linking the
// Security framework.
//
// file names the keychain to use. Empty means the user's default keychain,
// which is what Owl uses; the tests name one of their own so that no test
// touches a real keychain.
type Keychain struct{ file string }

// openKeychain returns the Store for the user's default keychain.
func openKeychain() (Store, error) { return &Keychain{}, nil }

// NewKeychain returns a Store keeping secrets in the keychain at path. An
// empty path is the user's default keychain.
func NewKeychain(path string) *Keychain { return &Keychain{file: path} }

// Name says which keychain the secrets are in.
func (k *Keychain) Name() string {
	if k.file == "" {
		return string(KindKeychain) + " (the default keychain)"
	}
	return string(KindKeychain) + " " + k.file
}

// Set writes a secret under ref, replacing one already there.
//
// Two things about this call are deliberate. The item already there is removed
// rather than updated in place: `security add-generic-password -U` asks the
// keychain for permission to change an item it did not create, which on an
// unattended daemon means a dialog nobody will ever answer. And the secret is
// an argument, which is how every tool that drives `security` passes one, and
// is visible to another process of the same user for as long as the call takes
// - a user who can read that can read the item out of the keychain anyway.
//
// The item is written with the ordinary access list rather than -A, so what
// may read it is `security` itself rather than every application. That is what
// Owl always reads it through, so the daemon is never asked to approve
// anything.
func (k *Keychain) Set(ctx context.Context, ref, secret string) error {
	if err := checkRef(ref); err != nil {
		return err
	}
	if err := k.Delete(ctx, ref); err != nil {
		return err
	}
	args := []string{"add-generic-password", "-s", service, "-a", ref, "-w", secret}
	if _, err := k.run(ctx, args); err != nil {
		return fmt.Errorf("writing the credential %s to the keychain: %w", ref, err)
	}
	return nil
}

// Get reads one back, or ErrNotFound.
func (k *Keychain) Get(ctx context.Context, ref string) (string, error) {
	if err := checkRef(ref); err != nil {
		return "", err
	}
	out, err := k.run(ctx, []string{"find-generic-password", "-s", service, "-a", ref, "-w"})
	if isNotFound(err) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, ref)
	}
	if err != nil {
		return "", fmt.Errorf("reading the credential %s from the keychain: %w", ref, err)
	}
	// `security -w` prints the password and a newline, and nothing else.
	return strings.TrimRight(out, "\r\n"), nil
}

// Delete removes one, and does not mind one that was never there.
func (k *Keychain) Delete(ctx context.Context, ref string) error {
	if err := checkRef(ref); err != nil {
		return err
	}
	_, err := k.run(ctx, []string{"delete-generic-password", "-s", service, "-a", ref})
	if err == nil || isNotFound(err) {
		return nil
	}
	return fmt.Errorf("removing the credential %s from the keychain: %w", ref, err)
}

// run calls `security`, with the keychain to work in appended where one was
// named. It must be last: `security` takes it as an argument of its own, after
// the options.
func (k *Keychain) run(ctx context.Context, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, keychainTimeout)
	defer cancel()
	if k.file != "" {
		args = append(args, k.file)
	}
	cmd := exec.CommandContext(ctx, "security", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", &keychainError{status: exit.ExitCode(), said: strings.TrimSpace(stderr.String())}
		}
		return "", err
	}
	return string(out), nil
}

// keychainError is what `security` said and the status it said it with, so
// that "the item is not there" can be told from everything else.
type keychainError struct {
	status int
	said   string
}

func (e *keychainError) Error() string {
	if e.said == "" {
		return fmt.Sprintf("security exited %d", e.status)
	}
	return fmt.Sprintf("security exited %d: %s", e.status, e.said)
}

// isNotFound reports whether security said the item is not there.
func isNotFound(err error) bool {
	var kerr *keychainError
	return errors.As(err, &kerr) && kerr.status == notFoundStatus
}
