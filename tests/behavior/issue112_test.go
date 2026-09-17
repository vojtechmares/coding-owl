package behavior_test

// Behavior tests for issue #112. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-112.md. The file's name, where it belongs and the text
// a scenario writes are issue111_test.go's, shared with the CLI's scenarios for
// the same feature so that both surfaces are pinned to one file in one place. They drive the desktop app's Go side, the
// internal/desktop package, in-process over the daemon's socket, and confirm
// what it did by reading the Account's own configuration directory: the app is
// a pure view over the daemon (ADR-0009), so the file is the daemon's proof
// that the save went somewhere a Run will read.

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestS1InstructionsSavedInTheAppComeBackThroughIt(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	app, ev := desktopApp(t, l)
	path := instructionsAt(l, "work")

	before, err := app.AccountInstructions("work")
	if err != nil {
		t.Fatal(err)
	}
	if before.File != instructionsFile || before.Path != path {
		t.Errorf("the app reports %q at %q, want %s at %s", before.File, before.Path, instructionsFile, path)
	}
	if before.Text != "" {
		t.Errorf("an account nobody has written instructions for reads %q, want none", before.Text)
	}

	written, err := app.SaveAccountInstructions("work", standingInstructions)
	if err != nil {
		t.Fatal(err)
	}
	if written.Text != standingInstructions {
		t.Errorf("saving returned %q, want what was typed", written.Text)
	}

	reopened, _ := desktopApp(t, l)
	got, err := reopened.AccountInstructions("work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != standingInstructions {
		t.Errorf("a reopened app reads %q, want %q", got.Text, standingInstructions)
	}
	if got.Path != path {
		t.Errorf("a reopened app reports them at %q, want %q", got.Path, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the app saved nothing in the account's configuration directory: %v", err)
	}
	if string(data) != standingInstructions {
		t.Errorf("%s holds %q, want %q", path, data, standingInstructions)
	}
	ev.none(t)
}

func TestS2BlankInstructionsClearThemAndTakeTheFileAway(t *testing.T) {
	l, _ := accountLayout(t)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	app, _ := desktopApp(t, l)
	path := instructionsAt(l, "work")
	if _, err := app.SaveAccountInstructions("work", standingInstructions); err != nil {
		t.Fatal(err)
	}

	cleared, err := app.SaveAccountInstructions("work", "  \n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cleared.Text) != "" {
		t.Errorf("clearing returned %q, want nothing", cleared.Text)
	}

	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there after the instructions were cleared: %v", path, err)
	}
	got, err := app.AccountInstructions("work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "" {
		t.Errorf("after clearing, the app reads %q, want none", got.Text)
	}
}
