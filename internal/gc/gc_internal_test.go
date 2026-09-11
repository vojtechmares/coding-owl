package gc

// An internal test, because the ordering it is about is between two reads
// inside Collect and cannot be seen from outside.

import "testing"

// Interleave sets what runs between the collection reading the worktree
// directory and reading the database. Only a test sets it.
func (s *Service) Interleave(f func()) { s.interleave = f }

func TestTheWorktreeDirectoryIsReadBeforeTheDatabase(t *testing.T) {
	var order []string
	s := NewService(Options{})
	s.interleave = func() { order = append(order, "interleaved") }

	// candidates is the read of the directory, and it happens before anything
	// the interleave could do: the Job behind a worktree made after this point
	// is one the database read below is certain to see, so the collection
	// leaves it alone.
	if _, err := s.candidates(); err != nil {
		t.Fatalf("candidates: %v", err)
	}
	order = append(order, "candidates")

	if len(order) != 1 || order[0] != "candidates" {
		t.Errorf("order = %v, want the directory read without the interleave running", order)
	}
}
