//go:build darwin

// Package system reads the machine Owl runs on through the tools its operating
// system ships with: the idle Detector of ADR-0005, in its own package as
// every implementation of an extension point is, chosen by build tag because
// nothing about reading a machine is portable.
package system

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/idle"
)

// New is the Detector for this platform.
func New() idle.Detector { return darwin{} }

// readTimeout bounds one reading. The tools that answer are the system's own
// and answer in milliseconds; one that does not must not stall the daemon's
// only view of the machine.
const readTimeout = 5 * time.Second

// maxOutput bounds what is read from either tool. Both print a handful of
// lines, and neither is Owl's to trust with the daemon's memory.
const maxOutput = 256 << 10

// darwin reads the machine through the tools macOS ships with, rather than
// through cgo: `ioreg` for how long it has been since any input, and `pmset`
// for what the machine is drawing from.
//
// Both are looked up on PATH, where `security` is pinned to its absolute path
// (internal/credential/keychain_darwin.go). The difference is what is at stake:
// the keychain is handed every Account's token, and these two are handed
// nothing and asked a question. An Agent that could put its own `ioreg` on the
// daemon's PATH runs unsandboxed as the same user already (ADR-0006), so it
// could as easily write `idle: {after: 1s, requirePower: false}` into the
// configuration; what it would gain is a lie about the machine, not a secret
// and not a privilege. In
// return, the tools being found the ordinary way is what lets the behaviour
// suite put a machine of its own in front of them and step away from it.
type darwin struct{}

func (darwin) Name() string { return "darwin" }

// Read is the machine now. Both halves are read, because a policy may hold
// either against it.
func (d darwin) Read(ctx context.Context) (idle.State, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	since, err := inputIdle(ctx)
	if err != nil {
		return idle.State{}, err
	}
	power, err := onPower(ctx)
	if err != nil {
		return idle.State{}, err
	}
	return idle.State{Since: since, OnPower: power}, nil
}

// idleTimeRE is how ioreg prints the nanoseconds since the last input event.
var idleTimeRE = regexp.MustCompile(`"HIDIdleTime"\s*=\s*(\d+)`)

// inputIdle is how long it has been since any keyboard or mouse input, which
// the HID system keeps and ioreg prints.
func inputIdle(ctx context.Context) (time.Duration, error) {
	out, err := output(ctx, "ioreg", "-c", "IOHIDSystem", "-d", "4", "-r")
	if err != nil {
		return 0, err
	}
	found := idleTimeRE.FindAllSubmatch(out, -1)
	if len(found) == 0 {
		return 0, fmt.Errorf("ioreg said nothing about how long the machine has been idle")
	}
	// The most recent input across every entry: one device being untouched
	// says nothing about the machine while another is being typed on.
	least := time.Duration(-1)
	for _, m := range found {
		ns, err := strconv.ParseInt(string(m[1]), 10, 64)
		if err != nil {
			// An entry Owl cannot read is not the whole reading: it is
			// skipped, and a reading with nothing left in it fails below.
			continue
		}
		if d := time.Duration(ns); least < 0 || d < least {
			least = d
		}
	}
	if least < 0 {
		return 0, fmt.Errorf("ioreg said how long the machine has been idle in something Owl cannot read")
	}
	return least, nil
}

// drawing is the line pmset answers with, naming what the machine is running
// from.
const drawing = "Now drawing from"

// onPower is whether the machine is on AC power.
func onPower(ctx context.Context) (bool, error) {
	out, err := output(ctx, "pmset", "-g", "ps")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, drawing) {
			continue
		}
		return strings.Contains(line, "AC Power"), nil
	}
	return false, fmt.Errorf("pmset said nothing about what the machine is drawing from")
}

// output is what a tool printed, bounded, with what it said on the way out
// carried into the error so a person can see why.
func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, said bytes.Buffer
	cmd.Stdout = &capped{to: &out, left: maxOutput}
	cmd.Stderr = &capped{to: &said, left: 4 << 10}
	if err := cmd.Run(); err != nil {
		if trimmed := strings.TrimSpace(said.String()); trimmed != "" {
			return nil, fmt.Errorf("%s: %w: %s", name, err, trimmed)
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out.Bytes(), nil
}

// capped is a writer that takes only so much and quietly drops the rest: what
// a tool prints is the system's, and a tool that will not stop printing must
// not be held in memory.
type capped struct {
	to   io.Writer
	left int
}

func (c *capped) Write(p []byte) (int, error) {
	if c.left <= 0 {
		return len(p), nil
	}
	take := p
	if len(take) > c.left {
		take = take[:c.left]
	}
	n, err := c.to.Write(take)
	c.left -= n
	if err != nil {
		return n, err
	}
	return len(p), nil
}
