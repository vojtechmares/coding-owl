package daemon

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/skill"
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
	var unusableAccount *account.InvalidError
	var unusableSkill *skill.InvalidError
	var unusableChat *chat.InvalidError
	var inUse *account.InUseError
	var refused *run.RefusedError
	// A cap or a ceiling that is taken is the caller's situation rather than
	// Owl failing, the same as any other refusal (ADR-0021, ADR-0020).
	var capped *run.CappedError
	switch {
	// A request the daemon ended - because it is stopping, or because the
	// caller's deadline passed - is neither the caller's mistake nor Owl
	// breaking, and a client that is told which can say so.
	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, err)
	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, err)
	case errors.As(err, &refused), errors.As(err, &inUse), errors.As(err, &capped):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrJobNotFound),
		errors.Is(err, store.ErrRunNotFound), errors.Is(err, store.ErrAccountNotFound),
		errors.Is(err, store.ErrConversationNotFound), errors.Is(err, store.ErrProviderNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.As(err, &conflict), errors.Is(err, store.ErrNameTaken),
		errors.Is(err, store.ErrAccountNameTaken):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.As(err, &invalid), errors.As(err, &unusable), errors.As(err, &unusableAccount),
		errors.As(err, &unusableSkill), errors.As(err, &unusableChat),
		errors.Is(err, store.ErrNotQueued):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
