// Package host runs Agents as child processes of the daemon (ADR-0006). Owl
// owns the process: its stdout, its exit status and its signals (ADR-0012).
// There is no sandbox here - the Job's worktree is the bound (ADR-0007).
package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/vojtechmares/coding-owl/internal/agent"
)

// stderrLimit is how much of an Agent's standard error is kept for the message
// a failed Run is reported with. It is a diagnostic, not a log.
const stderrLimit = 8 << 10

// killDelay is how long a cancelled Agent has to exit after SIGTERM before it
// is killed outright.
const killDelay = 5 * time.Second

// Executor starts Agents on this machine.
type Executor struct{}

// New returns the host Executor.
func New() *Executor { return &Executor{} }

// Name identifies the placement.
func (*Executor) Name() string { return "host" }

// Start launches the Agent as a direct child of the daemon. Cancelling ctx
// asks it to stop with SIGTERM, and kills it if it will not.
func (*Executor) Start(ctx context.Context, inv agent.Invocation) (agent.Process, error) {
	cmd := exec.CommandContext(ctx, inv.Path, inv.Args...)
	cmd.Dir = inv.Dir
	// The Agent inherits the user's already-authenticated environment
	// (ADR-0006); the invocation only adds to it.
	cmd.Env = append(os.Environ(), inv.Env...)
	// Nobody is at a terminal, so there is nothing to read: an Agent that
	// waits on stdin sees end of file rather than hanging until morning.
	cmd.Stdin = nil
	// A cancelled Run is asked to stop before it is killed, so an Agent that
	// commits as it goes (ADR-0017) gets to finish the commit it is making.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = killDelay

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("capturing the agent's output: %w", err)
	}
	p := &process{cmd: cmd, stdout: stdout}
	cmd.Stderr = &p.stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", inv.Path, err)
	}
	return p, nil
}

// process is one running Agent.
type process struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr limitedBuffer
}

// Stdout is the Agent's structured output.
func (p *process) Stdout() io.Reader { return p.stdout }

// Signal passes a signal to the Agent.
func (p *process) Signal(sig os.Signal) error { return p.cmd.Process.Signal(sig) }

// Wait blocks until the Agent exits and returns its exit status. A non-zero
// status is an answer, not an error.
func (p *process) Wait() (int, error) {
	err := p.cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// Stderr is what the Agent wrote to standard error, up to the limit above.
func (p *process) Stderr() string { return p.stderr.String() }

// limitedBuffer keeps the first stderrLimit bytes and counts the rest away.
// os/exec writes to it from the child's own goroutine, so it locks.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := stderrLimit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
