// Package agent is the vocabulary the two plugin axes share: what an Agent is
// invoked as, and what a running one looks like. A Driver builds an Invocation
// and an Executor starts it (ADR-0018), so both sides name the same things
// without either depending on the other.
package agent

import (
	"io"
	"os"
	"strings"
)

// Invocation is one Agent, ready to be started: the program, its arguments,
// where it runs, and what it takes out of and adds to the environment it
// inherits.
type Invocation struct {
	// Path is the program to run, already resolved.
	Path string
	// Args are its arguments, without the program name.
	Args []string
	// Dir is the working directory, which is the Job's worktree (ADR-0007).
	Dir string
	// Env is added to the environment the Executor starts from, as NAME=value.
	Env []string
	// Unset names variables the Executor takes out of the environment it
	// starts from, before Env is added: what the daemon itself was started
	// with is not always what an Agent may see (ADR-0019).
	Unset []string
	// Permissions are the tool rules the Agent runs with, in the tool's own
	// syntax, so that a Run can record what its Agent was allowed to do
	// (ADR-0035). The Executor does nothing with them; the Driver has already
	// arranged for the tool to read them.
	Permissions []string
}

// Environ is the environment the Agent runs in, given the one it would
// inherit: less what the invocation unsets, plus what it adds. Every place an
// invocation is started builds its environment here, so that what one Driver
// keeps from an Agent is kept from it wherever it runs.
func (inv Invocation) Environ(inherited []string) []string {
	drop := make(map[string]bool, len(inv.Unset))
	for _, name := range inv.Unset {
		drop[name] = true
	}
	env := make([]string, 0, len(inherited)+len(inv.Env))
	for _, kv := range inherited {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			env = append(env, kv)
		}
	}
	return append(env, inv.Env...)
}

// Process is a running Agent.
type Process interface {
	// Stdout is the Agent's structured output, read to end of file.
	Stdout() io.Reader
	// SignalGroup signals the Agent and everything it started. Claude Code
	// spawns test runners, compilers and package managers, and signalling only
	// the Agent would leave those running (ADR-0011).
	SignalGroup(sig os.Signal) error
	// Wait blocks until the Agent exits and returns its exit status. The
	// error is for a process that could not be waited on at all, never for a
	// non-zero status.
	Wait() (int, error)
	// Stderr is whatever the Agent wrote to standard error, kept for the
	// message a failed Run is reported with. It is bounded, and only complete
	// once Wait has returned.
	Stderr() string
}
