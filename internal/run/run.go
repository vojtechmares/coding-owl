// Package run carries out Jobs. It gives a Job the worktree and branch that
// belong to it (ADR-0007), has a Driver build the Agent and an Executor start
// it (ADR-0018), captures what the Agent writes, and records the Run and where
// the Job got to (ADR-0027).
//
// After an execution Run it runs Verification: the Project's own checks decide
// whether the work is acceptable, and a Job reaches review only if they all
// pass (ADR-0013). Nothing retries.
package run

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/driver"
	"github.com/vojtechmares/coding-owl/internal/executor"
	"github.com/vojtechmares/coding-owl/internal/gc"
	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/shell"
	"github.com/vojtechmares/coding-owl/internal/skill"
	"github.com/vojtechmares/coding-owl/internal/store"
	"github.com/vojtechmares/coding-owl/internal/verifier"
)

// logSuffix names a Run's captured output under the state directory
// (ADR-0014).
const logSuffix = ".jsonl"

// maxLine bounds one line of an Agent's structured output. A tool result can
// be large; a line that is larger than this is a runaway rather than an event.
const maxLine = 8 << 20

// maxHandoff bounds the handoff document, which is read into the daemon, the
// database and the next Agent's prompt.
const maxHandoff = 256 << 10

// maxDiff is how much of what a Run changed is carried into a reviewer's
// prompt. A reviewer given more than this is told to read the rest itself,
// which it can: it works in the worktree (ADR-0013). The whole prompt is one
// argument to the tool, and Linux takes at most 128 KiB in one of those, so
// this leaves room for the plan and for what Owl says around both.
const maxDiff = 64 << 10

// stderrTail is how much of a failed Agent's standard error is quoted in the
// reason the Job is blocked with.
const stderrTail = 500

// bookkeepingTimeout bounds the writes that record how a Run ended, which
// happen while the daemon may already be stopping.
const bookkeepingTimeout = 10 * time.Second

// setupTimeout bounds one of a Project's setup commands. Fetching
// dependencies is slow, and a Run that never starts is worse than one that
// waits.
const setupTimeout = 15 * time.Minute

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
	// Phase is what the Run was carrying out (ADR-0026).
	Phase Phase
	// Skills is what the Run read, so that what an Agent did is attributable
	// to the instructions it had (ADR-0024).
	Skills []skill.Locked
	// Paused is whether the Run is frozen right now. Only a Run this daemon is
	// carrying out can be (ADR-0011).
	Paused bool
	// Stage is where a Run that has not ended is. Only the daemon carrying it
	// out knows, and it is empty once the Run has ended.
	Stage Stage
}

// Stage is where a Run in progress is: an Agent is only one part of it.
type Stage string

const (
	// StageStarting is between the Run being recorded and its Agent running:
	// the log being opened, the Driver building the command.
	StageStarting Stage = "starting"
	// StageAgent is the Agent running, which is the only stage that can be
	// frozen (ADR-0011).
	StageAgent Stage = "agent"
	// StageVerifying is the Project's checks running on what the Agent left
	// (ADR-0013).
	StageVerifying Stage = "verifying"
	// StageFinishing is what comes after the Agent, and after Verification
	// where there was any: recording the plan, and the Run's end.
	StageFinishing Stage = "finishing"
)

// Details is a Job with its Runs, the system prompt in force for it, and what
// each of its phases would run at.
type Details struct {
	Job          queue.Job
	Runs         []Run
	SystemPrompt string
	// VerifierSystemPrompt is Owl's standing contract with the Agent that
	// verifies the work, for a Project that asks for one and empty for one
	// that does not: nothing Owl puts in front of an Agent is hidden
	// (ADR-0013, ADR-0017).
	VerifierSystemPrompt string
	// Phases is what each phase runs at and where each setting came from, in
	// the order a Job passes through them (ADR-0028).
	Phases []Settings
	// Checks is what Verification said about the Job's most recent Run that
	// was verified, in the order the checks were configured.
	Checks []verifier.Result
	// Handoff is the document on the Job's branch carrying intent and
	// progress from one Run to the next (ADR-0026): read from the worktree
	// while the Job has one, and from the branch once it does not.
	Handoff string
	// Diff is what the Job's branch changed against the Project's base
	// branch, empty for a Job without a branch or whose branch is gone.
	Diff git.DiffSummary
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
	// Accounts answers which subscription a Run draws on, and holds the
	// credential it draws with (ADR-0019).
	Accounts *account.Service
	// Driver is the coding tool Agents are (ADR-0018).
	Driver driver.Driver
	// Executor is where they run.
	Executor executor.Executor
	// Verifier decides whether what a Run produced is acceptable (ADR-0013).
	Verifier verifier.Verifier
	// AgentVerifier is the second pair of eyes a Project can ask for after its
	// own checks: a fresh Agent Session that reviews the work (ADR-0013). A
	// Service without one refuses a Project that asks for it rather than
	// letting the Job through unjudged.
	AgentVerifier verifier.Verifier
	// Skills fetches and places what a Project declares (ADR-0024).
	Skills *skill.Service
	// WorktreeConfigDir holds the exclude file Owl owns for each Job's
	// worktree, one directory per Job, outside the worktree so that an Agent
	// cannot edit what hides its own Skills (ADR-0033).
	WorktreeConfigDir string
	// Collector answers what garbage collection would report as unfinished, so
	// that owl status can say it (ADR-0015). A Service without one reports no
	// unfinished work.
	Collector Collector
	// WorktreeDir holds one worktree per Job (ADR-0014).
	WorktreeDir string
	// LogDir holds one captured stream per Run.
	LogDir string
	// ConfigPath is the daemon's own configuration file, which sets what the
	// phases of every Job run at unless something narrower says otherwise
	// (ADR-0014, ADR-0028).
	ConfigPath string
	// Logger receives what a background Run cannot return to a caller.
	Logger *slog.Logger
}

// Collector answers what garbage collection would report as unfinished work,
// without collecting anything.
type Collector interface {
	Unfinished(ctx context.Context) ([]gc.Unfinished, error)
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
	// so a lock in this process is the whole story. It also guards stopping,
	// which is what keeps a Run from being started while Close is waiting for
	// the Runs already going.
	starting sync.Mutex
	stopping bool

	// disposing serialises accepting and dropping Jobs, so that the state a
	// disposal decided on cannot change while it is reclaiming the worktree
	// and the branch that decision was about.
	disposing sync.Mutex

	// mu guards what this daemon knows about the Runs that are in flight: who
	// is following them, and which of them are frozen.
	mu      sync.Mutex
	brokers map[int64]*broker
	live    map[int64]*live
	// stages is where each Run in progress is once its Agent has exited; a
	// Run in live is at its Agent, and one in neither is starting.
	stages map[int64]Stage

	// carrying is the Jobs this daemon has a Run going for, which is what
	// makes "no daemon is running it" a question somebody can answer rather
	// than a guess from two tables (ADR-0015).
	carryingMu sync.Mutex
	carrying   map[int64]bool
}

// NewService returns a Service. Close it to stop the Agents it started.
func NewService(opts Options) *Service {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		opts:     opts,
		now:      time.Now,
		ctx:      ctx,
		cancel:   cancel,
		brokers:  map[int64]*broker{},
		live:     map[int64]*live{},
		stages:   map[int64]Stage{},
		carrying: map[int64]bool{},
	}
}

// Close stops every Agent still running and waits for their Runs to be
// recorded. No Run starts after it.
func (s *Service) Close() error {
	// Cancelling comes first: a Start in the middle of a Project's setup
	// commands holds the lock below for as long as they take, and cancelling
	// is what lets it give the lock back.
	s.cancel()

	// A frozen Agent cannot act on the signal cancelling just sent it, so
	// every paused Run is continued: otherwise the daemon waits out the kill
	// delay on processes that never heard it (ADR-0011).
	s.releasePaused()

	// Refusing new Runs under the same lock Start holds is what orders the two:
	// a Start already under way finishes counting its Run in before the wait
	// below begins, and one that arrives afterwards is refused.
	s.starting.Lock()
	s.stopping = true
	s.starting.Unlock()

	s.wg.Wait()
	return nil
}

// untilClosed is ctx, cut short when the Service closes. The work it bounds is
// the user's own commands, which can take minutes: a daemon that is stopping
// must not have to wait for them.
func (s *Service) untilClosed(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// Recover ends the Runs that were still going when the daemon last stopped and
// puts their Jobs back in the queue. Every Agent is a child of the daemon, so
// nothing it started survives a restart; the Job's worktree and branch do, so
// the next Run carries on in place rather than from the Project's base branch
// (ADR-0011).
func (s *Service) Recover(ctx context.Context) error {
	n, err := s.opts.Store.InterruptRunsInProgress(ctx, s.now().UTC(),
		string(OutcomeInterrupted), "the daemon stopped before this run ended")
	if err != nil {
		return err
	}
	if n > 0 {
		s.opts.Logger.Info("runs left over from an earlier daemon", "interrupted", n)
	}
	return s.requeueLeftOver(ctx)
}

// Start takes the Job at the head of the queue and runs it. started is false
// when nothing is pending, which is not an error.
func (s *Service) Start(ctx context.Context) (job queue.Job, run Run, started bool, err error) {
	s.starting.Lock()
	defer s.starting.Unlock()

	// The flag and the context are set a moment apart, and either of them
	// means the same thing: nothing new starts now.
	if s.stopping || s.ctx.Err() != nil {
		return queue.Job{}, Run{}, false, refused("the daemon is stopping; nothing new is started now")
	}
	if r, ok, err := s.opts.Store.RunInProgress(ctx); err != nil {
		return queue.Job{}, Run{}, false, err
	} else if ok {
		return queue.Job{}, Run{}, false,
			refused("a run for job %d is already in progress; only one agent runs at a time", r.JobID)
	}
	j, ok, err := s.nextRunnable(ctx)
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
	// So is what the phase runs at: a model nobody can run is the caller's
	// mistake, not a failed Run.
	global, err := s.global()
	if err != nil {
		return queue.Job{}, Run{}, false, err
	}
	phase := phaseOf(j)
	settings, err := resolve(phase, global, details.Config, j)
	if err != nil {
		return queue.Job{}, Run{}, false, &RefusedError{Err: err}
	}
	// Which subscription the work draws on is the Project's to say, and the
	// Job keeps it for its whole life (ADR-0023). A Job that has no Account to
	// run on has not failed at anything, so it stays exactly as it was.
	acct, token, err := s.accountFor(ctx, j, details)
	if err != nil {
		return queue.Job{}, Run{}, false, err
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

	// The Skills a Project declares are placed before anything reads them, and
	// before the setup commands, which may want them. A Skill that cannot be
	// fetched or placed refuses the Run rather than failing it: nothing is
	// wrong with the work (ADR-0024).
	placed, used, err := s.placeSkills(ctx, j, details)
	if err != nil {
		return queue.Job{}, Run{}, false, err
	}

	// A fresh worktree does not have the untracked things a build needs, so
	// the Project's setup commands run before the Agent does (ADR-0007). A Job
	// that cannot be prepared has not attempted anything, so it is blocked
	// without a Run to its name.
	if err := s.prepare(ctx, j, details.Config.Setup); err != nil {
		return queue.Job{}, Run{}, false, err
	}

	// The prompt is built after the worktree exists, because an execution Run
	// reads the handoff that is in it.
	prompt, err := s.promptFor(phase, j)
	if err != nil {
		return queue.Job{}, Run{}, false, err
	}

	r, err := s.opts.Store.StartRun(ctx, store.Run{
		JobID: j.ID, Started: s.now().UTC(), Phase: string(phase),
	})
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
	// What the Agent will read is recorded against the Run, so that what it
	// did is attributable to the instructions it had (ADR-0024).
	if err = s.opts.Store.SetRunSkills(ctx, r.ID, used); err != nil {
		return queue.Job{}, Run{}, false, err
	}
	if err = s.opts.Store.SetJobState(ctx, j.ID, string(queue.StateActive)); err != nil {
		return queue.Job{}, Run{}, false, err
	}
	if len(placed.Skills) > 0 {
		s.opts.Logger.Info("skills placed",
			"job", j.ID, "run", r.ID, "into", placed.Dir, "skills", len(placed.Skills))
	}
	// The Account is recorded at the Job's first Run and not touched again, so
	// what a Job drew on stays knowable for its whole life (ADR-0023).
	if j.Account == "" {
		if err = s.opts.Store.SetJobAccount(ctx, j.ID, acct.Name); err != nil {
			return queue.Job{}, Run{}, false, err
		}
		j.Account = acct.Name
	}

	req := driver.Request{
		Prompt:       prompt,
		SystemPrompt: SystemPrompt(details.Config.UnattendedClauses),
		WorkingDir:   j.Worktree,
		BudgetUSD:    details.Config.BudgetUSD,
		Model:        settings.Model.Value,
		Effort:       settings.Effort.Value,
		ConfigDir:    acct.ConfigDir,
		Token:        token,
	}
	// The broker is opened here rather than in the goroutine, so a follower
	// that arrives the instant owl start returns finds the Run rather than an
	// empty log it reads as the end of one.
	b := s.openBroker(r.ID)
	s.wg.Add(1)
	s.carry(j.ID, true)
	go s.carryOut(j, r, phase, req, b, details)

	return queue.FromStore(j), toRun(r), true, nil
}

// phaseOf is what a Job's next Run carries out: a Job that is planned and has
// no plan yet is planned first, and everything else is execution (ADR-0026).
func phaseOf(j store.Job) Phase {
	if j.Planned && strings.TrimSpace(j.Plan) == "" {
		return PhasePlan
	}
	return PhaseExecute
}

// promptFor builds what the Agent is asked to do this Run. The execution
// prompt carries the handoff as it stands in the Job's worktree, so a Run
// reads what the last one left - and what the user edited since (ADR-0026).
func (s *Service) promptFor(phase Phase, j store.Job) (string, error) {
	if phase == PhasePlan {
		return planPrompt(j.Prompt), nil
	}
	handoff, err := readHandoff(j.Worktree)
	if err != nil {
		return "", err
	}
	// The handoff in the worktree is what the last Run left and what the user
	// may have edited since, so it wins. A Job whose worktree has lost the
	// file still has its plan, which is what the handoff started as
	// (ADR-0026), and the prompt says so rather than naming a file that is not
	// there.
	source := HandoffPath + " on this branch"
	if strings.TrimSpace(handoff) == "" {
		handoff, source = j.Plan, "the plan this job was given"
	}
	return executePrompt(j.Prompt, handoff, source), nil
}

// readHandoff reads a Job's handoff from its worktree. A Job that has none yet
// is not an error: its first Run writes one.
//
// The file is written by an unattended Agent and read back into the daemon,
// the database and the next Agent's prompt, so it is opened without following
// a symlink and read up to a limit: a handoff is a document someone will read,
// not a way to fetch a file or exhaust the daemon.
func readHandoff(worktree string) (string, error) {
	if worktree == "" {
		return "", nil
	}
	path := filepath.Join(worktree, HandoffPath)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", HandoffPath, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxHandoff+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", HandoffPath, err)
	}
	if len(data) > maxHandoff {
		return "", fmt.Errorf("%s is larger than %d bytes; a handoff is a document, not a dump", HandoffPath, maxHandoff)
	}
	return string(data), nil
}

// recordPlan takes what a planning Run decided out of the handoff it wrote,
// commits it if the Agent did not, and keeps it on the Job. A planning Run
// that wrote nothing produced no plan, which is a failure however cleanly its
// Agent exited.
func (s *Service) recordPlan(ctx context.Context, j store.Job) error {
	plan, err := readHandoff(j.Worktree)
	if err != nil {
		return err
	}
	if strings.TrimSpace(plan) == "" {
		return fmt.Errorf("the planning run left no %s, so there is no plan to carry out", HandoffPath)
	}
	if _, err := git.CommitPath(j.Worktree, HandoffPath, "plan job "+strconv.FormatInt(j.ID, 10)); err != nil {
		return err
	}
	return s.opts.Store.SetJobPlan(ctx, j.ID, plan)
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
	// The Run carries the reason, so the Job needs no note of its own.
	if err := s.opts.Store.DequeueJob(ctx, jobID, string(queue.StateBlocked), ""); err != nil {
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
		out := toRun(r)
		// What a Run read is recorded against it, so that what an Agent did is
		// attributable to the instructions it had (ADR-0024).
		read, err := s.opts.Store.ListRunSkills(ctx, r.ID)
		if err != nil {
			return Details{}, err
		}
		for _, sk := range read {
			out.Skills = append(out.Skills, skill.Locked{
				Name: sk.Name, Source: sk.Source, Ref: sk.Ref, Commit: sk.Commit, Digest: sk.Digest,
			})
		}
		runs = append(runs, s.paused(out))
	}
	details, err := s.opts.Projects.Show(ctx, j.Project)
	if err != nil {
		return Details{}, err
	}
	global, err := s.global()
	if err != nil {
		return Details{}, err
	}
	settings := make([]Settings, 0, len(phases))
	for _, phase := range phasesOf(j) {
		// A phase whose settings are unusable is still worth reporting: the
		// value that is the problem is what a user needs to see.
		resolved, _ := resolve(phase, global, details.Config, j)
		settings = append(settings, resolved)
	}
	// The checks of the most recent Run that was verified are the ones that
	// say where the Job stands.
	var results []verifier.Result
	for i := len(runs) - 1; i >= 0 && results == nil; i-- {
		rows, err := s.opts.Store.ListCheckResults(ctx, runs[i].ID)
		if err != nil {
			return Details{}, err
		}
		for _, r := range rows {
			results = append(results, verifier.Result(r))
		}
	}
	handoff, err := handoffOf(j, details.Path)
	if err != nil {
		return Details{}, err
	}
	diff, err := diffOf(j, details.Path, details.BaseBranch)
	if err != nil {
		return Details{}, err
	}
	return Details{
		Job:                  queue.FromStore(j),
		Runs:                 runs,
		SystemPrompt:         SystemPrompt(details.Config.UnattendedClauses),
		VerifierSystemPrompt: s.verifierSystemPrompt(details.Config),
		Phases:               settings,
		Checks:               results,
		Handoff:              handoff,
		Diff:                 diff,
	}, nil
}

// handoffOf reads a Job's handoff from its worktree while it has one, and
// from its branch once the worktree is gone: an accepted Job keeps its branch
// (ADR-0015), and the handoff on it is still worth reading.
func handoffOf(j store.Job, projectPath string) (string, error) {
	if j.Worktree != "" {
		return readHandoff(j.Worktree)
	}
	if j.Branch == "" {
		return "", nil
	}
	has, err := git.HasBranch(projectPath, j.Branch)
	if err != nil || !has {
		return "", err
	}
	data, found, err := git.ShowFileOnBranch(projectPath, j.Branch, HandoffPath)
	if err != nil || !found {
		return "", err
	}
	if len(data) > maxHandoff {
		return "", fmt.Errorf("%s is larger than %d bytes; a handoff is a document, not a dump", HandoffPath, maxHandoff)
	}
	return string(data), nil
}

// diffOf summarises what a Job's branch changed, and is empty for a Job that
// has no branch or whose branch was deleted when it was dropped.
func diffOf(j store.Job, projectPath, base string) (git.DiffSummary, error) {
	if j.Branch == "" {
		return git.DiffSummary{}, nil
	}
	has, err := git.HasBranch(projectPath, j.Branch)
	if err != nil || !has {
		return git.DiffSummary{}, err
	}
	return git.DiffStat(projectPath, base, j.Branch)
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
func (s *Service) carryOut(j store.Job, r store.Run, phase Phase, req driver.Request, b *broker, details project.Details) {
	defer s.wg.Done()
	// The Job stops being carried only once everything about it is written
	// down, so that nothing can see it between its Run ending and the Job
	// being moved on and conclude that it was abandoned.
	defer s.carry(j.ID, false)
	outcome, reason, code := s.execute(r, req, b)
	s.closeBroker(r.ID)
	defer s.setStage(r.ID, "")

	// An Agent exiting cleanly says nothing about whether its work is any
	// good, so the Project's own checks decide (ADR-0013). They run under the
	// Service's own context and their own timeouts, not under the deadline
	// that bounds the writes afterwards - a check may take minutes. The Run
	// itself still succeeded: the Agent did its part, and Verification is what
	// refused it.
	refused := ""
	s.setStage(r.ID, StageFinishing)
	if outcome == OutcomeSucceeded && phase == PhaseExecute {
		s.setStage(r.ID, StageVerifying)
		refused = s.verify(s.ctx, j, r.ID, details, req)
		s.setStage(r.ID, StageFinishing)
		if s.ctx.Err() != nil {
			// The daemon stopped before Verification could finish, so nothing
			// has judged this work yet.
			outcome, reason, refused = OutcomeInterrupted, "the daemon stopped while verification was running", ""
		}
	}

	// The daemon may be stopping, so the bookkeeping does not run under the
	// Service's own context - and it gets its deadline here, after the work
	// that takes time.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), bookkeepingTimeout)
	defer cancel()

	// A planning Run has one more thing to do: what it decided is in the
	// handoff, and a planning Run that decided nothing has not succeeded.
	if outcome == OutcomeSucceeded && phase == PhasePlan {
		if err := s.recordPlan(ctx, j); err != nil {
			outcome, reason = OutcomeFailed, err.Error()
		}
	}

	if err := s.opts.Store.FinishRun(ctx, r.ID, s.now().UTC(), string(outcome), reason, code); err != nil {
		s.opts.Logger.Error("recording the end of a run", "run", r.ID, "error", err)
	}
	s.opts.Logger.Info("run finished",
		"run", r.ID, "job", r.JobID, "phase", phase, "outcome", outcome)

	switch {
	case outcome == OutcomeInterrupted:
		// The Run did not finish, so the Job goes back into the queue at the
		// place it kept while it ran (ADR-0011, ADR-0025).
		s.requeue(ctx, r.JobID, "returning an interrupted job to the queue")
	case outcome == OutcomeFailed:
		if err := s.opts.Store.DequeueJob(ctx, r.JobID, string(queue.StateBlocked), ""); err != nil {
			s.opts.Logger.Error("blocking a job", "job", r.JobID, "error", err)
		}
	case phase == PhasePlan:
		// The Job is planned, not finished: it waits its turn to be carried
		// out, in the place it already holds.
		s.requeue(ctx, r.JobID, "returning a planned job to the queue")
	case refused != "":
		// Verification refused the work. There is no retry: the Job waits for
		// the user with everything that is wrong attached (ADR-0013).
		if err := s.opts.Store.DequeueJob(ctx, r.JobID, string(queue.StateBlocked), refused); err != nil {
			s.opts.Logger.Error("blocking a job verification refused", "job", r.JobID, "error", err)
		}
	default:
		if err := s.opts.Store.DequeueJob(ctx, r.JobID, string(queue.StateReview), ""); err != nil {
			s.opts.Logger.Error("recording where a job got to", "job", r.JobID, "error", err)
		}
	}
}

// forgetWorktreeConfig takes away what Owl kept beside a worktree that has
// been reclaimed. Nothing reads it once the worktree is gone, and one
// directory per Job would otherwise stay for the life of the installation.
func (s *Service) forgetWorktreeConfig(job int64) {
	dir := s.worktreeConfigFor(job)
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		s.opts.Logger.Warn("reclaiming what was kept beside a worktree", "path", dir, "error", err)
	}
}

// worktreeConfigFor is where Owl keeps what belongs beside one Job's worktree,
// and is empty for a Service that was given nowhere to keep it.
func (s *Service) worktreeConfigFor(job int64) string {
	if s.opts.WorktreeConfigDir == "" {
		return ""
	}
	return filepath.Join(s.opts.WorktreeConfigDir, strconv.FormatInt(job, 10))
}

// placedBefore reports whether Owl has already placed Skills for a Job: what
// it keeps beside that worktree is made by placement and by nothing else.
func placedBefore(owned string) bool {
	if owned == "" {
		return false
	}
	_, err := os.Stat(owned)
	return err == nil
}

// placeSkills fetches what a Project declares and puts it in the Driver's own
// skills directory inside the Job's worktree, hidden from git (ADR-0033). It
// returns what was placed and what to record against the Run.
//
// A Skill pinned in the lockfile is fetched at the commit recorded there; one
// that asks to move on its own is resolved afresh at every Run. The lockfile
// comes from the Project's base branch, so an Agent cannot pull another
// version by editing the lock in its worktree (ADR-0014).
func (s *Service) placeSkills(ctx context.Context, j store.Job, details project.Details) (skill.Placement, []store.RunSkill, error) {
	declared := skill.Declare(details.Config)
	owned := s.worktreeConfigFor(j.ID)
	if len(declared) == 0 && !placedBefore(owned) {
		// Nothing declared and nothing placed before, so nothing is fetched,
		// nothing is placed, and the Project's own repository is not
		// reconfigured. A Project that wants no Skills should not be able to
		// tell that Skills exist.
		return skill.Placement{}, nil, nil
	}
	if s.opts.Skills == nil {
		if len(declared) == 0 {
			// Nothing to place and no way to take anything away, which is a
			// daemon nobody built for Skills rather than a Project's problem.
			return skill.Placement{}, nil, nil
		}
		return skill.Placement{}, nil, refused("project %s declares skills, but this daemon cannot fetch them", details.Name)
	}
	if owned == "" {
		return skill.Placement{}, nil, fmt.Errorf(
			"this daemon has nowhere to keep what hides a worktree's skills")
	}
	// A Project that has withdrawn its last Skill still has one to take away,
	// so placement runs with nothing to place rather than being skipped.
	resolved, used, err := s.opts.Skills.Prepare(ctx, declared, details.Lock)
	if err != nil {
		return skill.Placement{}, nil, &RefusedError{Err: err}
	}
	placed, err := skill.Place(skill.Site{
		Repo:      details.Path,
		Worktree:  j.Worktree,
		SkillsDir: s.opts.Driver.SkillsDir(),
		OwnedDir:  owned,
		Cache:     s.opts.Skills.Cache().Dir(),
	}, resolved)
	if err != nil {
		return skill.Placement{}, nil, &RefusedError{Err: err}
	}
	rows := make([]store.RunSkill, 0, len(used.Skills))
	for _, name := range used.Names() {
		u := used.Skills[name]
		rows = append(rows, store.RunSkill{
			Name: u.Name, Source: u.Source, Ref: u.Ref, Commit: u.Commit, Digest: u.Digest,
		})
	}
	return placed, rows, nil
}

// accountFor is the Account a Job runs on, with the credential to run it. A
// Job that has already run keeps the Account it ran on; one that has not takes
// the Account its Project's configuration names (ADR-0023). Neither being
// there is a refusal rather than a failure: nothing is wrong with the work,
// and the Job waits exactly where it was.
func (s *Service) accountFor(ctx context.Context, j store.Job, details project.Details) (account.Account, string, error) {
	name := j.Account
	if name == "" {
		name = details.Config.Account
	}
	if name == "" {
		return account.Account{}, "", refused(
			"project %s names no account to run on; put `account: <name>` in its .coding-owl.yaml on %s, and see owl account list for the accounts there are",
			details.Name, details.BaseBranch)
	}
	acct, token, err := s.opts.Accounts.Credential(ctx, name)
	if errors.Is(err, store.ErrAccountNotFound) {
		return account.Account{}, "", refused(
			"project %s runs on account %s, which is not there; owl account list shows the accounts there are",
			details.Name, name)
	}
	if err != nil {
		return account.Account{}, "", &RefusedError{Err: err}
	}
	return acct, token, nil
}

// nextRunnable takes the Job at the head of the queue, passing over one that
// has no attempts left and reporting it as exhausted on the way. A Job only
// reaches the queue with attempts to spend, so this holds that invariant
// rather than expecting to find a Job it catches: at zero a Job is never
// scheduled (ADR-0025), whatever put it there.
func (s *Service) nextRunnable(ctx context.Context) (store.Job, bool, error) {
	for {
		j, ok, err := s.opts.Store.NextQueued(ctx, string(queue.StatePending))
		if err != nil || !ok {
			return store.Job{}, false, err
		}
		if j.TTL > 0 {
			return j, true, nil
		}
		state, err := s.opts.Store.ReturnJobToQueue(ctx, j.ID,
			string(queue.StatePending), string(queue.StateExhausted))
		if err != nil {
			return store.Job{}, false, err
		}
		if queue.State(state) != queue.StateExhausted {
			// The Job is still at the head of the queue, so looking again
			// would find it again. Stopping says so instead of spinning.
			return store.Job{}, false, fmt.Errorf(
				"job %d has no attempts left but is %s", j.ID, state)
		}
		s.opts.Logger.Warn("a job out of attempts was still queued", "job", j.ID)
	}
}

// carry records whether this daemon has a Run going for a Job.
func (s *Service) carry(jobID int64, going bool) {
	s.carryingMu.Lock()
	defer s.carryingMu.Unlock()
	if going {
		s.carrying[jobID] = true
		return
	}
	delete(s.carrying, jobID)
}

// Carrying reports whether this daemon has a Run going for that Job. Every
// Agent is a child of the daemon (ADR-0012), so this daemon is the only one
// that could be, and an active Job it is not carrying is one a dead daemon
// left behind (ADR-0015).
func (s *Service) Carrying(jobID int64) bool {
	s.carryingMu.Lock()
	defer s.carryingMu.Unlock()
	return s.carrying[jobID]
}

// requeue puts a Job back in the queue after a Run that did not finish with
// it. A Job that has spent its last attempt is exhausted instead: it is not
// stuck on anything, it has simply had its Runs, and it wants a decision or
// owl jobs extend rather than another night (ADR-0025). what names the step
// for the log when it cannot be done.
func (s *Service) requeue(ctx context.Context, jobID int64, what string) {
	state, err := s.opts.Store.ReturnJobToQueue(ctx, jobID,
		string(queue.StatePending), string(queue.StateExhausted))
	if err != nil {
		s.opts.Logger.Error(what, "job", jobID, "error", err)
		return
	}
	if queue.State(state) == queue.StateExhausted {
		s.opts.Logger.Info("job out of attempts", "job", jobID)
	}
}

// prepare runs the Project's setup commands in the Job's worktree. A command
// that fails blocks the Job and says which one it was: nothing an Agent could
// do would help.
func (s *Service) prepare(ctx context.Context, j store.Job, setup []string) error {
	ctx, cancel := s.untilClosed(ctx)
	defer cancel()
	for _, cmd := range setup {
		res, err := shell.Run(ctx, j.Worktree, cmd, setupTimeout)
		failure := ""
		switch {
		case res.Cancelled || ctx.Err() != nil:
			// The daemon is stopping, or the caller hung up: the Job has not
			// failed at anything, so it stays where it was. This comes first,
			// because a command that never started reports the cancellation as
			// its own error.
			return fmt.Errorf("setup was stopped before %q finished", cmd)
		case err != nil:
			failure = fmt.Sprintf("the setup command %q could not be run: %v", cmd, err)
		case res.TimedOut:
			failure = fmt.Sprintf("the setup command %q timed out after %s", cmd, setupTimeout)
		case res.ExitCode != 0:
			failure = fmt.Sprintf("the setup command %q exited %d%s", cmd, res.ExitCode, quote(res.Output))
		default:
			continue
		}
		// Writing down why is bookkeeping, and outlives the context the
		// command itself ran under.
		write, cancelWrite := context.WithTimeout(context.WithoutCancel(s.ctx), bookkeepingTimeout)
		if err := s.opts.Store.DequeueJob(write, j.ID, string(queue.StateBlocked), failure); err != nil {
			s.opts.Logger.Error("blocking a job whose setup failed", "job", j.ID, "error", err)
		}
		cancelWrite()
		return errors.New(failure)
	}
	return nil
}

// verify runs the Project's checks over what the Run left in the worktree and
// records what each of them said. It returns the summary a blocked Job carries,
// or an empty string when nothing refused the work.
//
// cfg is the configuration read from the base branch before the Agent started.
// It is carried here rather than read again, because a Job's worktree shares
// the repository's refs: an Agent can move the base branch while it works, and
// what judges its work must be what was there before it did (ADR-0014,
// ADR-0030).
func (s *Service) verify(ctx context.Context, j store.Job, runID int64, details project.Details, req driver.Request) string {
	cfg := details.Config
	var results []verifier.Result
	if len(cfg.Checks) > 0 {
		if s.opts.Verifier == nil {
			// A Job must not reach review because nobody was asked (ADR-0013).
			return "verification could not be carried out: this daemon has no verifier"
		}
		got, err := s.opts.Verifier.Verify(ctx, verifier.Request{
			WorkingDir: j.Worktree, Checks: cfg.Checks,
		})
		if ctx.Err() != nil {
			// The daemon stopped part way through. Nothing has judged this
			// work, so nothing is written down about it: the Run is recorded
			// as interrupted and the Job waits its turn again.
			return ""
		}
		if err != nil {
			return fmt.Sprintf("verification could not be carried out: %v", err)
		}
		results = append(results, got...)
	}
	// The review comes after the Project's own checks: it is the expensive
	// opinion, and it reads the same work (ADR-0013). It runs whatever the
	// checks said, so a blocked Job reports everything that is wrong at once
	// rather than one thing a morning (ADR-0030).
	if cfg.Verification.Agent {
		var got []verifier.Result
		var err error
		if s.opts.AgentVerifier == nil {
			// A Job must not reach review because nobody was asked (ADR-0013).
			err = errors.New("this daemon has no agent verifier")
		} else {
			got, err = s.opts.AgentVerifier.Verify(ctx, s.agentRequest(j, details, req))
		}
		if ctx.Err() != nil {
			return ""
		}
		if err != nil {
			// A review that could not be carried out is a failure of its own,
			// recorded beside the checks that did run: what a Project's own
			// checks said is worth keeping whatever became of the review.
			got = []verifier.Result{{
				Name: config.AgentVerifierName, Verifier: verifier.KindAgent, ExitCode: store.NoExitCode,
				Reason: fmt.Sprintf("could not be carried out: %v", err),
			}}
		}
		results = append(results, got...)
	}
	if len(results) == 0 {
		return ""
	}
	rows := make([]store.CheckResult, 0, len(results))
	for _, r := range results {
		rows = append(rows, store.CheckResult(r))
	}
	// Writing down what the checks said is bookkeeping, and outlives a context
	// the checks themselves may have exhausted.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), bookkeepingTimeout)
	defer cancel()
	if err := s.opts.Store.SaveCheckResults(saveCtx, runID, rows); err != nil {
		s.opts.Logger.Error("recording what verification said", "run", runID, "error", err)
	}
	failed := verifier.Failed(results)
	if len(failed) == 0 {
		return ""
	}
	names := make([]string, 0, len(failed))
	for _, r := range failed {
		names = append(names, r.Name)
	}
	s.opts.Logger.Info("verification refused a run", "run", runID, "job", j.ID, "checks", strings.Join(names, ", "))
	return "verification failed: " + strings.Join(names, ", ")
}

// verifierSystemPrompt is what the agent Verifier is told it is for, for a
// Project that asks for one. Nothing Owl puts in front of an Agent is hidden
// (ADR-0017), and this is an Agent Owl starts.
func (s *Service) verifierSystemPrompt(cfg config.Config) string {
	if !cfg.Verification.Agent {
		return ""
	}
	// The Verifier says what it tells its own Agent, so the seam between them
	// stays the interface rather than the package (ADR-0005).
	if says, ok := s.opts.AgentVerifier.(interface{ SystemPrompt() string }); ok {
		return says.SystemPrompt()
	}
	return ""
}

// agentRequest is what the agent Verifier is given: the plan the work was meant to
// carry out, what the branch changed, and the Account and settings the Run it
// judges ran on (ADR-0013, ADR-0023).
//
// The diff is read here rather than by that Agent, so that what it judges is
// what the branch changed against the base branch the Run started from, and
// not whatever the Agent before it left the worktree looking at.
func (s *Service) agentRequest(j store.Job, details project.Details, req driver.Request) verifier.Request {
	out := verifier.Request{
		WorkingDir:   j.Worktree,
		Verification: details.Config.Verification,
		Plan:         j.Plan,
		Agent: verifier.Agent{
			ConfigDir: req.ConfigDir,
			Token:     req.Token,
			BudgetUSD: req.BudgetUSD,
			Model:     req.Model,
			Effort:    req.Effort,
		},
	}
	if j.Branch == "" {
		// A Job with no branch changed nothing, which is a thing to review
		// rather than a diff Owl failed to read.
		out.DiffComplete = true
		return out
	}
	patch, complete, err := git.DiffPatch(details.Path, details.BaseBranch, j.Branch, maxDiff)
	if err != nil {
		// A diff Owl cannot read is not a reason to skip the review: the
		// reviewer works in the worktree and can read it for itself.
		s.opts.Logger.Warn("reading what a run changed, for the review",
			"job", j.ID, "branch", j.Branch, "error", err)
		return out
	}
	out.Diff, out.DiffComplete = patch, complete
	return out
}

// global reads the daemon's own configuration, so an edit takes effect on the
// next Run rather than on the next restart.
func (s *Service) global() (config.Global, error) {
	if s.opts.ConfigPath == "" {
		return config.Global{}, nil
	}
	cfg, _, err := config.LoadGlobal(s.opts.ConfigPath)
	return cfg, err
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
		if s.ctx.Err() != nil {
			return OutcomeInterrupted, "the daemon stopped before the agent started", store.NoExitCode
		}
		return OutcomeFailed, err.Error(), store.NoExitCode
	}

	// From here the Run can be frozen, released and ended from outside
	// (ADR-0011).
	forget := s.track(r.ID, r.JobID, proc)
	defer forget()

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
	// The grace window ends a Run by stopping its Agent, so the Agent's death
	// is how this one was meant to end rather than a failure of its own.
	case s.interrupted(r.ID):
		return OutcomeInterrupted, expiredReason, store.NoExitCode
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
		Phase:    Phase(r.Phase),
	}
}
