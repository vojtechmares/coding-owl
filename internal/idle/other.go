//go:build !darwin

package idle

import (
	"context"
	"fmt"
	"runtime"
)

// New is the Detector for this platform, which is nothing: darwin is the only
// machine Owl knows how to read (ADR-0005). Nothing starts by itself here,
// because not knowing whether the machine is Idle is not permission to work;
// `owl start` still does.
func New() Detector { return unread{} }

type unread struct{}

func (unread) Name() string { return runtime.GOOS }

func (unread) Read(context.Context) (State, error) {
	return State{}, fmt.Errorf("owl cannot tell whether a %s machine is idle; it reads darwin, for now", runtime.GOOS)
}
