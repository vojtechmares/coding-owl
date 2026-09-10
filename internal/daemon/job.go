package daemon

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
)

// jobService exposes the queue and the Runs it produces over ConnectRPC. It
// holds no logic of its own; everything lives in internal/queue and
// internal/run.
type jobService struct {
	codingowlv1connect.UnimplementedJobServiceHandler
	jobs *queue.Service
	runs *run.Service
}

func (s *jobService) AddJob(ctx context.Context, req *connect.Request[codingowlv1.AddJobRequest]) (*connect.Response[codingowlv1.AddJobResponse], error) {
	j, err := s.jobs.Add(ctx, queue.AddRequest{
		Project:    req.Msg.GetProject(),
		Prompt:     req.Msg.GetPrompt(),
		WorkingDir: req.Msg.GetWorkingDir(),
		// Unspecified plans, because planning is the default (ADR-0026).
		Planned: req.Msg.GetPlanMode() != codingowlv1.PlanMode_PLAN_MODE_NO_PLAN,
		Model:   req.Msg.GetModel(),
		Effort:  req.Msg.GetEffort(),
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

func (s *jobService) StartRun(ctx context.Context, _ *connect.Request[codingowlv1.StartRunRequest]) (*connect.Response[codingowlv1.StartRunResponse], error) {
	j, r, started, err := s.runs.Start(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.StartRunResponse{Started: started}
	if started {
		res.Job, res.Run = toJobProto(j), toRunProto(r)
	}
	return connect.NewResponse(res), nil
}

func (s *jobService) GetJob(ctx context.Context, req *connect.Request[codingowlv1.GetJobRequest]) (*connect.Response[codingowlv1.GetJobResponse], error) {
	d, err := s.runs.Show(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.GetJobResponse{
		Job:          toJobProto(d.Job),
		Runs:         make([]*codingowlv1.Run, 0, len(d.Runs)),
		SystemPrompt: d.SystemPrompt,
		Phases:       make([]*codingowlv1.PhaseSettings, 0, len(d.Phases)),
	}
	for _, r := range d.Runs {
		res.Runs = append(res.Runs, toRunProto(r))
	}
	for _, c := range d.Checks {
		res.Checks = append(res.Checks, &codingowlv1.CheckResult{
			Name:     c.Name,
			Command:  c.Command,
			Passed:   c.Passed,
			ExitCode: int32(c.ExitCode),
			Output:   c.Output,
			Reason:   c.Reason,
		})
	}
	for _, p := range d.Phases {
		res.Phases = append(res.Phases, &codingowlv1.PhaseSettings{
			Phase:      string(p.Phase),
			Model:      p.Model.Value,
			ModelFrom:  p.Model.From,
			Effort:     p.Effort.Value,
			EffortFrom: p.Effort.From,
		})
	}
	return connect.NewResponse(res), nil
}

func (s *jobService) StreamRunLog(ctx context.Context, req *connect.Request[codingowlv1.StreamRunLogRequest], stream *connect.ServerStream[codingowlv1.StreamRunLogResponse]) error {
	err := s.runs.Log(ctx, req.Msg.GetRunId(), req.Msg.GetFollow(), func(l run.Line) error {
		return stream.Send(&codingowlv1.StreamRunLogResponse{Line: l.Text})
	})
	// A caller that hangs up ends the stream; that is how following stops, not
	// something to report as a failure.
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err != nil {
		return rpcError(err)
	}
	return nil
}

// runOutcomes put a Run's outcome on the wire. The set is closed (ADR-0027).
var runOutcomes = map[run.Outcome]codingowlv1.RunOutcome{
	run.OutcomeSucceeded:   codingowlv1.RunOutcome_RUN_OUTCOME_SUCCEEDED,
	run.OutcomeFailed:      codingowlv1.RunOutcome_RUN_OUTCOME_FAILED,
	run.OutcomeInterrupted: codingowlv1.RunOutcome_RUN_OUTCOME_INTERRUPTED,
}

func toRunProto(r run.Run) *codingowlv1.Run {
	out := &codingowlv1.Run{
		Id:       r.ID,
		JobId:    r.JobID,
		Attempt:  int32(r.Attempt),
		Started:  timestamppb.New(r.Started),
		Outcome:  runOutcomes[r.Outcome],
		Error:    r.Error,
		ExitCode: int32(r.ExitCode),
		LogPath:  r.LogPath,
		Phase:    string(r.Phase),
	}
	if !r.Ended.IsZero() {
		out.Ended = timestamppb.New(r.Ended)
	}
	return out
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
		Branch:    j.Branch,
		Worktree:  j.Worktree,
		Planned:   j.Planned,
		Plan:      j.Plan,
		Note:      j.Note,
		Position:  int32(j.Position),
		Created:   timestamppb.New(j.Created),
	}
}
