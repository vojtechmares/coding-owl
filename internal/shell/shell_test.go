package shell_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/shell"
)

func TestRunReportsWhatACommandDidAndPrinted(t *testing.T) {
	res, err := shell.Run(context.Background(), t.TempDir(),
		"echo to stdout; echo to stderr >&2; exit 2", time.Minute)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 2 {
		t.Errorf("exit status = %d, want 2", res.ExitCode)
	}
	if res.Stdout != "to stdout\n" {
		t.Errorf("stdout = %q, want only what went there", res.Stdout)
	}
	if !strings.Contains(res.Output, "to stdout") || !strings.Contains(res.Output, "to stderr") {
		t.Errorf("output = %q, want both streams", res.Output)
	}
	if res.TimedOut {
		t.Error("a command that exited is reported as timed out")
	}
}

func TestRunGivesTheShellItsIdioms(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := shell.Run(context.Background(), dir, `test -z "$(ls *.go)" && echo tidy || echo untidy`, time.Minute)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "untidy" {
		t.Errorf("stdout = %q; the command did not get a shell", res.Stdout)
	}
}

func TestRunStopsACommandAtItsTimeout(t *testing.T) {
	started := time.Now()

	res, err := shell.Run(context.Background(), t.TempDir(), "sleep 60", 200*time.Millisecond)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("the command ran for %s, so it was not stopped", elapsed)
	}
	if !res.TimedOut {
		t.Error("a command that was stopped is not reported as timed out")
	}
}

func TestRunTakesWhatACommandStartedWithIt(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-still-running")

	// The child outlives its parent shell unless the whole group is stopped.
	_, err := shell.Run(context.Background(), dir,
		"sh -c 'sleep 3; touch "+marker+"' & sleep 60", 200*time.Millisecond)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	time.Sleep(4 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("what the command started outlived it")
	}
}

func TestRunRefusesACommandWithNowhereToRun(t *testing.T) {
	_, err := shell.Run(context.Background(), "", "true", time.Minute)

	if err == nil {
		t.Fatal("Run with no directory = nil, want an error")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error %q does not say what is missing", err)
	}
}

func TestRunTreatsACommandThatOutlivesItsOutputAsFinished(t *testing.T) {
	// The command exits, but what it started in the background still holds the
	// pipe open - which is what a `docker compose up -d` looks like.
	res, err := shell.Run(context.Background(), t.TempDir(), "sh -c 'sleep 30' & exit 0", time.Minute)

	if err != nil {
		t.Fatalf("Run: %v; a command that exited is not a command that could not be run", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit status = %d, want the 0 it exited with", res.ExitCode)
	}
	if res.TimedOut || res.Cancelled {
		t.Errorf("result = %+v, want neither timed out nor stopped", res)
	}
}

// grace is the five seconds a timed-out command has to act on SIGTERM before
// it is killed (ADR-0034), and margin is what a scenario allows on top of it
// for the kill to land and be seen.
const (
	grace  = 5 * time.Second
	margin = 3 * time.Second
)

// pidIn reads a pid a command wrote to a file, or zero while there is none.
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

func TestS3TimedOutChecksChildThatIgnoresSIGTERMIsKilledWithinTheGracePeriod(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	timeout := 200 * time.Millisecond
	started := time.Now()

	// The check starts a child that will not stop when asked, writes the
	// child's pid down, and waits past its own timeout.
	res, err := shell.Run(context.Background(), dir,
		"(trap '' TERM; while :; do sleep 0.05; done) >/dev/null 2>&1 & echo $! > "+pidFile+"; sleep 60", timeout)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	child := pidIn(pidFile)
	if child == 0 {
		t.Fatal("the command recorded no child")
	}
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if elapsed := time.Since(started); elapsed > timeout+grace+margin {
		t.Errorf("Run took %s, want it back within the grace period plus a margin of its timeout", elapsed)
	}
	if !res.TimedOut {
		t.Error("a command that was stopped is not reported as timed out")
	}
	// A killed child is gone a moment after the kill, once whatever it was
	// left to has reaped it.
	deadline := started.Add(timeout + grace + margin)
	for alive(child) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if alive(child) {
		t.Errorf("the child that ignores SIGTERM is still alive %s after the command started", time.Since(started))
	}
}

func TestS4TimedOutCheckThatStopsWhenAskedReturnsAtItsTimeout(t *testing.T) {
	timeout := 200 * time.Millisecond
	started := time.Now()

	res, err := shell.Run(context.Background(), t.TempDir(), "sleep 60", timeout)

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if elapsed := time.Since(started); elapsed >= timeout+grace {
		t.Errorf("Run took %s for a command that stops on SIGTERM, want well under its timeout plus the %s grace period", elapsed, grace)
	}
	if !res.TimedOut {
		t.Error("a command that was stopped is not reported as timed out")
	}
}
