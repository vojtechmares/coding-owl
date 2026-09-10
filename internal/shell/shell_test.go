package shell_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
