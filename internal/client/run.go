package client

import (
	"context"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// Run is one attempt to carry out a Job, as the daemon reports it.
type Run struct {
	// ID identifies the Run and is what owl logs takes.
	ID int64
	// JobID is the Job this Run is an attempt at.
	JobID int64
	// Attempt counts the Job's Runs, from one.
	Attempt int
	// Started is when the Agent was launched.
	Started time.Time
	// Ended is when it exited, and is zero while the Run is still going.
	Ended time.Time
	// Outcome is succeeded, failed or interrupted, and is empty while the Run
	// is still going.
	Outcome string
	// Error is why the Run did not succeed.
	Error string
	// ExitCode is what the Agent exited with, and negative when it never got
	// far enough to have one.
	ExitCode int
	// LogPath is where its structured output was captured.
	LogPath string
	// Phase is what the Run was carrying out: plan or execute.
	Phase string
	// Skills is what the Run read, so that what an Agent did is attributable
	// to the instructions it had (ADR-0024).
	Skills []Skill
	// Paused is whether the Run is frozen right now (ADR-0011).
	Paused bool
	// Stage is where a Run that has not ended is: starting, agent, verifying
	// or finishing. Empty once it has ended.
	Stage string
}

// CheckResult is what one Verification check said about a Run.
type CheckResult struct {
	// Name identifies the check.
	Name string
	// Command is what it ran.
	Command string
	// Passed is whether it was satisfied.
	Passed bool
	// ExitCode is what the command exited with.
	ExitCode int
	// Output is what it printed.
	Output string
	// Reason says why it failed, empty for a check that passed.
	Reason string
}

// PhaseSettings is what one phase of a Job runs at, and where each setting
// came from.
type PhaseSettings struct {
	Phase      string
	Model      string
	ModelFrom  string
	Effort     string
	EffortFrom string
}

// JobDetails is what owl jobs show reports.
type JobDetails struct {
	Job  Job
	Runs []Run
	// SystemPrompt is the effective system prompt for the Job, exactly as the
	// Agent is given it.
	SystemPrompt string
	// Phases is what each phase of the Job runs at, in the order it passes
	// through them.
	Phases []PhaseSettings
	// Checks is what Verification said about the Job's most recent verified
	// Run.
	Checks []CheckResult
	// Handoff is the document on the Job's branch carrying intent and
	// progress from one Run to the next (ADR-0026), empty until a Run has
	// written one.
	Handoff string
	// Diff is what the Job's branch changed against the Project's base
	// branch, empty for a Job without a branch or whose branch is gone.
	Diff DiffSummary
}

// DiffFile is one file a Job's branch changed. A binary file is reported with
// no lines.
type DiffFile struct {
	Path       string
	Insertions int
	Deletions  int
}

// DiffSummary is what a Job's branch changed, per file and in total.
type DiffSummary struct {
	Files      []DiffFile
	Insertions int
	Deletions  int
}

// StartRun runs the Job at the head of the queue. started is false when the
// queue held nothing to run.
func (c *Client) StartRun(ctx context.Context) (job Job, run Run, started bool, err error) {
	res, err := c.jobs.StartRun(ctx, connect.NewRequest(&codingowlv1.StartRunRequest{}))
	if err != nil {
		return Job{}, Run{}, false, c.wrap(err)
	}
	if !res.Msg.GetStarted() {
		return Job{}, Run{}, false, nil
	}
	return jobFromProto(res.Msg.GetJob()), runFromProto(res.Msg.GetRun()), true, nil
}

// GetJob returns one Job with its Runs and the system prompt in force for it.
func (c *Client) GetJob(ctx context.Context, id int64) (JobDetails, error) {
	res, err := c.jobs.GetJob(ctx, connect.NewRequest(&codingowlv1.GetJobRequest{Id: id}))
	if err != nil {
		return JobDetails{}, c.wrap(err)
	}
	d := JobDetails{
		Job:          jobFromProto(res.Msg.GetJob()),
		SystemPrompt: res.Msg.GetSystemPrompt(),
		Runs:         make([]Run, 0, len(res.Msg.GetRuns())),
		Handoff:      res.Msg.GetHandoff(),
		Diff: DiffSummary{
			Insertions: int(res.Msg.GetDiff().GetInsertions()),
			Deletions:  int(res.Msg.GetDiff().GetDeletions()),
		},
	}
	for _, f := range res.Msg.GetDiff().GetFiles() {
		d.Diff.Files = append(d.Diff.Files, DiffFile{
			Path: f.GetPath(), Insertions: int(f.GetInsertions()), Deletions: int(f.GetDeletions()),
		})
	}
	for _, r := range res.Msg.GetRuns() {
		d.Runs = append(d.Runs, runFromProto(r))
	}
	for _, c := range res.Msg.GetChecks() {
		d.Checks = append(d.Checks, CheckResult{
			Name:     c.GetName(),
			Command:  c.GetCommand(),
			Passed:   c.GetPassed(),
			ExitCode: int(c.GetExitCode()),
			Output:   c.GetOutput(),
			Reason:   c.GetReason(),
		})
	}
	for _, p := range res.Msg.GetPhases() {
		d.Phases = append(d.Phases, PhaseSettings{
			Phase:      p.GetPhase(),
			Model:      p.GetModel(),
			ModelFrom:  p.GetModelFrom(),
			Effort:     p.GetEffort(),
			EffortFrom: p.GetEffortFrom(),
		})
	}
	return d, nil
}

// StreamRunLog sends a Run's captured output to line. With follow it keeps
// going until the Run ends.
func (c *Client) StreamRunLog(ctx context.Context, runID int64, follow bool, line func(string) error) error {
	stream, err := c.jobs.StreamRunLog(ctx, connect.NewRequest(&codingowlv1.StreamRunLogRequest{
		RunId: runID, Follow: follow,
	}))
	if err != nil {
		return c.wrap(err)
	}
	defer func() { _ = stream.Close() }()
	for stream.Receive() {
		if err := line(stream.Msg().GetLine()); err != nil {
			return err
		}
	}
	return c.wrap(stream.Err())
}

// runOutcomes translate the wire's outcome back into the daemon's vocabulary.
var runOutcomes = map[codingowlv1.RunOutcome]string{
	codingowlv1.RunOutcome_RUN_OUTCOME_SUCCEEDED:   "succeeded",
	codingowlv1.RunOutcome_RUN_OUTCOME_FAILED:      "failed",
	codingowlv1.RunOutcome_RUN_OUTCOME_INTERRUPTED: "interrupted",
}

func runFromProto(r *codingowlv1.Run) Run {
	out := Run{
		ID:       r.GetId(),
		JobID:    r.GetJobId(),
		Attempt:  int(r.GetAttempt()),
		Started:  r.GetStarted().AsTime(),
		Outcome:  runOutcomes[r.GetOutcome()],
		Error:    r.GetError(),
		ExitCode: int(r.GetExitCode()),
		LogPath:  r.GetLogPath(),
		Phase:    r.GetPhase(),
		Skills:   skillsFromProto(r.GetSkills()),
		Paused:   r.GetPaused(),
		Stage:    r.GetStage(),
	}
	if r.GetEnded() != nil {
		out.Ended = r.GetEnded().AsTime()
	}
	return out
}

// PauseRun freezes the Run in progress and everything its Agent started, and
// starts the grace window that will end it if nobody comes back.
func (c *Client) PauseRun(ctx context.Context) (Run, error) {
	res, err := c.jobs.PauseRun(ctx, connect.NewRequest(&codingowlv1.PauseRunRequest{}))
	if err != nil {
		return Run{}, c.wrap(err)
	}
	return runFromProto(res.Msg.GetRun()), nil
}

// ResumeRun continues a frozen Run where it was.
func (c *Client) ResumeRun(ctx context.Context) (Run, error) {
	res, err := c.jobs.ResumeRun(ctx, connect.NewRequest(&codingowlv1.ResumeRunRequest{}))
	if err != nil {
		return Run{}, c.wrap(err)
	}
	return runFromProto(res.Msg.GetRun()), nil
}
