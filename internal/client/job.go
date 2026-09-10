package client

import (
	"context"
	"fmt"
	"math"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// Job is one queued piece of work as the daemon reports it.
type Job struct {
	// ID identifies the Job and is what the owl queue commands take.
	ID int64
	// Source names the producer the Job came from; the local queue is
	// "local".
	Source string
	// SourceRef is the Job's reference within that Source.
	SourceRef string
	// Project is the name of the Project the Job is queued against.
	Project string
	// Prompt is the work to do.
	Prompt string
	// State is where the Job is in its lifecycle, in the daemon's own
	// vocabulary: pending, active, blocked, review, done, cancelled or
	// exhausted.
	State string
	// Branch is the branch the Job's work lands on, empty until it has one.
	Branch string
	// Worktree is where that branch is checked out, empty until it has one.
	Worktree string
	// Planned is whether the Job is planned before it is executed.
	Planned bool
	// Plan is what its planning Run decided, empty until there is one.
	Plan string
	// Reason is why the Job is where it is when no Run explains it.
	Reason string
	// Position is the Job's place in the queue, counting from one, and zero
	// for a Job that is not in the queue.
	Position int
	// Created is when the Job was first produced.
	Created time.Time
}

// AddJobRequest is what owl add carries. Project is optional: empty means the
// Project WorkingDir is in, which the daemon resolves.
type AddJobRequest struct {
	Project    string
	Prompt     string
	WorkingDir string
	// Plan is whether to plan the Job before executing it. Nil plans, which is
	// the default (ADR-0026).
	Plan *bool
	// Model and Effort override what every phase of this Job runs at.
	Model  string
	Effort string
}

// AddJob queues a Job.
func (c *Client) AddJob(ctx context.Context, req AddJobRequest) (Job, error) {
	mode := codingowlv1.PlanMode_PLAN_MODE_UNSPECIFIED
	if req.Plan != nil {
		mode = codingowlv1.PlanMode_PLAN_MODE_PLAN
		if !*req.Plan {
			mode = codingowlv1.PlanMode_PLAN_MODE_NO_PLAN
		}
	}
	res, err := c.jobs.AddJob(ctx, connect.NewRequest(&codingowlv1.AddJobRequest{
		Project:    req.Project,
		Prompt:     req.Prompt,
		WorkingDir: req.WorkingDir,
		PlanMode:   mode,
		Model:      req.Model,
		Effort:     req.Effort,
	}))
	if err != nil {
		return Job{}, c.wrap(err)
	}
	return jobFromProto(res.Msg.GetJob()), nil
}

// ListJobs returns the queue in order, or every Job whatever its state.
func (c *Client) ListJobs(ctx context.Context, all bool) ([]Job, error) {
	res, err := c.jobs.ListJobs(ctx, connect.NewRequest(&codingowlv1.ListJobsRequest{All: all}))
	if err != nil {
		return nil, c.wrap(err)
	}
	out := make([]Job, 0, len(res.Msg.GetJobs()))
	for _, j := range res.Msg.GetJobs() {
		out = append(out, jobFromProto(j))
	}
	return out, nil
}

// CancelJob takes a pending Job out of the queue.
func (c *Client) CancelJob(ctx context.Context, id int64) (Job, error) {
	res, err := c.jobs.CancelJob(ctx, connect.NewRequest(&codingowlv1.CancelJobRequest{Id: id}))
	if err != nil {
		return Job{}, c.wrap(err)
	}
	return jobFromProto(res.Msg.GetJob()), nil
}

// ReorderJob moves a pending Job to position, counting from one. A position
// too large for the wire is refused here rather than truncated onto it.
func (c *Client) ReorderJob(ctx context.Context, id int64, position int) (Job, error) {
	if position < math.MinInt32 || position > math.MaxInt32 {
		return Job{}, &StatusError{
			Kind:    KindInvalid,
			Message: fmt.Sprintf("position %d is outside the queue; no queue is that long", position),
		}
	}
	res, err := c.jobs.ReorderJob(ctx, connect.NewRequest(&codingowlv1.ReorderJobRequest{
		Id: id, Position: int32(position),
	}))
	if err != nil {
		return Job{}, c.wrap(err)
	}
	return jobFromProto(res.Msg.GetJob()), nil
}

// jobStates translates the wire's state back into the daemon's vocabulary,
// which is what the CLI and the desktop app show.
var jobStates = map[codingowlv1.JobState]string{
	codingowlv1.JobState_JOB_STATE_PENDING:   "pending",
	codingowlv1.JobState_JOB_STATE_ACTIVE:    "active",
	codingowlv1.JobState_JOB_STATE_BLOCKED:   "blocked",
	codingowlv1.JobState_JOB_STATE_REVIEW:    "review",
	codingowlv1.JobState_JOB_STATE_DONE:      "done",
	codingowlv1.JobState_JOB_STATE_CANCELLED: "cancelled",
	codingowlv1.JobState_JOB_STATE_EXHAUSTED: "exhausted",
}

// unknownState is shown for a state this client does not know, which is what
// an older client sees when the daemon has learned a new one.
const unknownState = "unknown"

func jobFromProto(j *codingowlv1.Job) Job {
	state, ok := jobStates[j.GetState()]
	if !ok {
		state = unknownState
	}
	return Job{
		ID:        j.GetId(),
		Source:    j.GetSource(),
		SourceRef: j.GetSourceRef(),
		Project:   j.GetProject(),
		Prompt:    j.GetPrompt(),
		State:     state,
		Branch:    j.GetBranch(),
		Worktree:  j.GetWorktree(),
		Planned:   j.GetPlanned(),
		Plan:      j.GetPlan(),
		Reason:    j.GetReason(),
		Position:  int(j.GetPosition()),
		Created:   j.GetCreated().AsTime(),
	}
}

// AcceptJob keeps a Job's work: its worktree is reclaimed and its branch is
// left where it is. Force reclaims a worktree holding uncommitted changes.
func (c *Client) AcceptJob(ctx context.Context, id int64, force bool) (Job, error) {
	res, err := c.jobs.AcceptJob(ctx, connect.NewRequest(&codingowlv1.AcceptJobRequest{Id: id, Force: force}))
	if err != nil {
		return Job{}, c.wrap(err)
	}
	return jobFromProto(res.Msg.GetJob()), nil
}

// DropJob refuses a Job's work: its worktree and its branch both go.
func (c *Client) DropJob(ctx context.Context, id int64, force bool) (Job, error) {
	res, err := c.jobs.DropJob(ctx, connect.NewRequest(&codingowlv1.DropJobRequest{Id: id, Force: force}))
	if err != nil {
		return Job{}, c.wrap(err)
	}
	return jobFromProto(res.Msg.GetJob()), nil
}

// StateCount is how many Jobs are in one state.
type StateCount struct {
	State string
	Count int
}

// RunInProgress is a Run that has not ended, with the Job it is an attempt at.
type RunInProgress struct {
	Run Run
	Job Job
}

// Overview is what owl status reports.
type Overview struct {
	// Running is every Run that has not ended.
	Running []RunInProgress
	// Counts is how the Jobs stand, in the order a Job passes through the
	// states, holding only the states that have Jobs in them.
	Counts []StateCount
	// Awaiting is the Jobs in review, waiting for a decision.
	Awaiting []Job
	// Blocked is the Jobs stuck on something wrong with the work.
	Blocked []Job
}

// Empty reports whether there is nothing at all to say.
func (o Overview) Empty() bool {
	return len(o.Running) == 0 && len(o.Counts) == 0
}

// GetOverview reports where the work stands.
func (c *Client) GetOverview(ctx context.Context) (Overview, error) {
	res, err := c.jobs.GetOverview(ctx, connect.NewRequest(&codingowlv1.GetOverviewRequest{}))
	if err != nil {
		return Overview{}, c.wrap(err)
	}
	var o Overview
	for _, r := range res.Msg.GetRunning() {
		o.Running = append(o.Running, RunInProgress{
			Run: runFromProto(r.GetRun()), Job: jobFromProto(r.GetJob()),
		})
	}
	for _, c := range res.Msg.GetCounts() {
		state, ok := jobStates[c.GetState()]
		if !ok {
			state = unknownState
		}
		o.Counts = append(o.Counts, StateCount{State: state, Count: int(c.GetCount())})
	}
	for _, j := range res.Msg.GetAwaiting() {
		o.Awaiting = append(o.Awaiting, jobFromProto(j))
	}
	for _, j := range res.Msg.GetBlocked() {
		o.Blocked = append(o.Blocked, jobFromProto(j))
	}
	return o, nil
}
