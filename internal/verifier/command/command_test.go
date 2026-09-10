package command_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/verifier"
	"github.com/vojtechmares/coding-owl/internal/verifier/command"
)

// verify runs these checks in a temporary directory.
func verify(t *testing.T, dir string, checks ...config.Check) []verifier.Result {
	t.Helper()
	results, err := command.New().Verify(context.Background(), verifier.Request{
		WorkingDir: dir, Checks: checks,
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != len(checks) {
		t.Fatalf("Verify returned %d results for %d checks", len(results), len(checks))
	}
	return results
}

func TestVerifyRunsEveryCheckEvenAfterOneFails(t *testing.T) {
	results := verify(t, t.TempDir(),
		config.Check{Name: "first", Run: "exit 1"},
		config.Check{Name: "second", Run: "true"},
		config.Check{Name: "third", Run: "exit 3"},
	)

	if results[0].Passed || !results[1].Passed || results[2].Passed {
		t.Errorf("results = %+v, want fail, pass, fail", results)
	}
	if results[2].ExitCode != 3 {
		t.Errorf("third exited %d, want 3", results[2].ExitCode)
	}
	if failed := verifier.Failed(results); len(failed) != 2 {
		t.Errorf("Failed returned %d results, want the two that refused the work", len(failed))
	}
}

func TestVerifyKeepsWhatACheckPrinted(t *testing.T) {
	results := verify(t, t.TempDir(),
		config.Check{Name: "loud", Run: "echo to stdout; echo to stderr >&2; exit 1"})

	if got := results[0].Output; !strings.Contains(got, "to stdout") || !strings.Contains(got, "to stderr") {
		t.Errorf("output = %q, want both streams", got)
	}
	if !strings.Contains(results[0].Reason, "1") {
		t.Errorf("reason = %q, does not name the exit status", results[0].Reason)
	}
	if results[0].Command != "echo to stdout; echo to stderr >&2; exit 1" {
		t.Errorf("command = %q, want what the check ran", results[0].Command)
	}
}

func TestVerifyEmptyOutputRequiresSilenceAsWellAsExitZero(t *testing.T) {
	results := verify(t, t.TempDir(),
		config.Check{Name: "untidy", Run: "echo main.go", Expect: config.ExpectEmptyOutput},
		config.Check{Name: "tidy", Run: "true", Expect: config.ExpectEmptyOutput},
		config.Check{Name: "grumbles", Run: "echo warning >&2", Expect: config.ExpectEmptyOutput},
	)

	if results[0].Passed {
		t.Error("a check that printed passed empty_output")
	}
	if !strings.Contains(results[0].Reason, "output") {
		t.Errorf("reason = %q, does not say what was wrong", results[0].Reason)
	}
	if !results[1].Passed {
		t.Errorf("a silent check failed empty_output: %+v", results[1])
	}
	// The expectation is about what the check prints, which is stdout; a
	// warning on stderr is not output the check produced about its subject.
	if !results[2].Passed {
		t.Errorf("a check that only wrote to stderr failed empty_output: %+v", results[2])
	}
}

func TestVerifyKillsAHungCheckAtItsTimeout(t *testing.T) {
	started := time.Now()

	results := verify(t, t.TempDir(), config.Check{Name: "hang", Run: "sleep 60", Timeout: 200 * time.Millisecond})

	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("the check took %s, so it was not killed at its timeout", elapsed)
	}
	if results[0].Passed {
		t.Error("a check that was killed passed")
	}
	if !strings.Contains(results[0].Reason, "timed out") {
		t.Errorf("reason = %q, does not name the timeout", results[0].Reason)
	}
}

func TestVerifyRunsChecksInTheWorktree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "here.txt"), []byte("yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := verify(t, dir, config.Check{Name: "where", Run: "test -f here.txt"})

	if !results[0].Passed {
		t.Errorf("a check did not run where the work is: %+v", results[0])
	}
}

func TestVerifyWithNoChecksSaysNothingRefusedTheWork(t *testing.T) {
	results, err := command.New().Verify(context.Background(), verifier.Request{WorkingDir: t.TempDir()})

	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != 0 || len(verifier.Failed(results)) != 0 {
		t.Errorf("results = %+v, want none", results)
	}
}
