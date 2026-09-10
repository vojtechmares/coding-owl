package daemon

// The Connect code is how a client tells "you asked for something impossible"
// from "Owl broke", so the mapping is checked rather than assumed.

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"

	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

func TestRPCErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want connect.Code
	}{
		{"not found", fmt.Errorf("%w: api", store.ErrNotFound), connect.CodeNotFound},
		{"name taken", fmt.Errorf("%w: api", store.ErrNameTaken), connect.CodeAlreadyExists},
		{"conflict", &project.ConflictError{Err: errors.New("already registered")}, connect.CodeAlreadyExists},
		{"no such job", fmt.Errorf("%w: 7", store.ErrJobNotFound), connect.CodeNotFound},
		{"invalid", &project.InvalidError{Err: errors.New("not a git repository")}, connect.CodeInvalidArgument},
		{"unusable queue request", &queue.InvalidError{Err: errors.New("job 7 is not pending")}, connect.CodeInvalidArgument},
		{"job left the queue mid-request", fmt.Errorf("%w: 7", store.ErrNotQueued), connect.CodeInvalidArgument},
		{"wrapped invalid", fmt.Errorf("while adding: %w", &project.InvalidError{Err: errors.New("bad name")}), connect.CodeInvalidArgument},
		{"anything else", errors.New("disk on fire"), connect.CodeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := connect.CodeOf(rpcError(tc.err)); got != tc.want {
				t.Errorf("code = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRPCErrorPassesNilThrough(t *testing.T) {
	if err := rpcError(nil); err != nil {
		t.Errorf("rpcError(nil) = %v, want nil", err)
	}
}
