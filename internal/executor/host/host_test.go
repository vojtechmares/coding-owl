package host_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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
