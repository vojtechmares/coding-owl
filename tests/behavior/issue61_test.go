package behavior_test

// Behavior test for issue #61. TestS3 maps to scenario S3 in
// tests/behavior/issue-61.md; S1 and S2 live with the daemon package, whose
// own API they exercise. This one drives the built owl binary under a
// permissive umask, with a runtime directory somebody else made too open.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestS3OwlDaemonRunUnderAPermissiveUmaskRestrictsTheSocketAndItsRuntimeDirectory(t *testing.T) {
	l := newLayout(t)
	runtime := filepath.Join(l.root, "r")
	dir := filepath.Join(runtime, "coding-owl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	l = l.withEnv("XDG_RUNTIME_DIR=" + runtime)
	sock := filepath.Join(dir, "owld.sock")
	// The daemon inherits the umask of whoever starts it.
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	startDaemon(t, l)
	waitForSocket(t, sock)
	if res := runOwl(t, l, "daemon", "status"); res.code != 0 {
		t.Fatalf("owl daemon status: exit %d, stderr: %s", res.code, res.stderr)
	}

	if got := perm(t, sock); got != 0o600 {
		t.Errorf("socket mode = %o, want 600: only the owning user may connect", got)
	}
	if got := perm(t, dir); got != 0o700 {
		t.Errorf("runtime directory mode = %o, want 700: a directory found in place is tightened like one made new", got)
	}
}

// perm is the permission bits of what is at path.
func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	return st.Mode().Perm()
}
