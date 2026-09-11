package daemon

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/gc"
)

// gcService exposes garbage collection over ConnectRPC. It holds no logic of
// its own; everything lives in internal/gc.
type gcService struct {
	codingowlv1connect.UnimplementedGarbageCollectionServiceHandler
	gc *gc.Service
}

func (s *gcService) Collect(ctx context.Context, _ *connect.Request[codingowlv1.CollectRequest]) (*connect.Response[codingowlv1.CollectResponse], error) {
	report, err := s.gc.Collect(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.CollectResponse{Pruned: report.Pruned}
	for _, r := range report.Reclaimed {
		res.Reclaimed = append(res.Reclaimed, &codingowlv1.Reclaimed{
			JobId: r.Job, Path: r.Path, Why: r.Why,
		})
	}
	for _, a := range report.Accepted {
		res.Accepted = append(res.Accepted, &codingowlv1.Accepted{
			JobId: a.Job, Branch: a.Branch, Base: a.Base,
		})
	}
	res.Unfinished = toUnfinishedProto(report.Unfinished)
	return connect.NewResponse(res), nil
}

// unfinishedReasons put what is unfinished about a piece of work on the wire.
// The set is closed, so a reason missing from here is one nobody agreed to.
var unfinishedReasons = map[gc.Reason]codingowlv1.UnfinishedReason{
	gc.ReasonUncommitted: codingowlv1.UnfinishedReason_UNFINISHED_REASON_UNCOMMITTED,
	gc.ReasonWaiting:     codingowlv1.UnfinishedReason_UNFINISHED_REASON_WAITING,
	gc.ReasonAbandoned:   codingowlv1.UnfinishedReason_UNFINISHED_REASON_ABANDONED,
}

func toUnfinishedProto(rows []gc.Unfinished) []*codingowlv1.Unfinished {
	out := make([]*codingowlv1.Unfinished, 0, len(rows))
	for _, u := range rows {
		row := &codingowlv1.Unfinished{
			JobId:   u.Job,
			Project: u.Project,
			Path:    u.Path,
			Reason:  unfinishedReasons[u.Reason],
		}
		if u.Since > 0 {
			row.Since = durationpb.New(u.Since)
		}
		out = append(out, row)
	}
	return out
}
