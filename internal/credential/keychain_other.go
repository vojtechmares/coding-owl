//go:build !darwin

package credential

import "errors"

// openKeychain reports that there is no keychain here. Owl is a macOS program
// (ADR-0002, ADR-0010); everywhere else it keeps secrets in a file only their
// owner can read, and says so rather than pretending to have a keychain.
func openKeychain() (Store, error) {
	return nil, errors.New("this platform has no keychain Owl can use; set credentialStore: file in the daemon's configuration")
}
