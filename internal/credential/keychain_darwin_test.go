package credential_test

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/credential"
)

// testKeychain returns a Store over a keychain of the test's own, made and
// deleted here so that no test touches the user's. Every call the Store makes
// names that keychain, so nothing reaches the default one even if this
// machine has no keychain at all - in which case there is nothing to test and
// the caller is told so.
func testKeychain(t *testing.T) (credential.Store, bool) {
	t.Helper()
	if _, err := exec.LookPath("security"); err != nil {
		t.Log("no security command here; the keychain store is not exercised")
		return nil, false
	}
	path := filepath.Join(t.TempDir(), "owl-test.keychain-db")
	if out, err := exec.Command("security", "create-keychain", "-p", "owl-test", path).CombinedOutput(); err != nil {
		t.Logf("creating a keychain to test with: %v\n%s", err, out)
		return nil, false
	}
	t.Cleanup(func() {
		if out, err := exec.Command("security", "delete-keychain", path).CombinedOutput(); err != nil {
			t.Errorf("deleting the test keychain: %v\n%s", err, out)
		}
	})
	return credential.NewKeychain(path), true
}
