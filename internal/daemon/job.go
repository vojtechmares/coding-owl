package daemon

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/queue"
)

// jobService exposes the queue over ConnectRPC. It holds no logic of its own;
// everything lives in internal/queue.
type jobService struct {
	codingowlv1connect.UnimplementedJobServiceHandler
	jobs *queue.Service
}

func (s *jobService) AddJob(ctx context.Context, req *connect.Request[codingowlv1.AddJobRequest]) (*connect.Response[codingowlv1.AddJobResponse], error) {
	j, err := s.jobs.Add(ctx, queue.AddRequest{
		Project:    req.Msg.GetProject(),
		Prompt:     req.Msg.GetPrompt(),
		WorkingDir: req.Msg.GetWorkingDir(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.AddJobResponse{Job: toJobProto(j)}), nil
}

func (s *jobService) ListJobs(ctx context.Context, req *connect.Request[codingowlv1.ListJobsRequest]) (*connect.Response[codingowlv1.ListJobsResponse], error) {
	js, err := s.jobs.List(ctx, req.Msg.GetAll())
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.ListJobsResponse{Jobs: make([]*codingowlv1.Job, 0, len(js))}
	for _, j := range js {
		res.Jobs = append(res.Jobs, toJobProto(j))
	}
	return connect.NewResponse(res), nil
}

func (s *jobService) CancelJob(ctx context.Context, req *connect.Request[codingowlv1.CancelJobRequest]) (*connect.Response[codingowlv1.CancelJobResponse], error) {
	j, err := s.jobs.Cancel(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.CancelJobResponse{Job: toJobProto(j)}), nil
}

func (s *jobService) ReorderJob(ctx context.Context, req *connect.Request[codingowlv1.ReorderJobRequest]) (*connect.Response[codingowlv1.ReorderJobResponse], error) {
	j, err := s.jobs.Reorder(ctx, req.Msg.GetId(), int(req.Msg.GetPosition()))
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.ReorderJobResponse{Job: toJobProto(j)}), nil
}

// jobStates puts a Job's state on the wire. The set is closed (ADR-0025), so
// a state missing from here is a state nobody agreed to.
var jobStates = map[queue.State]codingowlv1.JobState{
	queue.StatePending:   codingowlv1.JobState_JOB_STATE_PENDING,
	queue.StateActive:    codingowlv1.JobState_JOB_STATE_ACTIVE,
	queue.StateBlocked:   codingowlv1.JobState_JOB_STATE_BLOCKED,
	queue.StateReview:    codingowlv1.JobState_JOB_STATE_REVIEW,
	queue.StateDone:      codingowlv1.JobState_JOB_STATE_DONE,
	queue.StateCancelled: codingowlv1.JobState_JOB_STATE_CANCELLED,
	queue.StateExhausted: codingowlv1.JobState_JOB_STATE_EXHAUSTED,
}

func toJobProto(j queue.Job) *codingowlv1.Job {
	return &codingowlv1.Job{
		Id:        j.ID,
		Source:    j.Source,
		SourceRef: j.SourceRef,
		Project:   j.Project,
		Prompt:    j.Prompt,
		State:     jobStates[j.State],
		Position:  int32(j.Position),
		Created:   timestamppb.New(j.Created),
	}
}
