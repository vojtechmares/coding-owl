// Package agent is the vocabulary the two plugin axes share: what an Agent is
// invoked as, and what a running one looks like. A Driver builds an Invocation
// and an Executor starts it (ADR-0018), so both sides name the same things
// without either depending on the other.
package agent

import (
	"io"
	"os"
)

// Invocation is one Agent, ready to be started: the program, its arguments,
// where it runs and what it adds to the environment it inherits.
type Invocation struct {
	// Path is the program to run, already resolved.
	Path string
	// Args are its arguments, without the program name.
	Args []string
	// Dir is the working directory, which is the Job's worktree (ADR-0007).
	Dir string
	// Env is added to the environment the Executor starts from, as NAME=value.
	Env []string
}

// Process is a running Agent.
type Process interface {
	// Stdout is the Agent's structured output, read to end of file.
	Stdout() io.Reader
	// Signal asks the Agent to stop, or worse.
	Signal(sig os.Signal) error
	// Wait blocks until the Agent exits and returns its exit status. The
	// error is for a process that could not be waited on at all, never for a
	// non-zero status.
	Wait() (int, error)
	// Stderr is whatever the Agent wrote to standard error, kept for the
	// message a failed Run is reported with. It is bounded, and only complete
	// once Wait has returned.
	Stderr() string
}
