//go:build !darwin

package credential_test

import (
	"testing"

	"github.com/vojtechmares/coding-owl/internal/credential"
)

// testKeychain reports that there is no keychain to exercise here.
func testKeychain(*testing.T) (credential.Store, bool) { return nil, false }
