package account

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/drivers"
)

// maxInstructions bounds an Account's standing instructions. They are read
// into the context of every Run on that Account, so a file nobody meant to
// write is a file that quietly costs every Job on it; and they cross the
// socket and a desktop editor, neither of which wants a file of unbounded
// size. It is generous for prose and small for anything else.
const maxInstructions = 64 << 10

// instructionsMode is what the file is written with. It sits in a directory
// that holds a coding tool's whole credential state, and is nobody's but its
// owner's, like everything else there (ADR-0019).
const instructionsMode = 0o600

// Instructions are an Account's standing instructions: text every Run on that
// Account reads, whichever Project the Run is for (ADR-0037).
type Instructions struct {
	// Account is whose they are, as the Account is recorded rather than as it
	// was asked for.
	Account string
	// Driver is the coding tool that Account is on, which is what decides
	// whether there is a file at all and what it is called.
	Driver string
	// File is what the Account's Driver calls the file, and is empty for a
	// Driver whose tool reads no such file. A caller with an empty File has
	// nothing to show and nothing to save.
	File string
	// Path is where the file is, so a command can say where what it printed
	// came from. It is empty when File is.
	Path string
	// Text is what the file says, and is empty for an Account that has not
	// been given any.
	Text string
}

// Instructions returns an Account's standing instructions.
func (s *Service) Instructions(ctx context.Context, name string) (Instructions, error) {
	at, err := s.instructionsPath(ctx, name)
	if err != nil {
		return Instructions{}, err
	}
	if at.File == "" {
		return at, nil
	}
	text, err := readInstructions(at.Path)
	if err != nil {
		return Instructions{}, err
	}
	at.Text = text
	return at, nil
}

// SetInstructions writes an Account's standing instructions, and returns them
// as they now stand. Text that is empty once its surrounding blank space is
// taken off takes the file away rather than leaving an empty one behind: an
// Account with nothing to say should read as one whose Driver's tool finds no
// file at all, which is what it was before anybody wrote one.
func (s *Service) SetInstructions(ctx context.Context, name, text string) (Instructions, error) {
	at, err := s.instructionsPath(ctx, name)
	if err != nil {
		return Instructions{}, err
	}
	if at.File == "" {
		return Instructions{}, invalid(
			"account %s is on the %s driver, whose tool reads no standing instructions", at.Account, at.Driver)
	}
	// Measured as it will be written, trailing newline included, so that text
	// which is accepted here is text that can be read back: a save that fits
	// and then reads back as too long would be the worst of both.
	if size := len(ensureTrailingNewline(text)); size > maxInstructions {
		return Instructions{}, invalid(
			"those instructions come to %d bytes; keep them to %d or fewer - they are read into every run on account %s",
			size, maxInstructions, at.Account)
	}
	// A null would end the file early for whatever reads it, so what was
	// saved and what is read would differ with nothing to show for it.
	if strings.ContainsRune(text, '\x00') {
		return Instructions{}, invalid("an account's instructions may not hold a null byte")
	}
	if strings.TrimSpace(text) == "" {
		return at, removeInstructions(at)
	}
	if err := writeInstructions(at, text); err != nil {
		return Instructions{}, err
	}
	// What was written rather than what was typed, so that saving and then
	// reading back agree: the file is given the line ending a text file has,
	// and a caller showing what it saved would otherwise differ from the same
	// caller showing it again a moment later.
	at.Text = ensureTrailingNewline(text)
	return at, nil
}

// instructionsPath is where an Account's instructions live, with File and
// Path empty for a Driver whose tool reads none. The Account is looked up so
// that a name nobody has is refused as one, and so the file is named by the
// Driver that Account is actually on rather than by the default.
func (s *Service) instructionsPath(ctx context.Context, name string) (Instructions, error) {
	row, err := s.store.GetAccount(ctx, name)
	if err != nil {
		return Instructions{}, err
	}
	at := Instructions{Account: row.Name, Driver: row.Driver}
	file := drivers.InstructionsFile(row.Driver)
	if file == "" {
		return at, nil
	}
	at.File = file
	at.Path = filepath.Join(row.ConfigDir, file)
	return at, nil
}

// readInstructions is what the file says, and nothing for one that is not
// there: an Account nobody has written instructions for has none, which is an
// answer rather than a failure.
func readInstructions(path string) (string, error) {
	if err := regularFile(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading the account's %s: %w", filepath.Base(path), err)
	}
	// Bounded on the way out as well as on the way in, because the file is on
	// disk and Owl is not the only thing that can write there: a person with
	// an editor can, and so can an Agent running on the Account (ADR-0037).
	//
	// An over-long file is refused rather than cut down to the bound. Showing
	// the first part of it silently would be bad enough on its own - it can
	// split a character in half, and what is printed would not be what the
	// tool reads - but `owl account instructions edit` would then hand that
	// shortened text to an editor and save it back, and the rest of what the
	// user wrote would be gone with nothing having said so.
	if len(data) > maxInstructions {
		return "", invalid(
			"%s is %d bytes, and Owl reads at most %d; it is too long to show or edit here, "+
				"so shorten it in place and try again", path, len(data), maxInstructions)
	}
	return string(data), nil
}

// writeInstructions puts the text in place in one step: it is written in full
// to a file of its own beside the real one and then moved over it, so that a
// Run starting mid-save reads either what was there before or what was just
// written, and never half of either.
func writeInstructions(at Instructions, text string) error {
	dir := filepath.Dir(at.Path)
	// The directory is checked before the file is, because checking only the
	// file leaves the guarantee one level short: a link in the directory's
	// place is followed by everything below - the temp file is made through
	// it, the rename lands through it, and looking at the file without
	// following links still sees an ordinary file at the far end. Owl would
	// be writing into wherever the link points, which for an Account is the
	// user's own setup.
	//
	// The actor to keep in mind is not a stranger: it is an Agent running on
	// this Account, which is granted unrestricted writes and knows where its
	// configuration directory is (ADR-0035, ADR-0037).
	if err := regularDir(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := ensureDir(dir); err != nil {
		return err
	}
	if err := regularFile(at.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	tmp, err := os.CreateTemp(dir, at.File+".*.tmp")
	if err != nil {
		return fmt.Errorf("saving the account's %s: %w", at.File, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	// CreateTemp makes the file the owner's alone already; this is the mode
	// the file must have, said out loud.
	if err := tmp.Chmod(instructionsMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("saving the account's %s: %w", at.File, err)
	}
	if _, err := tmp.WriteString(ensureTrailingNewline(text)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("saving the account's %s: %w", at.File, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("saving the account's %s: %w", at.File, err)
	}
	if err := os.Rename(tmp.Name(), at.Path); err != nil {
		return fmt.Errorf("saving the account's %s: %w", at.File, err)
	}
	return nil
}

// removeInstructions takes the file away. One that is not there is what was
// asked for, not a failure.
func removeInstructions(at Instructions) error {
	if err := regularFile(at.Path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.Remove(at.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clearing the account's %s: %w", at.File, err)
	}
	return nil
}

// regularFile refuses a name that is taken by something Owl must not write
// through. It looks without following a link on purpose: a link is the user's,
// and following one would have Owl write wherever it points - outside the
// Account's directory, into their own setup, which is the one thing an
// Account exists to keep separate (ADR-0019).
func regularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return invalid("%s is not a regular file; it is %s, and Owl will not write through it", path, kindOfFile(info))
	}
	return nil
}

// regularDir refuses a directory that is not one, on the same terms and for
// the same reason.
func regularDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return invalid("%s is not a directory; it is %s, and Owl will not write through it", path, kindOfFile(info))
	}
	return nil
}

// kindOfFile says what is in a name's place in words. The mode's own type
// string is a row of dashes with a letter in it, which is not something to
// show somebody who has to decide what to do about it.
func kindOfFile(info fs.FileInfo) string {
	switch mode := info.Mode(); {
	case mode&fs.ModeSymlink != 0:
		return "a symbolic link"
	case mode.IsDir():
		return "a directory"
	case mode&fs.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&fs.ModeSocket != 0:
		return "a socket"
	case mode&fs.ModeDevice != 0:
		return "a device"
	default:
		return "not a regular file"
	}
}

// ensureTrailingNewline gives the file the line ending a text file has, so
// that what an editor saves and what a person would have typed agree.
func ensureTrailingNewline(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}
