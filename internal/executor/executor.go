// Package executor is the placement axis of ADR-0018: an Executor knows where
// an Agent runs. The host Executor runs it as a child process of the daemon
// (ADR-0006); a container Executor is the same interface, later.
package executor

import (
	"context"

	"github.com/vojtechmares/coding-owl/internal/agent"
)

// Executor starts Agents in one place.
type Executor interface {
	// Name identifies the placement, and is what a Run records.
	Name() string
	// Start launches the Agent. Cancelling ctx stops it.
	Start(ctx context.Context, inv agent.Invocation) (agent.Process, error)
}
