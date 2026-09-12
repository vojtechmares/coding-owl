package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// nothingSaid is a view that answers nothing: these tests are about what
// happens before anything is read or run.
type nothingSaid struct{ where string }

func (v *nothingSaid) Projects(context.Context) (string, error)            { return "", nil }
func (v *nothingSaid) Jobs(context.Context, bool) (string, error)          { return "", nil }
func (v *nothingSaid) Job(context.Context, int64) (string, error)          { return "", nil }
func (v *nothingSaid) RunLog(context.Context, int64, int) (string, error)  { return "", nil }
func (v *nothingSaid) JobDiff(context.Context, int64, int) (string, error) { return "", nil }
func (v *nothingSaid) Where(context.Context, string) (string, error)       { return v.where, nil }

// A command nobody answers runs nothing, and the model is told that nobody
// answered rather than that the user refused: those are different things, and
// a model given the wrong one tells the user something untrue (ADR-0022).
func TestACommandNobodyAnswersIsNotRun(t *testing.T) {
	dir := t.TempDir()
	s := NewService(nil, nil, NewTools(&nothingSaid{where: dir}))
	s.window = 50 * time.Millisecond
	var proposals int
	emit := func(d Delta) error {
		if d.Proposal != nil {
			proposals++
		}
		if d.Ran != nil {
			t.Error("a command nobody answered ran")
		}
		return nil
	}

	got, err := s.runCommand(context.Background(), 1,
		call(t, `{"command":"ls","directory":"`+dir+`"}`), emit)

	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if proposals != 1 {
		t.Errorf("the user was asked %d times, want once", proposals)
	}
	if !got.Failed {
		t.Errorf("the model was told %+v, want the command refused", got)
	}
	if !strings.Contains(got.Text, "nobody answered") {
		t.Errorf("the model was told %q, want it to say nobody answered", got.Text)
	}
	if strings.Contains(got.Text, "the user refused") {
		t.Errorf("the model was told %q, which nobody did", got.Text)
	}
}

// And one the user refuses says so, which is a different sentence.
func TestACommandTheUserRefusesSaysTheUserRefusedIt(t *testing.T) {
	dir := t.TempDir()
	s := NewService(nil, nil, NewTools(&nothingSaid{where: dir}))
	answered := make(chan struct{})
	emit := func(d Delta) error {
		if d.Proposal == nil {
			return nil
		}
		id := d.Proposal.ID
		go func() {
			defer close(answered)
			if err := s.AnswerCommand(id, Refuse); err != nil {
				t.Errorf("AnswerCommand: %v", err)
			}
		}()
		return nil
	}

	got, err := s.runCommand(context.Background(), 1,
		call(t, `{"command":"ls","directory":"`+dir+`"}`), emit)
	<-answered

	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if !got.Failed || !strings.Contains(got.Text, "the user refused it") {
		t.Errorf("the model was told %+v, want it to say the user refused it", got)
	}
}

// call is one tool call for the command tool, as a model would make it.
func call(t *testing.T, input string) ToolCall {
	t.Helper()
	return ToolCall{ID: "call-1", Name: toolRunCommand, Input: []byte(input)}
}

// One command carries one id from the asking to the running, so that what ran
// can be matched to what was agreed to.
func TestACommandCarriesOneIdFromTheAskingToTheRunning(t *testing.T) {
	dir := t.TempDir()
	s := NewService(nil, nil, NewTools(&nothingSaid{where: dir}))
	var proposed, ran string
	emit := func(d Delta) error {
		if d.Proposal != nil {
			proposed = d.Proposal.ID
			go func() {
				if err := s.AnswerCommand(proposed, AllowOnce); err != nil {
					t.Errorf("AnswerCommand: %v", err)
				}
			}()
		}
		if d.Ran != nil {
			ran = d.Ran.ID
		}
		return nil
	}

	got, err := s.runCommand(context.Background(), 1,
		call(t, `{"command":"ls","directory":"`+dir+`"}`), emit)

	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if got.Failed {
		t.Fatalf("the command was refused: %+v", got)
	}
	if proposed == "" || ran != proposed {
		t.Errorf("the command that ran is %q and the one that was asked about is %q, want the same one",
			ran, proposed)
	}
	// And the model's own call id is what answers the model, which is a
	// different thing and must not have been swapped for it.
	if got.CallID != "call-1" {
		t.Errorf("the result answers %q, want the call the model made", got.CallID)
	}
}

// The directory is read before the user is asked and the user takes their
// time, so what is about to run is checked against how the directory is now.
func TestACommandIsCheckedAgainAfterTheWait(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("hunter2\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("nothing here\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	s := NewService(nil, nil, NewTools(&nothingSaid{where: dir}))
	emit := func(d Delta) error {
		if d.Proposal == nil {
			return nil
		}
		// While the prompt is open, what was a file becomes a link out.
		if err := os.Remove(filepath.Join(dir, "notes.txt")); err != nil {
			return err
		}
		if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "notes.txt")); err != nil {
			return err
		}
		id := d.Proposal.ID
		go func() {
			if err := s.AnswerCommand(id, AllowOnce); err != nil {
				t.Errorf("AnswerCommand: %v", err)
			}
		}()
		return nil
	}

	got, err := s.runCommand(context.Background(), 1,
		call(t, `{"command":"cat notes.txt","directory":"`+dir+`"}`), emit)

	if err != nil {
		t.Fatalf("runCommand: %v", err)
	}
	if !got.Failed {
		t.Fatalf("the command ran against what the directory became: %+v", got)
	}
	if strings.Contains(got.Text, "hunter2") {
		t.Errorf("what is outside the directory was read: %q", got.Text)
	}
	if !strings.Contains(got.Text, "outside the working directory") {
		t.Errorf("the model was told %q, want it to say the link leaves the working directory", got.Text)
	}
}
