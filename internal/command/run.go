package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// MaxOutput is how much of what a command printed Owl keeps. It is held in
// memory and sent to the model in every later exchange, so a command that
// prints without end is stopped here rather than by whoever asked for it.
const MaxOutput = 64 << 10

// Timeout is how long one command may take. A chat is somebody waiting, and a
// command that has not answered by now is not going to.
const Timeout = 30 * time.Second

// waitDelay is how long after the deadline Owl waits for whatever the command
// started to let go of the output it was writing to.
const waitDelay = 2 * time.Second

// Result is what running one command came to: what it printed, and how it
// ended. A command that exits badly is an answer rather than a failure - the
// model asked what the repository says, and this is what it says.
type Result struct {
	// Output is what it printed, standard output and standard error together
	// in the order they arrived, cut to MaxOutput.
	Output string
	// ExitCode is how it ended.
	ExitCode int
	// Cut is whether there was more than Owl kept.
	Cut bool
}

// environment is what a command is given. The daemon's own environment holds
// what it was started with, which is not the chat's to hand to a program: this
// is the little that these programs need (ADR-0022).
func environment() []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		// Nothing is attached to a terminal, and nothing may ask for one: a
		// pager or a credential prompt would hang the chat waiting for a
		// keystroke that cannot arrive.
		"GIT_PAGER=cat",
		"GIT_TERMINAL_PROMPT=0",
		"PAGER=cat",
		"NO_COLOR=1",
		"LC_ALL=C",
	}
	return env
}

// Run carries out argv in dir and reports what it printed. It calls execve
// directly - there is no shell, and argv is exactly what was checked.
func Run(ctx context.Context, dir string, argv []string) (Result, error) {
	if len(argv) == 0 {
		return Result{}, errors.New("there is no command to run")
	}
	if dir == "" {
		return Result{}, errors.New("a command needs a directory to run in")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Result{}, fmt.Errorf("the working directory cannot be read: %w", err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", dir)
	}
	// The program is resolved here rather than by the shell there is not one
	// of, so a program that is not installed is said plainly.
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return Result{}, fmt.Errorf("%s is not installed on this machine: %w", argv[0], err)
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = environment()
	// The deadline kills the program Owl started; anything that program
	// started holds the pipe Owl is reading, and without this Run would wait
	// for it past the deadline it is meant to enforce.
	cmd.WaitDelay = waitDelay
	// Nothing types at it. A program that reads its input would otherwise wait
	// for what the daemon's own standard input happens to be.
	cmd.Stdin = nil
	out := &capped{max: MaxOutput}
	cmd.Stdout, cmd.Stderr = out, out

	err = cmd.Run()
	// What a program prints is bytes, and a repository holds files that are
	// not text. The output crosses a wire that carries strings and is shown to
	// a person, so what is not a character is replaced rather than carried.
	got := Result{Output: strings.ToValidUTF8(out.String(), "\ufffd"), Cut: out.cut}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return got, nil
	// Why it ended is asked before how: a command Owl killed for running out
	// of time exits like any other, and reporting that as the command's own
	// answer would tell the model the repository said something it did not.
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return got, fmt.Errorf("%s did not finish within %s", argv[0], Timeout)
	case ctx.Err() != nil:
		// The caller went away, which is a window that closed rather than a
		// command that took too long.
		return got, fmt.Errorf("%s was stopped: %w", argv[0], ctx.Err())
	case errors.As(err, &exit):
		// The command ran and said no. That is what the chat asked for.
		got.ExitCode = exit.ExitCode()
		return got, nil
	default:
		return got, fmt.Errorf("running %s: %w", argv[0], err)
	}
}

// capped is a writer that keeps the beginning of what it is given and counts
// the rest away. The beginning is what answers the question: a command printing
// without end is not one whose last page matters.
type capped struct {
	max int
	buf bytes.Buffer
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.max - c.buf.Len()
	if room <= 0 {
		c.cut = true
		return len(p), nil
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		c.cut = true
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *capped) String() string { return c.buf.String() }
