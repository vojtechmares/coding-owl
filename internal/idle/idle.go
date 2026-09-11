// Package idle is when Owl may work. The premise is that a coding agent runs
// on the machine you are not using, so Owl works while the machine is Idle:
// nobody has touched it for long enough, and it is on AC power (ADR-0011).
//
// What reads the machine is a Detector, one per platform and chosen at build
// time (ADR-0005). What the reading means is a Policy, which is the user's to
// set.
package idle

import (
	"context"
	"fmt"
	"time"
)

// State is what a Detector read about the machine.
type State struct {
	// Since is how long it has been since any keyboard or mouse input.
	Since time.Duration
	// OnPower is whether the machine is drawing from AC power.
	OnPower bool
}

// Detector reads the machine Owl is running on. It only reads: whether that
// state means Owl may work is the Policy's to say, not the platform's.
type Detector interface {
	// Name says what reads the machine, for what the daemon logs at startup.
	Name() string
	// Read is the machine's state now. A machine that cannot be read is an
	// error rather than a machine nobody is at: not knowing must never be
	// mistaken for permission to work.
	Read(ctx context.Context) (State, error)
}

// DefaultAfter is how long without input the machine must be before Owl works,
// when the configuration does not say (ADR-0011).
const DefaultAfter = 10 * time.Minute

// DefaultInterval is how often the daemon looks at the machine when the
// configuration does not say. It is the delay between the user coming back and
// the Agent freezing, so it is short rather than tidy.
const DefaultInterval = 5 * time.Second

// Policy is what a reading has to be for Owl to work.
type Policy struct {
	// After is how long the machine must have been without input.
	After time.Duration
	// RequirePower is whether it must be on AC power. Unattended work on a
	// battery flattens it, so this is on unless it is turned off.
	RequirePower bool
}

// DefaultPolicy is the policy of ADR-0011: ten minutes without input, and on
// AC power.
func DefaultPolicy() Policy {
	return Policy{After: DefaultAfter, RequirePower: true}
}

// Allows reports whether that state is Idle under this policy, and when it is
// not, why not - which is what a person is told when nothing is running.
func (p Policy) Allows(s State) (bool, string) {
	if p.RequirePower && !s.OnPower {
		return false, "the machine is on battery"
	}
	if s.Since < p.After {
		return false, "the machine is in use"
	}
	return true, ""
}

// Unreadable says a machine could not be read, in the words a person is shown.
func Unreadable(err error) string {
	return fmt.Sprintf("the machine could not be read: %v", err)
}
