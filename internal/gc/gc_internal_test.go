package gc

// Interleave sets what runs between the collection reading the worktree
// directory and reading the database, and BeforeDeciding what runs between
// reading the database and acting on it. Only a test sets them: they are how
// the ordering and the locking a collection depends on can be exercised at
// all, since from outside the package a collection is one call.
//
// They live in a test file, so nothing outside the tests can reach them.
func (s *Service) Interleave(f func()) { s.interleave = f }

// BeforeDeciding sets what runs after the collection has read the database and
// before it decides anything.
func (s *Service) BeforeDeciding(f func()) { s.beforeDeciding = f }
