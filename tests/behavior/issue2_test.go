package behavior_test

// Behavior tests for issue #2. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-2.md. They drive the built owl binary from the outside,
// except S8 which drives the daemon in-process through the shared client.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/daemon"
	"github.com/vojtechmares/coding-owl/internal/xdg"
)

const testVersion = "1.2.3-test"

var (
	owlBin  string
	repoDir string
)

func TestMain(m *testing.M) {
	dir, err := filepath.Abs("../..")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	repoDir = dir

	tmp, err := os.MkdirTemp("", "owlbin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	owlBin = filepath.Join(tmp, "owl")

	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/vojtechmares/coding-owl/internal/version.Version="+testVersion,
		"-o", owlBin, "./cmd/owl")
	build.Dir = repoDir
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building owl:", err)
		_ = os.RemoveAll(tmp)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// layout is a temporary XDG layout. Directories are created under a short
// temp dir because unix socket paths are limited to about 104 bytes.
type layout struct {
	root, home, config, data, state string
	env                             []string
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "owl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newLayout(t *testing.T) *layout {
	t.Helper()
	root := shortTempDir(t)
	l := &layout{
		root:   root,
		home:   filepath.Join(root, "home"),
		config: filepath.Join(root, "cfg"),
		data:   filepath.Join(root, "data"),
		state:  filepath.Join(root, "state"),
	}
	for _, d := range []string{l.home, l.config, l.data, l.state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	l.env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + l.home,
		"XDG_CONFIG_HOME=" + l.config,
		"XDG_DATA_HOME=" + l.data,
		"XDG_STATE_HOME=" + l.state,
	}
	return l
}

func (l *layout) socket() string { return filepath.Join(l.state, "coding-owl", "owld.sock") }

func (l *layout) withEnv(kv ...string) *layout {
	c := *l
	c.env = append(append([]string{}, l.env...), kv...)
	return &c
}

// withoutEnv returns a copy of the layout with the named variables removed.
func (l *layout) withoutEnv(names ...string) *layout {
	c := *l
	c.env = nil
	for _, e := range l.env {
		drop := false
		for _, n := range names {
			if strings.HasPrefix(e, n+"=") {
				drop = true
			}
		}
		if !drop {
			c.env = append(c.env, e)
		}
	}
	return &c
}

type result struct {
	stdout, stderr string
	code           int
}

func runOwl(t *testing.T, l *layout, args ...string) result {
	t.Helper()
	cmd := exec.Command(owlBin, args...)
	cmd.Env = l.env
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running owl %v: %v", args, err)
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// daemonProc is a running `owl daemon run` subprocess.
type daemonProc struct {
	cmd     *exec.Cmd
	out     *syncBuffer
	done    chan struct{} // closed once the process has been waited for
	waitErr error
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func startDaemon(t *testing.T, l *layout) *daemonProc {
	t.Helper()
	cmd := exec.Command(owlBin, "daemon", "run")
	cmd.Env = l.env
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &daemonProc{cmd: cmd, out: out, done: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

// exit waits for the process to end and returns its exit code.
func (p *daemonProc) exit(t *testing.T, within time.Duration) int {
	t.Helper()
	select {
	case <-p.done:
		var exitErr *exec.ExitError
		if errors.As(p.waitErr, &exitErr) {
			return exitErr.ExitCode()
		}
		if p.waitErr != nil {
			t.Fatalf("daemon wait: %v", p.waitErr)
		}
		return 0
	case <-time.After(within):
		t.Fatalf("daemon did not exit within %s; output:\n%s", within, p.out.String())
		return -1
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	waitFor(t, "socket "+path, func() bool {
		st, err := os.Stat(path)
		return err == nil && st.Mode()&fs.ModeSocket != 0
	})
}

func waitForLog(t *testing.T, p *daemonProc, substr string) {
	t.Helper()
	waitFor(t, "log containing "+strconv.Quote(substr), func() bool {
		return strings.Contains(p.out.String(), substr)
	})
}

func line(t *testing.T, out, key string) string {
	t.Helper()
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, key+":") {
			return strings.TrimSpace(strings.TrimPrefix(ln, key+":"))
		}
	}
	t.Fatalf("no %q line in output:\n%s", key, out)
	return ""
}

func TestS1DaemonRunListensInForeground(t *testing.T) {
	l := newLayout(t)
	p := startDaemon(t, l)
	waitForSocket(t, l.socket())
	waitForLog(t, p, "socket="+l.socket())

	if want := "pid=" + strconv.Itoa(p.cmd.Process.Pid); !strings.Contains(p.out.String(), want) {
		t.Errorf("daemon log does not report its own pid %s; the process forked or detached?\n%s", want, p.out.String())
	}

	var pidFiles []string
	_ = filepath.WalkDir(l.root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".pid") {
			pidFiles = append(pidFiles, path)
		}
		return nil
	})
	if len(pidFiles) > 0 {
		t.Errorf("daemon wrote pid files: %v", pidFiles)
	}
}

func TestS2DaemonStatusReportsVersionUptimeSocket(t *testing.T) {
	l := newLayout(t)
	startDaemon(t, l)
	waitForSocket(t, l.socket())

	res := runOwl(t, l, "daemon", "status")
	if res.code != 0 {
		t.Fatalf("exit %d, stderr: %s", res.code, res.stderr)
	}
	if got := line(t, res.stdout, "version"); got != testVersion {
		t.Errorf("version = %q, want %q", got, testVersion)
	}
	if up := line(t, res.stdout, "uptime"); true {
		if _, err := time.ParseDuration(up); err != nil {
			t.Errorf("uptime %q does not parse as a duration: %v", up, err)
		}
	}
	if got := line(t, res.stdout, "socket"); got != l.socket() {
		t.Errorf("socket = %q, want %q", got, l.socket())
	}
}

func TestS3DaemonStatusWithoutDaemonFails(t *testing.T) {
	l := newLayout(t)
	res := runOwl(t, l, "daemon", "status")
	if res.code == 0 {
		t.Fatalf("expected non-zero exit, stdout: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, "not running") {
		t.Errorf("stderr does not say the daemon is not running: %q", res.stderr)
	}
	if !strings.Contains(res.stderr, l.socket()) {
		t.Errorf("stderr does not name the socket path %s: %q", l.socket(), res.stderr)
	}
	if res.stdout != "" {
		t.Errorf("stdout should be empty, got %q", res.stdout)
	}
}

func TestS4SocketPathPrefersRuntimeDir(t *testing.T) {
	l := newLayout(t)
	runtime := filepath.Join(l.root, "run")
	if err := os.MkdirAll(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	l = l.withEnv("XDG_RUNTIME_DIR=" + runtime)
	want := filepath.Join(runtime, "coding-owl", "owld.sock")

	startDaemon(t, l)
	waitForSocket(t, want)

	if _, err := os.Stat(l.socket()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket under XDG_STATE_HOME should not exist, stat err = %v", err)
	}
	res := runOwl(t, l, "daemon", "status")
	if res.code != 0 {
		t.Fatalf("exit %d, stderr: %s", res.code, res.stderr)
	}
	if got := line(t, res.stdout, "socket"); got != want {
		t.Errorf("socket = %q, want %q", got, want)
	}
}

func TestS5SocketPathFallsBackToHome(t *testing.T) {
	l := newLayout(t).withoutEnv("XDG_STATE_HOME", "XDG_RUNTIME_DIR")
	want := filepath.Join(l.home, ".local", "state", "coding-owl", "owld.sock")
	startDaemon(t, l)
	waitForSocket(t, want)
}

func TestS6OverlongSocketPathRefused(t *testing.T) {
	l := newLayout(t)
	long := filepath.Join(l.root, strings.Repeat("d", 60), strings.Repeat("e", 60))
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	l = l.withEnv("XDG_STATE_HOME=" + long)
	want := filepath.Join(long, "coding-owl", "owld.sock")

	p := startDaemon(t, l)
	code := p.exit(t, 5*time.Second)
	if code == 0 {
		t.Fatalf("expected non-zero exit, output: %s", p.out.String())
	}
	out := p.out.String()
	if !strings.Contains(out, "too long") {
		t.Errorf("stderr does not say the path is too long: %q", out)
	}
	if !strings.Contains(out, want) {
		t.Errorf("stderr does not print the path %s: %q", want, out)
	}
	if _, err := os.Stat(want); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket should not exist, stat err = %v", err)
	}
}

var dirRe = regexp.MustCompile(`(config|data|state)=(\S+)`)

func startupDirs(t *testing.T, p *daemonProc) map[string]string {
	t.Helper()
	waitForLog(t, p, "state=")
	dirs := map[string]string{}
	for _, m := range dirRe.FindAllStringSubmatch(p.out.String(), -1) {
		dirs[m[1]] = strings.Trim(m[2], `"`)
	}
	return dirs
}

func TestS7DirectoriesResolvePerADR0014(t *testing.T) {
	t.Run("env set", func(t *testing.T) {
		l := newLayout(t)
		p := startDaemon(t, l)
		waitForSocket(t, l.socket())
		dirs := startupDirs(t, p)
		want := map[string]string{
			"config": filepath.Join(l.config, "coding-owl"),
			"data":   filepath.Join(l.data, "coding-owl"),
			"state":  filepath.Join(l.state, "coding-owl"),
		}
		for k, w := range want {
			if dirs[k] != w {
				t.Errorf("%s = %q, want %q", k, dirs[k], w)
			}
		}
	})
	t.Run("defaults", func(t *testing.T) {
		l := newLayout(t).withoutEnv("XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR")
		p := startDaemon(t, l)
		waitForSocket(t, filepath.Join(l.home, ".local", "state", "coding-owl", "owld.sock"))
		dirs := startupDirs(t, p)
		want := map[string]string{
			"config": filepath.Join(l.home, ".config", "coding-owl"),
			"data":   filepath.Join(l.home, ".local", "share", "coding-owl"),
			"state":  filepath.Join(l.home, ".local", "state", "coding-owl"),
		}
		for k, w := range want {
			if dirs[k] != w {
				t.Errorf("%s = %q, want %q", k, dirs[k], w)
			}
		}
	})
}

func TestS8StatusThroughSharedClientInProcess(t *testing.T) {
	l := newLayout(t)
	paths, err := xdg.ResolveFrom(func(k string) string {
		for _, e := range l.env {
			if strings.HasPrefix(e, k+"=") {
				return strings.TrimPrefix(e, k+"=")
			}
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- daemon.Run(ctx, daemon.Options{
			Paths:   paths,
			Version: "in-process",
			Logger:  slog.New(slog.NewTextHandler(os.Stderr, nil)),
		})
	}()

	c := client.New(paths.SocketPath)
	var st *client.DaemonStatus
	waitFor(t, "daemon to answer", func() bool {
		var err error
		st, err = c.DaemonStatus(context.Background())
		return err == nil
	})
	if st.Version != "in-process" {
		t.Errorf("version = %q, want in-process", st.Version)
	}
	if st.Uptime < 0 {
		t.Errorf("uptime = %s, want non-negative", st.Uptime)
	}
	if st.SocketPath != paths.SocketPath {
		t.Errorf("socket = %q, want %q", st.SocketPath, paths.SocketPath)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("daemon.Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}

	_, err = c.DaemonStatus(context.Background())
	if !errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("after stop, err = %v, want ErrDaemonNotRunning", err)
	}
}

func TestS9SigtermStopsCleanly(t *testing.T) {
	l := newLayout(t)
	p := startDaemon(t, l)
	waitForSocket(t, l.socket())

	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := p.exit(t, 5*time.Second); code != 0 {
		t.Errorf("exit code = %d, want 0; output:\n%s", code, p.out.String())
	}
	if _, err := os.Stat(l.socket()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("socket should be removed, stat err = %v", err)
	}
}

func TestS10StaleSocketReplaced(t *testing.T) {
	l := newLayout(t)
	if err := os.MkdirAll(filepath.Dir(l.socket()), 0o755); err != nil {
		t.Fatal(err)
	}
	// A leftover socket that nothing listens on: bind then close without
	// unlinking, which is what a crashed daemon leaves behind.
	stale, err := os.Create(l.socket())
	if err != nil {
		t.Fatal(err)
	}
	_ = stale.Close()

	startDaemon(t, l)
	waitForSocket(t, l.socket())
	res := runOwl(t, l, "daemon", "status")
	if res.code != 0 {
		t.Fatalf("exit %d, stderr: %s", res.code, res.stderr)
	}
}

func TestS11SecondDaemonRefused(t *testing.T) {
	l := newLayout(t)
	startDaemon(t, l)
	waitForSocket(t, l.socket())

	second := startDaemon(t, l)
	if code := second.exit(t, 5*time.Second); code == 0 {
		t.Fatalf("second daemon should fail; output: %s", second.out.String())
	}
	if !strings.Contains(second.out.String(), "already listening") {
		t.Errorf("second daemon stderr should say a daemon is already listening: %q", second.out.String())
	}

	res := runOwl(t, l, "daemon", "status")
	if res.code != 0 {
		t.Fatalf("first daemon stopped answering: exit %d, stderr: %s", res.code, res.stderr)
	}
}

func TestS12CLIUsesOnlySharedClient(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "./internal/cli")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	imports := strings.Split(strings.TrimSpace(string(out)), "\n")
	hasClient := false
	for _, imp := range imports {
		if imp == "github.com/vojtechmares/coding-owl/internal/client" {
			hasClient = true
		}
		if strings.Contains(imp, "/gen/") || strings.HasPrefix(imp, "connectrpc.com/") || strings.HasPrefix(imp, "google.golang.org/protobuf") {
			t.Errorf("CLI imports %s directly; it must go through the shared client", imp)
		}
	}
	if !hasClient {
		t.Errorf("CLI does not import the shared client package; imports: %v", imports)
	}
}

func TestS13CIRunsTestsLintAndBreaking(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoDir, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	ci := string(b)
	for _, want := range []string{"push", "go test", "buf lint", "buf breaking", "main"} {
		if !strings.Contains(ci, want) {
			t.Errorf("ci.yml does not contain %q", want)
		}
	}
}
