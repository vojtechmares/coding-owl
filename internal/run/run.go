// Package run carries out Jobs. It gives a Job the worktree and branch that
// belong to it (ADR-0007), has a Driver build the Agent and an Executor start
// it (ADR-0018), captures what the Agent writes, and records the Run and where
// the Job got to (ADR-0027).
//
// Verification is a pass-through here: a clean exit leaves the Job in review
// and a failure blocks it (ADR-0013). Nothing retries.
package run

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/executor"
	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// logSuffix names a Run's captured output under the state directory
// (ADR-0014).
const logSuffix = ".jsonl"

// maxLine bounds one line of an Agent's structured output. A tool result can
// be large; a line that is larger than this is a runaway rather than an event.
const maxLine = 8 << 20

// stderrTail is how much of a failed Agent's standard error is quoted in the
// reason the Job is blocked with.
const stderrTail = 500

// bookkeepingTimeout bounds the writes that record how a Run ended, which
// happen while the daemon may already be stopping.
const bookkeepingTimeout = 10 * time.Second

// Outcome is how a Run ended (ADR-0027).
type Outcome string

const (
	// OutcomeSucceeded is an Agent that exited cleanly.
	OutcomeSucceeded Outcome = "succeeded"
	// OutcomeFailed is an Agent that exited non-zero, or could not be run.
	OutcomeFailed Outcome = "failed"
	// OutcomeInterrupted is a Run that was stopped rather than finished.
	OutcomeInterrupted Outcome = "interrupted"
)

// Run is one attempt to carry out a Job.
type Run struct {
	// ID identifies the Run and is what owl logs takes.
	ID int64
	// JobID is the Job this Run is an attempt at.
	JobID int64
	// Attempt counts this Job's Runs, from one.
	Attempt int
	// Started is when the Agent was launched.
	Started time.Time
	// Ended is when it exited, and is zero while the Run is still going.
	Ended time.Time
	// Outcome is how it ended, and is empty while it is still going.
	Outcome Outcome
	// Error is why it did not succeed.
	Error string
	// ExitCode is what the Agent exited with, and store.NoExitCode when it
	// never got far enough to have one.
	ExitCode int
	// LogPath is where its structured output was captured.
	LogPath string
}

// Details is a Job with its Runs and the system prompt in force for it.
type Details struct {
	Job          queue.Job
	Runs         []Run
	SystemPrompt string
}

// Line is one line of an Agent's structured output, numbered from one so a
// follower can tell what it has already seen.
type Line struct {
	Seq  int64
	Text string
}

// RefusedError marks a Run that cannot be started right now: something else is
// running, or the tool is not one Owl drives. Nothing is wrong with the Job.
type RefusedError struct{ Err error }

func (e *RefusedError) Error() string { return e.Err.Error() }
func (e *RefusedError) Unwrap() error { return e.Err }

func refused(format string, a ...any) error {
	return &RefusedError{Err: fmt.Errorf(format, a...)}
}

// Options is what a Service needs to carry out Jobs.
type Options struct {
	// Store holds the Jobs and their Runs.
	Store *store.Store
	// Projects answers what a Project is configured to do.
	Projects *project.Service
	// Driver is the coding tool Agents are (ADR-0018).
	Driver driver.Driver
	// Executor is where they run.
	Executor executor.Executor
	// WorktreeDir holds one worktree per Job (ADR-0014).
	WorktreeDir string
	// LogDir holds one captured stream per Run.
	LogDir string
	// Logger receives what a background Run cannot return to a caller.
	Logger *slog.Logger
}

// Service carries out Jobs.
type Service struct {
	opts Options
	now  func() time.Time

	// ctx is cancelled when the Service closes, which stops every Agent.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// starting serialises owl start, so the one-Agent-at-a-time cap is not a
	// check two callers can pass at once. Only one daemon can hold the socket,
	// so a lock in this process is the whole story.
	starting sync.Mutex

	mu      sync.Mutex
	brokers map[int64]*broker
}

// NewService returns a Service. Close it to stop the Agents it started.
func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		opts:    opts,
		now:     time.Now,
		ctx:     ctx,
		cancel:  cancel,
		brokers: map[int64]*broker{},
	}
}

// Close stops every Agent still running and waits for their Runs to be
// recorded.
func (s *Service) Close() error {
	s.cancel()
	s.wg.Wait()
	return nil
}

// Recover ends the Runs that were still going when the daemon last stopped.
// Every Agent is a child of the daemon, so nothing it started is still running
// after a restart, and a Run left open would otherwise report a Run in
// progress forever. The Jobs themselves are left where they were: requeueing
// them is a decision of its own (ADR-0011).
func (s *Service) Recover(ctx context.Context) error {
	n, err := s.opts.Store.InterruptRunsInProgress(ctx, s.now().UTC(),
		string(OutcomeInterrupted), "the daemon stopped before this run ended")
	if err != nil {
		return err
	}
	if n > 0 {
		s.opts.Logger.Info("runs left over from an earlier daemon", "interrupted", n)
	}
	return nil
}

// Start takes the Job at the head of the queue and runs it. started is false
// when nothing is pending, which is not an error.
func (s *Service) Start(ctx context.Context) (job queue.Job, run Run, started bool, err error) {
	s.starting.Lock()
	defer s.starting.Unlock()

	if r, ok, err := s.opts.Store.RunInProgress(ctx); err != nil {
		return queue.Job{}, Run{}, false, err
	} else if ok {
		return queue.Job{}, Run{}, false,
			refused("a run for job %d is already in progress; only one agent runs at a time", r.JobID)
	}
	j, ok, err := s.opts.Store.NextQueued(ctx, string(queue.StatePending))
	if err != nil || !ok {
		return queue.Job{}, Run{}, false, err
	}
	// What the Project is configured to do is read from its base branch, so an
	// Agent cannot change the terms it runs under (ADR-0014).
	details, err := s.opts.Projects.Show(ctx, j.Project)
	if err != nil {
		return queue.Job{}, Run{}, false, err
	}
	// The tool is checked before anything is written down, so an unsupported
	// Claude Code leaves the Job exactly as it was.
	if err := s.opts.Driver.Check(ctx); err != nil {
		return queue.Job{}, Run{}, false, &RefusedError{Err: err}
	}

	if j.Branch == "" {
		// The worktree and the branch belong to the Job, so a Job that takes
		// several nights accumulates its work in one place (ADR-0007).
		id := strconv.FormatInt(j.ID, 10)
		branch := details.Config.BranchPrefix + "job-" + id
		worktree := filepath.Join(s.opts.WorktreeDir, id)
		if err := mkdirPrivate(s.opts.WorktreeDir); err != nil {
			return queue.Job{}, Run{}, false, err
		}
		if err := git.AddWorktree(details.Path, worktree, branch, details.BaseBranch); err != nil {
			return queue.Job{}, Run{}, false, err
		}
		if err := s.opts.Store.SetJobWorkspace(ctx, j.ID, branch, worktree); err != nil {
			return queue.Job{}, Run{}, false, err
		}
		j.Branch, j.Worktree = branch, worktree
	}

	r, err := s.opts.Store.StartRun(ctx, store.Run{JobID: j.ID, Started: s.now().UTC()})
	if err != nil {
		return queue.Job{}, Run{}, false, err
	}
	// From here the Run exists, and a Run left open would report a Run in
	// progress until the daemon restarts. Anything that goes wrong now ends it.
	defer func() {
		if err != nil {
			s.abandon(r.ID, j.ID, err)
		}
	}()
	logPath := filepath.Join(s.opts.LogDir, strconv.FormatInt(r.ID, 10)+logSuffix)
	if err = s.opts.Store.SetRunLog(ctx, r.ID, logPath); err != nil {
		return queue.Job{}, Run{}, false, err
	}
	r.LogPath = logPath
	if err = s.opts.Store.SetJobState(ctx, j.ID, string(queue.StateActive)); err != nil {
		return queue.Job{}, Run{}, false, err
	}

	req := driver.Request{
		Prompt:       j.Prompt,
		SystemPrompt: SystemPrompt(details.Config.UnattendedClauses),
		WorkingDir:   j.Worktree,
		BudgetUSD:    details.Config.BudgetUSD,
	}
	// The broker is opened here rather than in the goroutine, so a follower
	// that arrives the instant owl start returns finds the Run rather than an
	// empty log it reads as the end of one.
	b := s.openBroker(r.ID)
	s.wg.Add(1)
	go s.carryOut(r, req, b)

	return toJob(j), toRun(r), true, nil
}

// abandon ends a Run that never got as far as an Agent, and blocks its Job
// with the reason, so a failure between the Run being recorded and the Agent
// starting does not leave the queue waiting on a Run that is not happening.
func (s *Service) abandon(runID, jobID int64, cause error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), bookkeepingTimeout)
	defer cancel()
	if err := s.opts.Store.FinishRun(ctx, runID, s.now().UTC(),
		string(OutcomeFailed), cause.Error(), store.NoExitCode); err != nil {
		s.opts.Logger.Error("ending a run that never started", "run", runID, "error", err)
	}
	if err := s.opts.Store.DequeueJob(ctx, jobID, string(queue.StateBlocked)); err != nil {
		s.opts.Logger.Error("blocking a job whose run never started", "job", jobID, "error", err)
	}
}

// Show returns a Job with its Runs and the system prompt in force for it, so
// nothing Owl injects into a Run is hidden (ADR-0017).
func (s *Service) Show(ctx context.Context, jobID int64) (Details, error) {
	j, err := s.opts.Store.GetJob(ctx, jobID)
	if err != nil {
		return Details{}, err
	}
	rows, err := s.opts.Store.ListRuns(ctx, jobID)
	if err != nil {
		return Details{}, err
	}
	runs := make([]Run, 0, len(rows))
	for _, r := range rows {
		runs = append(runs, toRun(r))
	}
	details, err := s.opts.Projects.Show(ctx, j.Project)
	if err != nil {
		return Details{}, err
	}
	return Details{
		Job:          toJob(j),
		Runs:         runs,
		SystemPrompt: SystemPrompt(details.Config.UnattendedClauses),
	}, nil
}

// Log sends a Run's captured output to send, from its first line. With follow,
// it keeps going until the Run ends.
func (s *Service) Log(ctx context.Context, runID int64, follow bool, send func(Line) error) error {
	r, err := s.opts.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	// Subscribing before the file is read is what makes the two halves meet:
	// a line written between them arrives on the channel carrying the number
	// it had in the file, and is skipped as already sent.
	var live subscription
	following := false
	if follow {
		var ok bool
		if live, ok = s.subscribe(runID); ok {
			following = true
			defer live.stop()
		}
	}
	sent, err := s.sendFile(r.LogPath, send)
	if err != nil {
		return err
	}
	if !following {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ln, ok := <-live.lines:
			if !ok {
				if live.behind() {
					return fmt.Errorf("this run wrote faster than the log could be followed; read it again with owl logs %d", runID)
				}
				return nil
			}
			if ln.Seq <= sent {
				continue
			}
			sent = ln.Seq
			if err := send(ln); err != nil {
				return err
			}
		}
	}
}

// sendFile sends what has already been captured and returns how many lines
// that was. A Run whose Agent has not written anything yet has no file, which
// is not a failure.
func (s *Service) sendFile(path string, send func(Line) error) (int64, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	var sent int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		sent++
		if err := send(Line{Seq: sent, Text: sc.Text()}); err != nil {
			return sent, err
		}
	}
	return sent, sc.Err()
}

// carryOut runs the Agent and records what became of the Job.
func (s *Service) carryOut(r store.Run, req driver.Request, b *broker) {
	defer s.wg.Done()
	outcome, reason, code := s.execute(r, req, b)
	s.closeBroker(r.ID)

	// The daemon may be stopping, so the bookkeeping does not run under the
	// Service's own context.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), bookkeepingTimeout)
	defer cancel()
	if err := s.opts.Store.FinishRun(ctx, r.ID, s.now().UTC(), string(outcome), reason, code); err != nil {
		s.opts.Logger.Error("recording the end of a run", "run", r.ID, "error", err)
	}
	// An interrupted Run leaves its Job where it was: it was not finished, and
	// requeueing it is the daemon's business at restart (ADR-0011).
	state := queue.StateReview
	switch outcome {
	case OutcomeInterrupted:
		s.opts.Logger.Info("run interrupted", "run", r.ID, "job", r.JobID)
		return
	case OutcomeFailed:
		state = queue.StateBlocked
	}
	if err := s.opts.Store.DequeueJob(ctx, r.JobID, string(state)); err != nil {
		s.opts.Logger.Error("recording where a job got to", "job", r.JobID, "error", err)
	}
	s.opts.Logger.Info("run finished", "run", r.ID, "job", r.JobID, "outcome", outcome)
}

// execute starts the Agent, captures its output and reports how it ended, with
// the status it exited with when it got far enough to have one.
func (s *Service) execute(r store.Run, req driver.Request, b *broker) (Outcome, string, int) {
	if err := mkdirPrivate(filepath.Dir(r.LogPath)); err != nil {
		return OutcomeFailed, fmt.Sprintf("preparing the run's log: %v", err), store.NoExitCode
	}
	f, err := os.OpenFile(r.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return OutcomeFailed, fmt.Sprintf("opening the run's log: %v", err), store.NoExitCode
	}
	defer func() { _ = f.Close() }()

	inv, err := s.opts.Driver.Command(req)
	if err != nil {
		return OutcomeFailed, err.Error(), store.NoExitCode
	}
	proc, err := s.opts.Executor.Start(s.ctx, inv)
	if err != nil {
		return OutcomeFailed, err.Error(), store.NoExitCode
	}

	sc := bufio.NewScanner(proc.Stdout())
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var writeErr error
	for sc.Scan() {
		line := sc.Text()
		if _, err := f.WriteString(line + "\n"); err != nil && writeErr == nil {
			// The log is the Run's evidence, so losing it is a failure of the
			// Run rather than a line in the daemon's own log.
			writeErr = err
			s.opts.Logger.Error("capturing a run's output", "run", r.ID, "error", err)
		}
		b.publish(line)
	}
	readErr := sc.Err()
	code, waitErr := proc.Wait()

	switch {
	case s.ctx.Err() != nil:
		return OutcomeInterrupted, "the daemon stopped while the agent was working", store.NoExitCode
	case waitErr != nil:
		return OutcomeFailed, fmt.Sprintf("waiting for the agent: %v", waitErr), store.NoExitCode
	case code != 0:
		return OutcomeFailed, fmt.Sprintf("the agent exited with status %d%s", code, quote(proc.Stderr())), code
	case readErr != nil:
		return OutcomeFailed, fmt.Sprintf("reading the agent's output: %v", readErr), code
	case writeErr != nil:
		return OutcomeFailed, fmt.Sprintf("capturing the agent's output: %v", writeErr), code
	default:
		return OutcomeSucceeded, "", code
	}
}

// mkdirPrivate creates a directory only this user may read, and tightens one
// that is already there: MkdirAll leaves an existing directory's mode alone,
// and a Job's worktree and a Run's log are nobody else's business.
func mkdirPrivate(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// quote renders the tail of an Agent's standard error for the reason a Job is
// blocked with, or nothing when it said nothing.
func quote(stderr string) string {
	stderr = trimTail(stderr, stderrTail)
	if stderr == "" {
		return ""
	}
	return ": " + stderr
}

func trimTail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

func toRun(r store.Run) Run {
	return Run{
		ID:       r.ID,
		JobID:    r.JobID,
		Attempt:  r.Attempt,
		Started:  r.Started,
		Ended:    r.Ended,
		Outcome:  Outcome(r.Outcome),
		Error:    r.Error,
		ExitCode: r.ExitCode,
		LogPath:  r.LogPath,
	}
}

func toJob(j store.Job) queue.Job {
	return queue.Job{
		ID:        j.ID,
		Source:    j.Source,
		SourceRef: j.SourceRef,
		Project:   j.Project,
		Prompt:    j.Prompt,
		State:     queue.State(j.State),
		Branch:    j.Branch,
		Worktree:  j.Worktree,
		Position:  j.Position,
		Created:   j.Created,
	}
}
