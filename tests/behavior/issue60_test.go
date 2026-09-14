package behavior_test

// Behavior test for issue #60. TestS3 maps to scenario S3 in
// tests/behavior/issue-60.md; S1 and S2 live with the daemon package, whose
// own API they exercise. This one drives the built owl binary with a
// configuration file the daemon cannot read.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestS3OwlDaemonRunWithABrokenConfigurationExitsAndLeavesTheDaemonNotRunning(t *testing.T) {
	l := newLayout(t)
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\ncredentialStore: file\nnotAKey: 1\n")
	config := filepath.Join(l.config, "coding-owl", "config.yaml")

	p := startDaemon(t, l)
	code := p.exit(t, 10*time.Second)

	if code == 0 {
		t.Fatalf("owl daemon run exited 0 with a configuration it cannot read; output:\n%s", p.out())
	}
	if !strings.Contains(p.out(), config) {
		t.Errorf("the output does not name the configuration file %s:\n%s", config, p.out())
	}
	if _, err := os.Lstat(l.socket()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Lstat(socket) = %v, want not to exist: a daemon that could not start left a socket behind", err)
	}
	res := runOwl(t, l, "daemon", "status")
	if res.code == 0 || !strings.Contains(res.stderr, "not running") {
		t.Errorf("owl daemon status = %d %q, want a non-zero exit saying the daemon is not running", res.code, res.stderr)
	}
}
