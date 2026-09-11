package gc

// Interleave sets what runs between the collection reading the worktree
// directory and reading the database. Only a test sets it: it is how the
// ordering those two reads depend on can be exercised at all, since from
// outside the package a collection is one call.
//
// It lives in a test file, so nothing outside the tests can reach it.
func (s *Service) Interleave(f func()) { s.interleave = f }
