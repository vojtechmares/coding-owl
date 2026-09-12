package chat

import (
	"context"
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
