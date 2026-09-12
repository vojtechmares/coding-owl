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
	// TTL is how many Runs the Job may still take. One is spent at the end of
	// every Run whatever its outcome, and a Job with none left is exhausted
	// (ADR-0025).
	TTL int
	// Account is the Account the Job ran on, empty until it has run
	// (ADR-0023).
	Account string
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
	// TTL is how many Runs the Job may take. Zero asks for no particular
	// number and takes the daemon's default of ten.
	TTL int
}

// AddJob queues a Job.
func (c *Client) AddJob(ctx context.Context, req AddJobRequest) (Job, error) {
	if err := attemptsFitTheWire(req.TTL); err != nil {
		return Job{}, err
	}
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
		Ttl:        int32(req.TTL),
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

// attemptsFitTheWire refuses a number of attempts the wire cannot carry. The
// count travels as a 32-bit number, so a wider one would arrive truncated -
// and a Job would silently get a number of attempts nobody asked for.
func attemptsFitTheWire(ttl int) error {
	if ttl < 0 || ttl > math.MaxInt32 {
		return &StatusError{
			Kind:    KindInvalid,
			Message: fmt.Sprintf("attempts count from one; %d is not a number of runs a job can take", ttl),
		}
	}
	return nil
}

// ExtendJob gives a Job more attempts, returning it to the queue if it had run
// out. Zero asks for no particular number and adds the daemon's default.
func (c *Client) ExtendJob(ctx context.Context, id int64, ttl int) (Job, error) {
	if err := attemptsFitTheWire(ttl); err != nil {
		return Job{}, err
	}
	res, err := c.jobs.ExtendJob(ctx, connect.NewRequest(&codingowlv1.ExtendJobRequest{
		Id: id, Ttl: int32(ttl),
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
		TTL:       int(j.GetTtl()),
		Account:   j.GetAccount(),
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
	// Exhausted is the Jobs that have run out of attempts.
	Exhausted []Job
	// Unfinished is what garbage collection found and would not touch
	// (ADR-0015).
	Unfinished []Unfinished
	// Machine is what the daemon last read about the machine it runs on, which
	// is why work is or is not happening (ADR-0011).
	Machine Machine
	// Holding is what refused to start work on a machine Owl may work on,
	// empty when nothing has refused.
	Holding string
	// Accounts is what each Account has used against what it is held to
	// (ADR-0020).
	Accounts []AccountCeiling
	// PassedOver is the Jobs the scheduler would not start now, and why: the
	// cap that is binding, or the ceiling (ADR-0021, ADR-0025).
	PassedOver []PassedOverJob
}

// AccountCeiling is one Account against what it is held to (ADR-0020).
type AccountCeiling struct {
	// Name is the Account.
	Name string
	// Windows is what is known about each of its windows.
	Windows []UsageWindow
	// Waiting is why work on this Account is held back, empty when nothing
	// holds it back, and Until is when the window that holds it back starts
	// again.
	Waiting string
	Until   time.Time
}

// UsageWindow is how much of one window is spent, and how much of it Owl will
// work into.
type UsageWindow struct {
	// Name is the window in Owl's words: `five-hour` or `weekly`.
	Name string
	// Read is whether anything has been read about it yet; Utilization and
	// Resets say nothing until it is true.
	Read bool
	// Utilization is how much of it is spent, as a percentage, counting what
	// the user spent themselves.
	Utilization float64
	// Ceiling is how much of it Owl will work into, zero when nobody set one.
	Ceiling float64
	// Resets is when the window starts again.
	Resets time.Time
}

// Machine is the machine Owl runs on, as the daemon last read it (ADR-0011).
type Machine struct {
	// Read is whether it could be read at all. Everything else here is worth
	// nothing when it is false.
	Read bool
	// Idle is whether it is a machine Owl may work on, under the policy.
	Idle bool
	// Since is how long it has been since any keyboard or mouse input.
	Since time.Duration
	// OnPower is whether it is drawing from AC power.
	OnPower bool
	// Detail is why it is not a machine Owl may work on, or why it could not
	// be read. Empty when it is Idle.
	Detail string
}

// Empty reports whether there is nothing at all to say.
// PassedOverJob is a Job the scheduler would not start now, and why.
type PassedOverJob struct {
	Job    Job
	Reason string
}

func (o Overview) Empty() bool {
	return len(o.Running) == 0 && len(o.Counts) == 0 && len(o.Unfinished) == 0 &&
		len(o.Accounts) == 0 && len(o.PassedOver) == 0
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
	for _, j := range res.Msg.GetExhausted() {
		o.Exhausted = append(o.Exhausted, jobFromProto(j))
	}
	o.Unfinished = unfinishedFromProto(res.Msg.GetUnfinished())
	if m := res.Msg.GetMachine(); m != nil {
		o.Machine = Machine{
			Read:    m.GetRead(),
			Idle:    m.GetIdle(),
			Since:   m.GetSince().AsDuration(),
			OnPower: m.GetOnPower(),
			Detail:  m.GetDetail(),
		}
	}
	o.Holding = res.Msg.GetHolding()
	for _, a := range res.Msg.GetAccounts() {
		ceiling := AccountCeiling{Name: a.GetName(), Waiting: a.GetWaiting()}
		if a.GetUntil() != nil {
			ceiling.Until = a.GetUntil().AsTime()
		}
		for _, w := range a.GetWindows() {
			ceiling.Windows = append(ceiling.Windows, UsageWindow{
				Name: w.GetName(), Read: w.GetRead(), Utilization: w.GetUtilization(),
				Ceiling: w.GetCeiling(), Resets: w.GetResets().AsTime(),
			})
		}
		o.Accounts = append(o.Accounts, ceiling)
	}
	for _, p := range res.Msg.GetPassedOver() {
		o.PassedOver = append(o.PassedOver, PassedOverJob{
			Job: jobFromProto(p.GetJob()), Reason: p.GetReason(),
		})
	}
	return o, nil
}
