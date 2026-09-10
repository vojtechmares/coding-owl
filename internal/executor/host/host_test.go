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

// script writes a shell script and returns its path.
func script(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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

func TestStartRefusesAnAgentWithNowhereToRun(t *testing.T) {
	_, err := host.New().Start(context.Background(), agent.Invocation{Path: "/bin/sh", Args: []string{"-c", "true"}})

	if err == nil {
		t.Fatal("Start accepted an agent with no working directory")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("error %q does not say what is missing", err)
	}
}

func TestStartLetsTheInvocationOverrideWhatTheDaemonInherited(t *testing.T) {
	t.Setenv("OWL_TEST_VALUE", "the daemon's own")

	p, err := host.New().Start(context.Background(), agent.Invocation{
		Path: script(t, "echo $OWL_TEST_VALUE\n"),
		Dir:  t.TempDir(),
		Env:  []string{"OWL_TEST_VALUE=the account's"},
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

	// An Account's configuration directory and token are added to an
	// environment that may already carry the user's own (ADR-0019), so the
	// invocation has to win.
	if got := strings.TrimSpace(string(out)); got != "the account's" {
		t.Errorf("the agent saw %q, want what the invocation set", got)
	}
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
