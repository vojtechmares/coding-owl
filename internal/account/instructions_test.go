package account_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/account"
)

// instructionsFile is what the claude-code Driver calls the file, which is
// what an Account on the default Driver gets.
const instructionsFile = "CLAUDE.md"

func TestInstructionsAreEmptyForAnAccountNobodyHasWrittenAny(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")

	in, err := f.svc.Instructions(context.Background(), "work")

	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if in.Text != "" {
		t.Errorf("a fresh account's instructions say %q, want nothing", in.Text)
	}
	if in.File != instructionsFile {
		t.Errorf("the file is %q, want the one the account's tool reads", in.File)
	}
	if want := filepath.Join(f.dataDir, "accounts", "work", instructionsFile); in.Path != want {
		t.Errorf("the file is at %s, want it beside the account's own configuration at %s", in.Path, want)
	}
}

func TestInstructionsAreWrittenIntoTheAccountsOwnDirectory(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")

	in, err := f.svc.SetInstructions(context.Background(), "work", "Write commit messages in the imperative.")

	if err != nil {
		t.Fatalf("SetInstructions: %v", err)
	}
	path := filepath.Join(a.ConfigDir, instructionsFile)
	if in.Path != path {
		t.Errorf("the instructions were saved to %s, want %s", in.Path, path)
	}
	// The tool reads the file itself, so what is on disk is what matters
	// rather than what the call reported.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back what was saved: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "Write commit messages in the imperative." {
		t.Errorf("the file says %q, want what was written", got)
	}
}

func TestInstructionsComeBackAsTheyWereWritten(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	want := "Never touch the migrations.\n\nCommit as you go.\n"

	if _, err := f.svc.SetInstructions(context.Background(), "work", want); err != nil {
		t.Fatalf("SetInstructions: %v", err)
	}
	in, err := f.svc.Instructions(context.Background(), "work")

	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if in.Text != want {
		t.Errorf("the instructions read back as %q, want %q", in.Text, want)
	}
}

func TestTheFileIsTheAccountsOwnerAlone(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")

	if _, err := f.svc.SetInstructions(context.Background(), "work", "anything"); err != nil {
		t.Fatalf("SetInstructions: %v", err)
	}

	info, err := os.Stat(filepath.Join(a.ConfigDir, instructionsFile))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	// It sits in a directory holding a coding tool's whole credential state,
	// and is nobody's but its owner's like everything else there (ADR-0019).
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("the file is mode %o, want 600", perm)
	}
}

func TestBlankInstructionsTakeTheFileAway(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")
	if _, err := f.svc.SetInstructions(context.Background(), "work", "something"); err != nil {
		t.Fatalf("SetInstructions: %v", err)
	}

	if _, err := f.svc.SetInstructions(context.Background(), "work", "   \n\t\n"); err != nil {
		t.Fatalf("clearing them: %v", err)
	}

	// An Account with nothing to say should read as one whose tool finds no
	// file at all, which is what it was before anybody wrote one.
	if _, err := os.Lstat(filepath.Join(a.ConfigDir, instructionsFile)); !os.IsNotExist(err) {
		t.Errorf("the file is still there after being cleared (%v), want it gone", err)
	}
	in, err := f.svc.Instructions(context.Background(), "work")
	if err != nil {
		t.Fatalf("Instructions: %v", err)
	}
	if in.Text != "" {
		t.Errorf("the cleared instructions say %q, want nothing", in.Text)
	}
}

func TestInstructionsTooLongAreRefused(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")

	_, err := f.svc.SetInstructions(context.Background(), "work", strings.Repeat("a", (64<<10)+1))

	var invalid *account.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("SetInstructions gave %v, want a refusal the user can act on", err)
	}
	// They are read into every Run on the Account, so the refusal has to say
	// that rather than just naming a number.
	if !strings.Contains(err.Error(), "every run") {
		t.Errorf("the refusal says %q, want it to say why the size matters", err)
	}
}

func TestInstructionsWithANullAreRefused(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")

	_, err := f.svc.SetInstructions(context.Background(), "work", "before\x00after")

	var invalid *account.InvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("SetInstructions gave %v, want a refusal the user can act on", err)
	}
}

func TestASymlinkInTheFilesPlaceIsRefusedRatherThanWrittenThrough(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")
	// A user's own file, outside the Account's directory entirely.
	theirs := filepath.Join(t.TempDir(), "their-own.md")
	if err := os.WriteFile(theirs, []byte("their own instructions\n"), 0o600); err != nil {
		t.Fatalf("writing their file: %v", err)
	}
	if err := os.Symlink(theirs, filepath.Join(a.ConfigDir, instructionsFile)); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := f.svc.SetInstructions(context.Background(), "work", "Owl's own text")

	if err == nil {
		t.Fatal("SetInstructions wrote through a symlink, want it refused")
	}
	// Following it would have Owl write outside the Account's directory,
	// into whatever the user pointed it at, which is the one thing an
	// Account exists to keep separate (ADR-0019).
	data, readErr := os.ReadFile(theirs)
	if readErr != nil {
		t.Fatalf("reading their file: %v", readErr)
	}
	if got := string(data); got != "their own instructions\n" {
		t.Errorf("their own file now says %q; Owl wrote through the link", got)
	}
}

func TestAnOverLongFileIsRefusedRatherThanShownCutDown(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")
	// Owl is not the only thing that can write there: a person with an editor
	// can, and so can an Agent running on the Account (ADR-0037).
	theirs := strings.Repeat("a", (64<<10)+1)
	if err := os.WriteFile(filepath.Join(a.ConfigDir, instructionsFile), []byte(theirs), 0o600); err != nil {
		t.Fatalf("writing an over-long file by hand: %v", err)
	}

	_, err := f.svc.Instructions(context.Background(), "work")

	// Showing the first part of it silently would let `instructions edit`
	// hand that shortened text to an editor and save it back, and the rest of
	// what they wrote would be gone with nothing having said so.
	if err == nil {
		t.Fatal("an over-long file was read, want it refused rather than cut down")
	}
	var invalid *account.InvalidError
	if !errors.As(err, &invalid) {
		t.Errorf("Instructions gave %v, want a refusal the user can act on", err)
	}
	// It is their file and Owl will not shorten it, so the refusal has to say
	// what to do about it.
	if !strings.Contains(err.Error(), "shorten it in place") {
		t.Errorf("the refusal says %q, want it to say what the user can do", err)
	}
	data, readErr := os.ReadFile(filepath.Join(a.ConfigDir, instructionsFile))
	if readErr != nil {
		t.Fatalf("reading their file back: %v", readErr)
	}
	if len(data) != len(theirs) {
		t.Errorf("their file is now %d bytes, want it left at %d", len(data), len(theirs))
	}
}

func TestASymlinkInTheDirectorysPlaceIsRefusedToo(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")
	// Their own setup, which is the thing an Account exists to keep separate.
	theirs := t.TempDir()
	if err := os.WriteFile(filepath.Join(theirs, instructionsFile), []byte("their own\n"), 0o600); err != nil {
		t.Fatalf("writing their file: %v", err)
	}
	// Checking only the file leaves the guarantee one level short: at the far
	// end of a link the file looks perfectly ordinary.
	if err := os.RemoveAll(a.ConfigDir); err != nil {
		t.Fatalf("clearing the account's directory: %v", err)
	}
	if err := os.Symlink(theirs, a.ConfigDir); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := f.svc.SetInstructions(context.Background(), "work", "Owl's own text")

	if err == nil {
		t.Fatal("SetInstructions wrote through a linked directory, want it refused")
	}
	data, readErr := os.ReadFile(filepath.Join(theirs, instructionsFile))
	if readErr != nil {
		t.Fatalf("reading their file: %v", readErr)
	}
	if got := string(data); got != "their own\n" {
		t.Errorf("their own file now says %q; Owl wrote through the linked directory", got)
	}
}

func TestTheRefusalSaysWhatIsInTheFilesPlaceInWords(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), filepath.Join(a.ConfigDir, instructionsFile)); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := f.svc.SetInstructions(context.Background(), "work", "text")

	if err == nil {
		t.Fatal("SetInstructions accepted a link, want it refused")
	}
	// The mode's own type string is a row of dashes with a letter in it,
	// which is not something to show somebody who has to decide what to do.
	if !strings.Contains(err.Error(), "a symbolic link") {
		t.Errorf("the refusal says %q, want it to name what is there in words", err)
	}
}

func TestTextThatWouldNotReadBackIsRefused(t *testing.T) {
	f := newFixture(t)
	f.added(t, "work")
	// Exactly the bound, with the trailing newline still to be added: a save
	// that fits and then reads back as too long would be the worst of both.
	atTheBound := strings.Repeat("a", 64<<10)

	if _, err := f.svc.SetInstructions(context.Background(), "work", atTheBound); err == nil {
		t.Fatal("text that would be over the bound once written was accepted, want it refused")
	}

	// One shorter leaves room for the newline, and must still be accepted.
	justUnder := strings.Repeat("a", (64<<10)-1)
	if _, err := f.svc.SetInstructions(context.Background(), "work", justUnder); err != nil {
		t.Fatalf("text that fits once written was refused: %v", err)
	}
	if _, err := f.svc.Instructions(context.Background(), "work"); err != nil {
		t.Errorf("what was saved cannot be read back: %v", err)
	}
}

func TestInstructionsForAnAccountNobodyHasAreRefused(t *testing.T) {
	f := newFixture(t)

	if _, err := f.svc.Instructions(context.Background(), "nobody"); err == nil {
		t.Error("Instructions answered for an account nobody has, want it refused")
	}
	if _, err := f.svc.SetInstructions(context.Background(), "nobody", "text"); err == nil {
		t.Error("SetInstructions wrote for an account nobody has, want it refused")
	}
}

func TestSavedInstructionsEndInANewline(t *testing.T) {
	f := newFixture(t)
	a := f.added(t, "work")

	if _, err := f.svc.SetInstructions(context.Background(), "work", "no newline at the end"); err != nil {
		t.Fatalf("SetInstructions: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(a.ConfigDir, instructionsFile))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("the file ends %q, want the line ending a text file has", string(data))
	}
}
