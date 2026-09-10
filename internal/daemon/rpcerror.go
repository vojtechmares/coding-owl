package daemon

import (
	"errors"

	"connectrpc.com/connect"

	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// rpcError gives a failure the Connect code that describes it, so a client
// can tell "you asked for something impossible" from "Owl broke".
func rpcError(err error) error {
	if err == nil {
		return nil
	}
	var invalid *project.InvalidError
	var conflict *project.ConflictError
	var unusable *queue.InvalidError
	var refused *run.RefusedError
	switch {
	case errors.As(err, &refused):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrJobNotFound), errors.Is(err, store.ErrRunNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.As(err, &conflict), errors.Is(err, store.ErrNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.As(err, &invalid), errors.As(err, &unusable), errors.Is(err, store.ErrNotQueued):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
