package command_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vojtechmares/coding-owl/internal/command"
)

// The daemon runs the command itself and reports what it printed, which is
// what goes back to the chat (ADR-0022).
func TestRunCarriesOutTheCommandInTheDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "owl.txt", "owl was here\n")

	got, err := command.Run(context.Background(), dir, []string{"wc", "-c", "owl.txt"})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ExitCode != 0 {
		t.Errorf("exit = %d, want 0", got.ExitCode)
	}
	if !strings.Contains(got.Output, "13") {
		t.Errorf("output = %q, want the size of the file", got.Output)
	}
}

// A command that fails is not a failure of Owl's: its status and what it
// printed are the answer, and the model needs both.
func TestRunReportsAFailingCommandRatherThanFailing(t *testing.T) {
	got, err := command.Run(context.Background(), t.TempDir(), []string{"cat", "missing.txt"})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got.ExitCode == 0 {
		t.Errorf("exit = 0, want the failure the command reported")
	}
	// Standard error is part of what it printed, or a command that only
	// complains would answer with nothing.
	if !strings.Contains(got.Output, "missing.txt") {
		t.Errorf("output = %q, want what the command complained about", got.Output)
	}
}

// What one command may print is bounded: it is held in memory and sent to the
// model, so a command that never stops is stopped here.
func TestRunBoundsWhatItKeeps(t *testing.T) {
	dir := t.TempDir()
	var big strings.Builder
	for at := range 20000 {
		big.WriteString("line ")
		big.WriteString(strings.Repeat("x", 20))
		big.WriteByte('\n')
		_ = at
	}
	write(t, dir, "big.txt", big.String())

	got, err := command.Run(context.Background(), dir, []string{"cat", "big.txt"})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Against a number rather than against the constant: a bound compared with
	// itself is not a bound, and one raised to a gigabyte would still pass.
	if len(got.Output) > 128<<10 {
		t.Errorf("output is %d bytes, want no more than owl keeps", len(got.Output))
	}
	if command.MaxOutput > 128<<10 {
		t.Errorf("MaxOutput is %d bytes, which is more than owl should hold in memory per command",
			command.MaxOutput)
	}
	if len(got.Output) == 0 {
		t.Error("output is empty, want the beginning of what the command printed")
	}
}

// A program that is not there is Owl's failure to report, not a command that
// exited badly: the two read differently to whoever asked.
func TestRunReportsAProgramItCannotFind(t *testing.T) {
	_, err := command.Run(context.Background(), t.TempDir(),
		[]string{"owl-no-such-program-exists", "x"})

	if err == nil {
		t.Fatal("Run of a program that is not installed = nil, want an error")
	}
	if !strings.Contains(err.Error(), "owl-no-such-program-exists") {
		t.Errorf("the error %q does not name the program", err)
	}
}

// The command is given an environment Owl chose rather than the daemon's own:
// whatever the daemon was started with is not the chat's to hand out.
func TestRunGivesTheCommandAnEnvironmentOwlChose(t *testing.T) {
	t.Setenv("OWL_A_SECRET_THE_DAEMON_HOLDS", "hunter2")
	dir := t.TempDir()

	got, err := command.Run(context.Background(), dir, []string{"env"})

	if err != nil {
		t.Skipf("env is not available here: %v", err)
	}
	if strings.Contains(got.Output, "hunter2") {
		t.Errorf("the daemon's own environment reached the command:\n%s", got.Output)
	}
	// It still needs enough to work: git reads its configuration from HOME.
	for _, want := range []string{"PATH=", "HOME="} {
		if !strings.Contains(got.Output, want) {
			t.Errorf("the command was given no %s:\n%s", want, got.Output)
		}
	}
}

// The caller's context ends the command: a chat whose window closed is not one
// to keep running programs for.
func TestRunStopsWithItsCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := command.Run(ctx, t.TempDir(), []string{"wc", "-l"})

	if err == nil {
		t.Fatal("Run with a cancelled caller = nil, want it stopped")
	}
}

// A directory that is not one is refused before anything is started.
func TestRunRefusesADirectoryThatIsNotOne(t *testing.T) {
	dir := t.TempDir()
	at := write(t, dir, "owl.txt", "owl\n")

	if _, err := command.Run(context.Background(), at, []string{"ls"}); err == nil {
		t.Error("Run in a file = nil, want it refused")
	}
	if _, err := command.Run(context.Background(), "", []string{"ls"}); err == nil {
		t.Error("Run with no directory = nil, want it refused")
	}
}

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	at := filepath.Join(dir, name)
	if err := os.WriteFile(at, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", at, err)
	}
	return at
}

// A repository holds files that are not text. What they print crosses a wire
// that carries strings and is shown to a person, so what is not a character is
// replaced rather than carried.
func TestRunKeepsWhatItPrintsReadableAsText(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.png"),
		[]byte{0xff, 0xfe, 0x00, 'o', 'w', 'l', 0x80}, 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	got, err := command.Run(context.Background(), dir, []string{"cat", "logo.png"})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !utf8.ValidString(got.Output) {
		t.Errorf("output = %q, want something a string can carry", got.Output)
	}
	if !strings.Contains(got.Output, "owl") {
		t.Errorf("output = %q, want what was readable kept", got.Output)
	}
}

// A caller that went away and a command that took too long read differently to
// whoever asked, so they are said differently.
func TestRunSaysWhetherItWasStoppedOrTookTooLong(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := command.Run(ctx, t.TempDir(), []string{"wc", "-l"})

	if err == nil {
		t.Fatal("Run with a cancelled caller = nil, want it stopped")
	}
	if strings.Contains(err.Error(), "did not finish within") {
		t.Errorf("the error %q says the command took too long, want it to say the caller went away", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error %q does not carry why it stopped", err)
	}
}
