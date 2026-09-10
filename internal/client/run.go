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
	// LogPath is where its structured output was captured.
	LogPath string
}

// JobDetails is what owl jobs show reports.
type JobDetails struct {
	Job  Job
	Runs []Run
	// SystemPrompt is the effective system prompt for the Job, exactly as the
	// Agent is given it.
	SystemPrompt string
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
	}
	for _, r := range res.Msg.GetRuns() {
		d.Runs = append(d.Runs, runFromProto(r))
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
		ID:      r.GetId(),
		JobID:   r.GetJobId(),
		Attempt: int(r.GetAttempt()),
		Started: r.GetStarted().AsTime(),
		Outcome: runOutcomes[r.GetOutcome()],
		Error:   r.GetError(),
		LogPath: r.GetLogPath(),
	}
	if r.GetEnded() != nil {
		out.Ended = r.GetEnded().AsTime()
	}
	return out
}
