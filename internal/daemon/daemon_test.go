package daemon_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	return xdg.Paths{
		ConfigDir:  filepath.Join(root, "cfg", "coding-owl"),
		DataDir:    filepath.Join(root, "data", "coding-owl"),
		StateDir:   filepath.Join(root, "state", "coding-owl"),
		SocketPath: filepath.Join(root, "state", "coding-owl", "owld.sock"),
	}
}

func run(t *testing.T, paths xdg.Paths) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- daemon.Run(ctx, daemon.Options{
			Paths:   paths,
			Version: "t",
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		})
	}()
	var once sync.Once
	var stopErr error
	stop = func() error {
		once.Do(func() {
			cancel()
			select {
			case stopErr = <-errc:
			case <-time.After(5 * time.Second):
				stopErr = errors.New("daemon did not stop")
			}
		})
		return stopErr
	}
	t.Cleanup(func() { _ = stop() })
	return stop
}

func waitStatus(t *testing.T, c *client.Client) *client.DaemonStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		st, err := c.DaemonStatus(context.Background())
		if err == nil {
			return st
		}
		last = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("daemon never answered: %v", last)
	return nil
}

func TestStatusOverSocketThenNotRunningAfterStop(t *testing.T) {
	paths := tempPaths(t)
	stop := run(t, paths)
	c := client.New(paths.SocketPath)

	st := waitStatus(t, c)
	if st.Version != "t" || st.SocketPath != paths.SocketPath || st.Uptime < 0 {
		t.Errorf("status = %+v", st)
	}

	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := os.Stat(paths.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket not removed after stop: %v", err)
	}
	_, err := c.DaemonStatus(context.Background())
	if !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("err = %v, want ErrDaemonNotRunning", err)
	}
}

// leaveStaleSocket binds a unix socket and closes it without unlinking,
// which is exactly what a crashed daemon leaves behind.
func leaveStaleSocket(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = ln.Close()
}

func TestStaleSocketIsReplaced(t *testing.T) {
	paths := tempPaths(t)
	leaveStaleSocket(t, paths.SocketPath)

	run(t, paths)
	waitStatus(t, client.New(paths.SocketPath))
}

func TestNonSocketFileAtSocketPathIsRefused(t *testing.T) {
	paths := tempPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.SocketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.SocketPath, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := daemon.Run(context.Background(), daemon.Options{Paths: paths, Version: "t"})
	if err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("err = %v, want a refusal naming a non-socket", err)
	}
	if b, rerr := os.ReadFile(paths.SocketPath); rerr != nil || string(b) != "not a socket" {
		t.Errorf("file must be left untouched, got %q %v", b, rerr)
	}
}

func TestSecondDaemonOnSameSocketIsRefused(t *testing.T) {
	paths := tempPaths(t)
	run(t, paths)
	c := client.New(paths.SocketPath)
	waitStatus(t, c)

	err := daemon.Run(context.Background(), daemon.Options{Paths: paths, Version: "t"})
	if !errors.Is(err, daemon.ErrAlreadyListening) {
		t.Fatalf("second Run err = %v, want ErrAlreadyListening", err)
	}
	// The first daemon must be untouched.
	waitStatus(t, c)
}

func TestOverlongSocketPathIsRefused(t *testing.T) {
	paths := tempPaths(t)
	paths.SocketPath = filepath.Join(paths.StateDir, strings.Repeat("x", 120), "owld.sock")
	err := daemon.Run(context.Background(), daemon.Options{Paths: paths, Version: "t"})
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("err = %v, want a refusal saying the path is too long", err)
	}
}

func TestSilentSocketAtSocketPathIsRefused(t *testing.T) {
	paths := tempPaths(t)
	if err := os.MkdirAll(filepath.Dir(paths.SocketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// A datagram socket is a socket inode that neither answers nor refuses a
	// stream dial; the daemon must leave it alone rather than guess.
	conn, err := net.ListenPacket("unixgram", paths.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	err = daemon.Run(context.Background(), daemon.Options{Paths: paths, Version: "t"})
	if err == nil || !strings.Contains(err.Error(), "refusing to remove") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if _, serr := os.Lstat(paths.SocketPath); serr != nil {
		t.Errorf("socket must be left in place: %v", serr)
	}
}

// globalConfig writes the daemon's own configuration file into the layout.
func globalConfig(t *testing.T, paths xdg.Paths, body string) string {
	t.Helper()
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.ConfigDir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// noSocket fails the test if anything is at the socket path: a daemon that
// could not start never owned one, so there is nothing to leave behind.
func noSocket(t *testing.T, paths xdg.Paths) {
	t.Helper()
	if _, err := os.Lstat(paths.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Lstat(socket) = %v, want not to exist: a daemon that could not start left a socket behind", err)
	}
}

func TestS1ADaemonWhoseConfigurationDoesNotParseLeavesNoSocketBehind(t *testing.T) {
	paths := tempPaths(t)
	path := globalConfig(t, paths, "apiVersion: codingowl.dev/v1\ncredentialStore: file\nnotAKey: 1\n")

	err := daemon.Run(context.Background(), daemon.Options{Paths: paths, Version: "t"})

	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want an error naming %s", err, path)
	}
	noSocket(t, paths)
}

func TestS2ADaemonWhoseCredentialStoreIsUnknownLeavesNoSocketBehind(t *testing.T) {
	paths := tempPaths(t)
	globalConfig(t, paths, "apiVersion: codingowl.dev/v1\ncredentialStore: vault\n")

	err := daemon.Run(context.Background(), daemon.Options{Paths: paths, Version: "t"})

	if err == nil || !strings.Contains(err.Error(), "vault") {
		t.Fatalf("err = %v, want an error naming the credential store", err)
	}
	noSocket(t, paths)
}

// TestProjectErrorsReachTheClientClassified checks the whole error path over a
// real socket: a service failure keeps both its message and its classification
// by the time a client sees it.
func TestProjectErrorsReachTheClientClassified(t *testing.T) {
	paths := tempPaths(t)
	stop := run(t, paths)
	defer func() { _ = stop() }()
	c := client.New(paths.SocketPath)
	waitStatus(t, c)
	ctx := context.Background()

	if _, err := c.GetProject(ctx, "ghost"); kindOfClientErr(t, err) != client.KindNotFound {
		t.Errorf("GetProject(ghost) = %v, want a not-found error", err)
	}

	repo := initRepo(t, filepath.Join(t.TempDir(), "api"))
	if _, err := c.AddProject(ctx, repo, "a/b", ""); kindOfClientErr(t, err) != client.KindInvalid {
		t.Errorf("AddProject with an invalid name = %v, want an invalid error", err)
	}
	if _, err := c.AddProject(ctx, repo, "api", ""); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if _, err := c.AddProject(ctx, repo, "api", ""); kindOfClientErr(t, err) != client.KindAlreadyExists {
		t.Errorf("AddProject with a taken name = %v, want an already-exists error", err)
	}
}

func kindOfClientErr(t *testing.T, err error) client.Kind {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	var se *client.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not a *client.StatusError", err)
	}
	return se.Kind
}

// initRepo makes dir a repository on branch main with one commit.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=Owl Test", "GIT_AUTHOR_EMAIL=owl@example.com",
		"GIT_COMMITTER_NAME=Owl Test", "GIT_COMMITTER_EMAIL=owl@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"commit", "--allow-empty", "-m", "initial commit"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}
