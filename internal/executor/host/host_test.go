package host_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	if sig := p.KilledBy(); sig != nil {
		t.Errorf("an agent that exited 3 is reported killed by %v", sig)
	}
}

// An Agent the OOM killer, a crash or somebody's kill -9 ends did not exit
// with a status, and the signal it was killed by is the only account of it
// there is (issue #101).
func TestStartReportsTheSignalAnAgentWasKilledBy(t *testing.T) {
	p, err := host.New().Start(context.Background(), agent.Invocation{Path: script(t, "kill -KILL $$\n"), Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, _ = io.ReadAll(p.Stdout())

	_, err = p.Wait()

	if err != nil {
		t.Errorf("Wait on a killed agent = %v, want the signal reported instead", err)
	}
	if got := p.KilledBy(); got != syscall.SIGKILL {
		t.Errorf("killed by %v, want %v", got, syscall.SIGKILL)
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

// Cancelling the context is Owl ending the Agent itself, and an Agent that
// acts on the SIGTERM it is sent dies of it. That is reported like any other
// signal: telling Owl's own endings apart is the Run's to do, not the
// Executor's (issue #101, ADR-0034).
func TestStartReportsTheSignalACancelledAgentDiedOf(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// exec, so the Agent is the process that is sent SIGTERM rather than a
	// shell waiting on it.
	p, err := host.New().Start(ctx, agent.Invocation{Path: script(t, "echo started\nexec sleep 60\n"), Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	buf := make([]byte, len("started\n"))
	if _, err := io.ReadFull(p.Stdout(), buf); err != nil {
		t.Fatalf("reading the first line: %v", err)
	}

	cancel()
	_, _ = io.ReadAll(p.Stdout())
	_, err = p.Wait()

	if err != nil {
		t.Errorf("Wait on a cancelled agent = %v, want the signal reported instead", err)
	}
	if got := p.KilledBy(); got != syscall.SIGTERM {
		t.Errorf("killed by %v, want %v, which the agent was stopped with", got, syscall.SIGTERM)
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
	// The Agent leaves behind a child that ignores being asked to stop, so the
	// group still has a member after the Agent is reaped and after Wait has
	// tidied up what it could. Signalling by the group's id would still work
	// here - and that is exactly the habit that signals a stranger once the
	// group is empty and the system has handed the number out again.
	p, err := host.New().Start(context.Background(), shell(t,
		"(trap '' TERM; for i in $(seq 1 200); do echo beat >> "+beats+"; sleep 0.05; done) >/dev/null 2>&1 &\nexit 0\n"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, p.Stdout()) }()
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	waitFor(t, "the child the agent left behind", func() bool { return count(beats) > 0 })
	t.Cleanup(func() { _ = syscall.Kill(-pidOf(t, p), syscall.SIGKILL) })

	// SIGKILL, which nothing can ignore: if the guard were not there, the
	// child would be gone rather than still beating.
	err = p.SignalGroup(syscall.SIGKILL)

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

func TestWaitCleansUpOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	beats := filepath.Join(dir, "beats")
	terms := filepath.Join(dir, "terms")
	// The child records every time it is asked to stop, and ignores the
	// asking, so a scenario can count how often the group was signalled.
	ready := filepath.Join(dir, "ready")
	// The Agent waits for the child to have its handler in place before it
	// exits, so what the cleanup signal lands on is not a matter of timing.
	p, err := host.New().Start(context.Background(), shell(t,
		"(trap 'echo term >> "+terms+"' TERM; echo ready > "+ready+
			"; for i in $(seq 1 200); do echo beat >> "+beats+"; sleep 0.05; done) >/dev/null 2>&1 &\n"+
			"while [ ! -f "+ready+" ]; do sleep 0.01; done\nexit 0\n"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, p.Stdout()) }()
	if _, err := p.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	pid := pidOf(t, p)
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	waitFor(t, "the child to be asked to stop", func() bool { return lines(terms) == 1 })

	// A second Wait must signal nothing: by now the pid is the system's to
	// hand out again, and there is no member of the group left to pin it.
	if _, err := p.Wait(); err == nil {
		t.Error("a second Wait reported success")
	}

	time.Sleep(200 * time.Millisecond)
	if got := lines(terms); got != 1 {
		t.Errorf("the group was asked to stop %d times, want once", got)
	}
}

// lines counts the lines of a file, or none when it is not there yet.
func lines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

// grace is the five seconds a cancelled Agent has to act on SIGTERM before it
// is killed (ADR-0034), and margin is what a scenario allows on top of it for
// the kill to land and be seen.
const (
	grace  = 5 * time.Second
	margin = 3 * time.Second
)

// pidIn reads a pid a script wrote to a file, or zero while there is none.
func pidIn(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

// alive reports whether a process with that pid can still be signalled.
func alive(pid int) bool { return syscall.Kill(pid, syscall.Signal(0)) == nil }

func TestS1CancelledAgentsChildThatIgnoresSIGTERMIsKilledWithinTheGracePeriod(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The Agent starts a child that will not stop when asked, writes the
	// child's pid down, and waits.
	p, err := host.New().Start(ctx, shell(t,
		"(trap '' TERM; while :; do sleep 0.05; done) >/dev/null 2>&1 &\necho $! > "+pidFile+"\necho started\nsleep 60\n"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, p.Stdout()) }()
	waitFor(t, "the child to start", func() bool { return pidIn(pidFile) != 0 && alive(pidIn(pidFile)) })
	child := pidIn(pidFile)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })

	cancelledAt := time.Now()
	cancel()

	done := make(chan struct{})
	go func() { _, _ = p.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(grace + margin):
		t.Fatal("Wait did not return within the grace period plus a margin")
	}
	deadline := cancelledAt.Add(grace + margin)
	for alive(child) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(child) {
		t.Errorf("the child that ignores SIGTERM is still alive %s after the cancel", time.Since(cancelledAt))
	}
}

func TestS2CancelledAgentThatStopsWhenAskedIsNotMadeToWaitForTheKill(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// An Agent that acts on being asked to stop: it exits, and takes the short
	// sleeps it runs with it. A bare `sleep 60` would not do here - a sleep
	// forked at the very moment the signal lands can miss it and live on, which
	// is exactly the case the grace period is for.
	p, err := host.New().Start(ctx, agent.Invocation{
		Path: script(t, "trap 'exit 0' TERM\necho started\nwhile :; do sleep 0.1; done\n"), Dir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	buf := make([]byte, len("started\n"))
	if _, err := io.ReadFull(p.Stdout(), buf); err != nil {
		t.Fatalf("reading the first line: %v", err)
	}

	cancelledAt := time.Now()
	cancel()

	done := make(chan struct{})
	go func() {
		_, _ = io.ReadAll(p.Stdout())
		_, _ = p.Wait()
		close(done)
	}()
	select {
	case <-done:
		if elapsed := time.Since(cancelledAt); elapsed >= grace {
			t.Errorf("Wait took %s for an agent that stops on SIGTERM, want well under the %s grace period", elapsed, grace)
		}
	case <-time.After(grace + margin):
		t.Fatal("Wait did not return")
	}
}

func TestS3ExecutorTakesWhatAnInvocationUnsetsOutOfTheAgentsEnvironment(t *testing.T) {
	// The daemon's own environment carries a credential (issue #52).
	t.Setenv("ANTHROPIC_API_KEY", "the-daemons-own-key")

	p, err := host.New().Start(context.Background(), agent.Invocation{
		Path:  script(t, "echo \"key=$ANTHROPIC_API_KEY\"\necho \"own=$OWL_TEST_VALUE\"\n"),
		Dir:   t.TempDir(),
		Env:   []string{"OWL_TEST_VALUE=carried"},
		Unset: []string{"ANTHROPIC_API_KEY"},
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
		t.Fatalf("output = %q, want the two values", out)
	}
	if lines[0] != "key=" {
		t.Errorf("the agent saw %q, want the daemon's credential taken out of its environment", lines[0])
	}
	if lines[1] != "own=carried" {
		t.Errorf("the agent saw %q, want the value the invocation added", lines[1])
	}
}
