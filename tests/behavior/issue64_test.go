package behavior_test

// Behavior tests for issue #64. TestS<n> maps to scenario S<n> in
// tests/behavior/issue-64.md; S4 lives with the client package, whose own API
// it exercises. These drive the built owl binary with a state directory that
// puts the socket path over the limit a unix socket address allows.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overlongSocket is a layout whose socket path is too long for a unix socket
// address, and the path it resolves to.
func overlongSocket(t *testing.T) (*layout, string) {
	t.Helper()
	l := newLayout(t)
	long := filepath.Join(l.root, strings.Repeat("d", 60), strings.Repeat("e", 60))
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	return l.withEnv("XDG_STATE_HOME=" + long), filepath.Join(long, "coding-owl", "owld.sock")
}

// saysTooLong checks that a command's failure names the over-long path as
// what is wrong, rather than a daemon that is not running or the dialer's own
// words for a path it cannot use.
func saysTooLong(t *testing.T, what string, res result, sock string) {
	t.Helper()
	if res.code == 0 {
		t.Fatalf("%s exited 0 with a socket path too long to dial\nstdout:\n%s", what, res.stdout)
	}
	if !strings.Contains(res.stderr, "too long") || !strings.Contains(res.stderr, sock) {
		t.Errorf("%s does not say the socket path %s is too long: %q", what, sock, res.stderr)
	}
	if strings.Contains(res.stderr, "not running") || strings.Contains(res.stderr, "invalid argument") {
		t.Errorf("%s blames something other than the path's length: %q", what, res.stderr)
	}
}

func TestS1OwlStatusSaysTheSocketPathIsTooLong(t *testing.T) {
	l, sock := overlongSocket(t)

	res := runOwl(t, l, "status")

	saysTooLong(t, "owl status", res, sock)
}

func TestS2OwlAddSaysTheSocketPathIsTooLong(t *testing.T) {
	l, sock := overlongSocket(t)

	res := runOwl(t, l, "add", "work", "--project", "api")

	saysTooLong(t, "owl add", res, sock)
}

func TestS3OwlDaemonStatusSaysTheSameThing(t *testing.T) {
	l, sock := overlongSocket(t)

	res := runOwl(t, l, "daemon", "status")

	saysTooLong(t, "owl daemon status", res, sock)
}
