// Package shell runs a Project's own commands - its Verification checks and
// its setup commands - through the system shell.
//
// The shell is allowed here and forbidden for the chat's commands (ADR-0022)
// because of who writes the string: these are written by the user and read
// from the Project's base branch (ADR-0014, ADR-0030) before the Agent that is
// being judged by them starts. A Job's worktree shares the repository's refs,
// so an Agent can move that branch while it works - which is why what runs is
// read once, up front, and carried rather than read again. If a command here
// ever becomes model-authored, the reasoning has gone and this package should
// not be used for it.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// shellPath is the interpreter, which is what makes `a | b`, `&&` and
// `$(...)` mean what the user expects them to mean.
const shellPath = "/bin/sh"

// killDelay is how long a command has to exit after its timeout before it is
// killed outright.
const killDelay = 5 * time.Second

// maxOutput bounds what one command's output can cost, since it is kept for a
// report a person reads.
const maxOutput = 64 << 10

// Result is what running one command said.
type Result struct {
	// ExitCode is the status it exited with, or NoExitCode when it never got
	// far enough to have one.
	ExitCode int
	// Stdout is what it printed, up to the limit above.
	Stdout string
	// Output is everything it printed, standard error included, in the order
	// the two streams were written.
	Output string
	// TimedOut reports whether the command outlasted its own timeout.
	TimedOut bool
	// Cancelled reports whether what asked for the command gave up first: the
	// daemon stopping, or the caller hanging up. It is not a verdict on the
	// command.
	Cancelled bool
}

// NoExitCode is the status of a command that never ran.
const NoExitCode = -1

// Run executes command in dir, giving it timeout to finish. err is returned
// only when the command could not be run at all, which is a different thing
// from it running and failing.
func Run(ctx context.Context, dir, command string, timeout time.Duration) (Result, error) {
	// A command with nowhere to run would inherit the daemon's own working
	// directory, which is wherever the user started it. The worktree is where
	// a Project's commands belong (ADR-0007), so it is required.
	if dir == "" {
		return Result{ExitCode: NoExitCode}, errors.New("a command must be given a directory to run in")
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shellPath, "-c", command)
	cmd.Dir = dir
	// The shell gets a process group of its own, so that a check which starts
	// a test runner takes it with it when it is stopped.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The command inherits the user's environment, the way it would if they
	// ran it themselves (ADR-0006).
	cmd.Env = os.Environ()
	// Nobody is at a terminal, so a command that reads sees end of file rather
	// than waiting until morning.
	cmd.Stdin = nil
	cmd.Cancel = func() error { return signalGroup(cmd, syscall.SIGTERM) }
	cmd.WaitDelay = killDelay

	both := &interleaved{}
	stdout := &capped{also: both}
	cmd.Stdout = stdout
	cmd.Stderr = &capped{also: both}

	err := cmd.Run()
	res := Result{
		ExitCode: 0,
		Stdout:   stdout.String(),
		Output:   both.String(),
		// Whose deadline ran out decides which of these it was: the command's
		// own timeout, or whoever was waiting for it.
		TimedOut:  errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil,
		Cancelled: parent.Err() != nil,
	}
	if err == nil {
		return res, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	res.ExitCode = NoExitCode
	return res, fmt.Errorf("running %s: %w", command, err)
}

// signalGroup signals the command's whole process group, so that whatever it
// started stops with it rather than outliving the check and holding the
// worktree open.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		// The group may already be gone, which is not a failure to report.
		return cmd.Process.Signal(sig)
	}
	return nil
}

// interleaved collects both streams in the order they were written, which is
// the order someone reading the report expects them in.
type interleaved struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *interleaved) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := maxOutput - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil
}

func (w *interleaved) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// capped keeps one stream, bounded, and passes it on to the combined one.
type capped struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	also *interleaved
}

func (w *capped) Write(p []byte) (int, error) {
	w.mu.Lock()
	if room := maxOutput - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
		} else {
			w.buf.Write(p)
		}
	}
	w.mu.Unlock()
	return w.also.Write(p)
}

func (w *capped) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
