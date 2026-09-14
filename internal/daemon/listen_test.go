package daemon

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Nobody but the owner can reach the socket from the moment it exists: this
// is the one check the end-state tests cannot make, since by the time a daemon
// answers the socket has been restricted a second time. What the umask leaves
// the owner is the platform's business (macOS keeps the owner's execute bit on
// a socket); what it leaves everybody else is nothing.
func TestListenPrivateMakesTheSocketTheOwnersAloneUnderAPermissiveUmask(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })
	// A unix socket path is short by necessity, so not under t.TempDir, whose
	// name carries the test's.
	root, err := os.MkdirTemp("", "owl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	sock := filepath.Join(root, "owld.sock")

	ln, err := listenPrivate(sock)
	if err != nil {
		t.Fatalf("listenPrivate: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	st, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got&0o077 != 0 {
		t.Errorf("socket mode as made = %o, want nothing for group or others: the umask let somebody else in", got)
	}
	if got := syscall.Umask(0o022); got != 0o022 {
		t.Errorf("umask after listening = %o, want 022: it was not restored", got)
	}
}
