package run

// Close cancels the Service's context before it sets the flag that refuses new
// Runs, so there is a moment when the first is true and the second is not. A
// Run started in it could never begin. Only a test inside the package can put
// the Service in that state deliberately.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/store"
)

func TestStartRefusesWhileTheContextIsDoneAndTheFlagIsNotYetSet(t *testing.T) {
	// A real store, so that a Start which is not refused gets far enough to
	// answer rather than to panic.
	st, _, err := store.Open(filepath.Join(t.TempDir(), "owl.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := NewService(Options{Store: st})
	// Exactly what Close does first, and nothing of what it does after.
	s.cancel()
	if s.stopping {
		t.Fatal("the flag is already set, so this is not the moment being tested")
	}

	_, _, started, err := s.Start(context.Background())

	if started {
		t.Error("a run started while the daemon's context was already done")
	}
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("Start = %v, want a RefusedError", err)
	}
}
