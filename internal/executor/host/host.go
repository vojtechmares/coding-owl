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
	"sync/atomic"
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
	// An empty Dir is not an error to os/exec: it means the daemon's own
	// working directory, which is wherever the user started it - very possibly
	// the checkout an Agent must never touch. With no sandbox, the worktree is
	// the only structural bound there is (ADR-0006, ADR-0007), so it is
	// required rather than assumed.
	if inv.Dir == "" {
		return nil, errors.New("an agent must be given a working directory to run in")
	}
	cmd := exec.CommandContext(ctx, inv.Path, inv.Args...)
	cmd.Dir = inv.Dir
	// The Agent inherits the user's already-authenticated environment
	// (ADR-0006); the invocation only adds to it.
	cmd.Env = append(os.Environ(), inv.Env...)
	// Nobody is at a terminal, so there is nothing to read: an Agent that
	// waits on stdin sees end of file rather than hanging until morning.
	cmd.Stdin = nil
	// The Agent leads a process group of its own, so that stopping it stops
	// everything it started rather than orphaning a compile (ADR-0011).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A cancelled Run is asked to stop before it is killed, so an Agent that
	// commits as it goes (ADR-0017) gets to finish the commit it is making.
	cmd.Cancel = func() error { return signalGroup(cmd, syscall.SIGTERM) }
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
	// reaped is set once Wait has returned. After that the pid is the
	// system's to hand out again, and signalling it would be signalling
	// somebody else.
	reaped atomic.Bool
}

// Stdout is the Agent's structured output.
func (p *process) Stdout() io.Reader { return p.stdout }

// Pid is the Agent's own pid, which is also the id of the process group it
// leads.
func (p *process) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// SignalGroup passes a signal to the Agent and everything it started.
func (p *process) SignalGroup(sig os.Signal) error {
	signal, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("%v cannot be sent to a process group", sig)
	}
	// Signalling a group by its negated pid goes straight to the kernel,
	// without the guard os.Process.Signal keeps against a process that has
	// been waited for. That guard is the whole reason a reaped pid is not
	// signalled: the system is free to give the number to somebody else.
	if p.reaped.Load() {
		return os.ErrProcessDone
	}
	return signalGroup(p.cmd, signal)
}

// signalGroup signals the command's whole process group, falling back to the
// process itself when the group is already gone.
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		return cmd.Process.Signal(sig)
	}
	return nil
}

// Wait blocks until the Agent exits and returns its exit status. A non-zero
// status is an answer, not an error.
func (p *process) Wait() (int, error) {
	err := p.cmd.Wait()
	// Whatever the Agent started is Owl's to clean up: a test runner or a
	// compile left behind would go on burning the machine with nobody
	// watching it, which is the same reason signals go to the group in the
	// first place (ADR-0011, ADR-0012). This is the last moment it can be
	// done: the group is named by the Agent's pid, and that name is only
	// pinned while a member of the group is still alive.
	_ = signalGroup(p.cmd, syscall.SIGTERM)
	p.reaped.Store(true)
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
