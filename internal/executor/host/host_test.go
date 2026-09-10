package host_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/executor/host"
)

// script writes an executable shell script and returns its path.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStartRunsTheAgentAndReportsItsOutput(t *testing.T) {
	path := script(t, "echo one\necho two\necho oops >&2\nexit 0\n")

	p, err := host.New().Start(context.Background(), agent.Invocation{Path: path, Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	code, err := p.Wait()

	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 0 {
		t.Errorf("exit status = %d, want 0", code)
	}
	if string(out) != "one\ntwo\n" {
		t.Errorf("stdout = %q, want the two lines it printed", out)
	}
	if !strings.Contains(p.Stderr(), "oops") {
		t.Errorf("stderr = %q, want what the agent wrote there", p.Stderr())
	}
}

func TestStartReportsANonZeroExitAsAStatusNotAnError(t *testing.T) {
	p, err := host.New().Start(context.Background(), agent.Invocation{Path: script(t, "exit 3\n"), Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, _ = io.ReadAll(p.Stdout())

	code, err := p.Wait()

	if err != nil {
		t.Errorf("Wait on a failed agent = %v, want the status instead", err)
	}
	if code != 3 {
		t.Errorf("exit status = %d, want 3", code)
	}
}

func TestStartRunsInTheInvocationsDirectoryAndEnvironment(t *testing.T) {
	dir := t.TempDir()

	p, err := host.New().Start(context.Background(), agent.Invocation{
		Path: script(t, "pwd\necho $OWL_TEST_VALUE\n"),
		Dir:  dir,
		Env:  []string{"OWL_TEST_VALUE=carried"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out, err := io.ReadAll(p.Stdout())
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q, want the directory and the value", out)
	}
	if got, want := resolve(t, lines[0]), resolve(t, dir); got != want {
		t.Errorf("the agent ran in %s, want %s", got, want)
	}
	if lines[1] != "carried" {
		t.Errorf("the agent saw %q, want the value the invocation added", lines[1])
	}
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return p
}

func TestStartStopsTheAgentWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, err := host.New().Start(ctx, agent.Invocation{Path: script(t, "echo started\nsleep 60\n"), Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	buf := make([]byte, len("started\n"))
	if _, err := io.ReadFull(p.Stdout(), buf); err != nil {
		t.Fatalf("reading the first line: %v", err)
	}
	cancel()

	done := make(chan int, 1)
	go func() {
		_, _ = io.ReadAll(p.Stdout())
		code, _ := p.Wait()
		done <- code
	}()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the agent outlived its context")
	}
}

func TestStartReportsAProgramThatIsNotThere(t *testing.T) {
	_, err := host.New().Start(context.Background(), agent.Invocation{
		Path: filepath.Join(t.TempDir(), "nothing"), Dir: t.TempDir(),
	})

	if err == nil {
		t.Fatal("Start on a missing program = nil, want an error")
	}
}

func TestStartRefusesAnAgentWithNoWorkingDirectory(t *testing.T) {
	_, err := host.New().Start(context.Background(), agent.Invocation{Path: script(t, "pwd\n")})

	if err == nil {
		t.Fatal("Start with no working directory = nil, want an error: it would inherit the daemon's own")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("error %q does not say what is missing", err)
	}
}

// shell writes a script and returns an Invocation that runs it.
func shell(t *testing.T, script string) agent.Invocation {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return agent.Invocation{Path: "/bin/sh", Args: []string{path}, Dir: dir}
}

func TestSignalGroupReachesWhatTheAgentStarted(t *testing.T) {
	dir := t.TempDir()
	beats := filepath.Join(dir, "beats")
	// The Agent starts a child and then waits: signalling the Agent alone
	// would leave the child writing.
	p, err := host.New().Start(context.Background(), shell(t,
		"(for i in $(seq 1 200); do echo beat >> "+beats+"; sleep 0.05; done) &\necho started\nsleep 30\n"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		_ = p.SignalGroup(syscall.SIGKILL)
		_, _ = p.Wait()
	})
	go func() { _, _ = io.Copy(io.Discard, p.Stdout()) }()
	waitFor(t, "the child to start", func() bool { return count(beats) > 0 })

	if err := p.SignalGroup(syscall.SIGSTOP); err != nil {
		t.Fatalf("SignalGroup: %v", err)
	}

	before := count(beats)
	time.Sleep(300 * time.Millisecond)
	if got := count(beats); got != before {
		t.Errorf("the child wrote %d more beats while the group was stopped", got-before)
	}
	if err := p.SignalGroup(syscall.SIGCONT); err != nil {
		t.Fatalf("SignalGroup: %v", err)
	}
	waitFor(t, "the child to beat again", func() bool { return count(beats) > before })
}

func TestSignalGroupRefusesAProcessThatHasBeenWaitedFor(t *testing.T) {
	dir := t.TempDir()
	beats := filepath.Join(dir, "beats")
	// The Agent leaves a child behind and exits, so the group still has a
	// member after the Agent is reaped. Signalling by the group's id would
	// still work here - and that is exactly the habit that signals a stranger
	// once the group is empty and the system has handed the number out again.
	p, err := host.New().Start(context.Background(), shell(t,
		"(for i in $(seq 1 200); do echo beat >> "+beats+"; sleep 0.05; done) >/dev/null 2>&1 &\nexit 0\n"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, p.Stdout()) }()
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	waitFor(t, "the child the agent left behind", func() bool { return count(beats) > 0 })
	t.Cleanup(func() { _ = syscall.Kill(-pidOf(t, p), syscall.SIGKILL) })

	err = p.SignalGroup(syscall.SIGTERM)

	if !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("SignalGroup after Wait = %v, want %v", err, os.ErrProcessDone)
	}
	// Nothing was signalled: the child is still going.
	before := count(beats)
	time.Sleep(200 * time.Millisecond)
	if count(beats) == before {
		t.Errorf("the child stopped, so the group was signalled by a pid nobody owns any more")
	}
}

// pidOf is the Agent's own pid, which is also its process group's id.
func pidOf(t *testing.T, p agent.Process) int {
	t.Helper()
	type haspid interface{ Pid() int }
	if hp, ok := p.(haspid); ok {
		return hp.Pid()
	}
	t.Skip("the process does not report its pid")
	return 0
}

func count(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "beat\n")
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
