package cli_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/cli"
	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/daemon"
	"github.com/vojtechmares/coding-owl/internal/xdg"
)

func tempPaths(t *testing.T) xdg.Paths {
	t.Helper()
	root, err := os.MkdirTemp("", "owl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	state := filepath.Join(root, "state", "coding-owl")
	return xdg.Paths{
		ConfigDir:  filepath.Join(root, "cfg", "coding-owl"),
		DataDir:    filepath.Join(root, "data", "coding-owl"),
		StateDir:   state,
		SocketPath: filepath.Join(state, "owld.sock"),
	}
}

func startDaemon(t *testing.T, paths xdg.Paths) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx, daemon.Options{Paths: paths, Version: "9.9.9", Logger: slog.New(slog.DiscardHandler)})
	}()
	t.Cleanup(func() { cancel(); <-done })
	c := client.New(paths.SocketPath)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.DaemonStatus(ctx); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon did not come up")
}

func execute(t *testing.T, paths xdg.Paths, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = cli.Run(context.Background(), cli.Env{Paths: paths, Stdout: &out, Stderr: &errb}, args)
	return out.String(), errb.String(), code
}

func TestDaemonStatusPrintsVersionUptimeSocket(t *testing.T) {
	paths := tempPaths(t)
	startDaemon(t, paths)

	stdout, stderr, code := execute(t, paths, "daemon", "status")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 3 || lines[0] != "version: 9.9.9" || !strings.HasPrefix(lines[1], "uptime: ") || lines[2] != "socket: "+paths.SocketPath {
		t.Errorf("unexpected output:\n%s", stdout)
	}
}

func TestDaemonStatusWhenNotRunning(t *testing.T) {
	paths := tempPaths(t)
	stdout, stderr, code := execute(t, paths, "daemon", "status")
	if code == 0 {
		t.Fatal("expected non-zero exit")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "owl: daemon not running at "+paths.SocketPath) {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	_, stderr, code := execute(t, tempPaths(t), "frobnicate")
	if code == 0 || stderr == "" {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}
