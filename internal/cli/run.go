package cli

import (
	"context"
	"errors"
	"fmt"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/verifier"
)

// nothingPending is what owl start prints when the queue holds nothing to run.
const nothingPending = "nothing pending to run"

// startTimeout bounds owl start, which waits for the Project's setup commands
// before the Run begins. Each of those is bounded by the daemon; this only has
// to outlast them.
const startTimeout = time.Hour

// noValue stands in for a field a Job does not have yet.
const noValue = "(none)"

func newStartCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run the Job at the head of the queue",
		Long: `Run the Job at the head of the queue.

The Job gets a git worktree and a branch of its own, so an Agent never
touches your checkout. This command waits while the Project's setup
commands prepare that worktree, and returns once the Agent has started;
the Run itself continues in the daemon. Follow it with owl logs.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Starting a Run includes the Project's setup commands, which
			// fetch dependencies and are slow by nature. The daemon bounds
			// each of them, so this only has to be longer than they are - and
			// Ctrl-C is how a person stops waiting.
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, startTimeout)
			defer cancel()
			return withTimeout(ctx, env, func(ctx context.Context, c *client.Client) error {
				job, run, started, err := c.StartRun(ctx)
				if err != nil {
					return err
				}
				if !started {
					_, _ = fmt.Fprintln(env.Stdout, nothingPending)
					return nil
				}
				_, _ = fmt.Fprintf(env.Stdout, "started run %d for job %d in %s\n", run.ID, job.ID, job.Project)
				_, _ = fmt.Fprintf(env.Stdout, "follow it with: owl logs %d -f\n", run.ID)
				return nil
			})
		},
	}
}

func newLogsCmd(env Env) *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <run>",
		Short: "Print what an Agent wrote during a Run",
		Long: `Print what an Agent wrote during a Run.

The output is the Agent's own structured stream, one JSON event per line,
exactly as it was captured. With --follow it keeps printing until the Run
ends.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := runID(args[0])
			if err != nil {
				return err
			}
			// Following has no timeout: a Run takes as long as it takes, and
			// Ctrl-C is how a person stops watching.
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			if !follow {
				var deadline context.CancelFunc
				ctx, deadline = context.WithTimeout(ctx, callTimeout)
				defer deadline()
			}
			err = client.New(env.Paths.SocketPath).StreamRunLog(ctx, id, follow, func(line string) error {
				_, err := fmt.Fprintln(env.Stdout, line)
				return err
			})
			// Ctrl-C is how a person stops watching, not a failure.
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing until the Run ends")
	return cmd
}

// interruptCmd builds owl pause and owl resume, which differ only in what they
// ask the daemon to do to the Run in progress.
func interruptCmd(env Env, verb, short, long string, act func(context.Context, *client.Client) ([]client.Run, []string, error), done string) *cobra.Command {
	return &cobra.Command{
		Use:   verb,
		Short: short,
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				r, notReached, err := act(ctx, c)
				if err != nil {
					return err
				}
				// One line each: more than one Run may be going (ADR-0021),
				// and a person who froze four of them should see four.
				for _, one := range r {
					_, _ = fmt.Fprintf(env.Stdout, "run %d of job %d is %s\n", one.ID, one.JobID, done)
				}
				// And a line each for the ones this did not reach - already
				// paused, being ended - which would otherwise go unsaid
				// because something else did happen.
				for _, why := range notReached {
					_, _ = fmt.Fprintln(env.Stdout, terminalSafe(why))
				}
				return nil
			})
		},
	}
}

func newPauseCmd(env Env) *cobra.Command {
	return interruptCmd(env, "pause",
		"Freeze every Run in flight and give the machine back",
		`Freeze every Run in flight and give the machine back.

The Agents and everything they started - test runners, compilers, package
managers - stop where they are, so the machine is yours again immediately.
Nothing is lost: owl resume continues the same Run where it was.

A Run that stays frozen for the whole grace window is ended rather than left
holding its sockets, and its Job goes back in the queue to be carried on from
its handoff. The window is graceWindow in the daemon's configuration, and is
fifteen minutes unless that says otherwise.`,
		func(ctx context.Context, c *client.Client) ([]client.Run, []string, error) {
			return c.PauseRun(ctx)
		},
		"frozen")
}

func newResumeCmd(env Env) *cobra.Command {
	return interruptCmd(env, "resume",
		"Continue the Runs that are frozen",
		`Continue the Runs that are frozen.

The same Runs carry on where they were, with everything the Agent had in mind
still in its head - nothing was ended, only stopped.`,
		func(ctx context.Context, c *client.Client) ([]client.Run, []string, error) {
			return c.ResumeRun(ctx)
		},
		"running again")
}

func newJobsCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Inspect Jobs and what running them involves",
	}
	cmd.AddCommand(newJobsShowCmd(env), newJobsExtendCmd(env), newJobsAcceptCmd(env), newJobsDropCmd(env))
	return cmd
}

// disposeCmd builds owl jobs accept and owl jobs drop, which differ only in
// what they do with the branch and what they leave the Job as.
func disposeCmd(env Env, verb, short, long string, dispose func(context.Context, *client.Client, int64, bool) (client.Job, error)) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   verb + " <id>",
		Short: short,
		Long:  long,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0])
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				j, err := dispose(ctx, c, id, force)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "job %d is %s\n", j.ID, j.State)
				if j.Branch != "" {
					_, _ = fmt.Fprintf(env.Stdout, "branch: %s\n", j.Branch)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false,
		"reclaim the worktree even though it holds changes nobody has committed")
	return cmd
}

func newJobsAcceptCmd(env Env) *cobra.Command {
	return disposeCmd(env, "accept",
		"Keep a Job's work and reclaim its worktree",
		`Keep a Job's work and reclaim its worktree.

The branch stays exactly where the Agent left it, with every commit on it:
merging, rebasing or pushing that branch is yours to do, and Owl never does
it for you. Only the worktree goes, freeing the disk it held.

A worktree holding changes nobody has committed is refused, because
reclaiming it would destroy them. Pass --force to reclaim it anyway.`,
		func(ctx context.Context, c *client.Client, id int64, force bool) (client.Job, error) {
			return c.AcceptJob(ctx, id, force)
		})
}

func newJobsDropCmd(env Env) *cobra.Command {
	return disposeCmd(env, "drop",
		"Refuse a Job's work, deleting its branch and its worktree",
		`Refuse a Job's work, deleting its branch and its worktree.

Both go: the branch is deleted whether or not it was merged anywhere, and
the worktree with it. Accept the Job instead if you want to keep what the
Agent wrote.

A worktree holding changes nobody has committed is refused. Pass --force to
drop it anyway.`,
		func(ctx context.Context, c *client.Client, id int64, force bool) (client.Job, error) {
			return c.DropJob(ctx, id, force)
		})
}

func newJobsExtendCmd(env Env) *cobra.Command {
	var ttl int
	cmd := &cobra.Command{
		Use:   "extend <id>",
		Short: "Give a Job more attempts",
		Long: `Give a Job more attempts.

A Job may take ten Runs before it is exhausted, and a Run that was
interrupted costs one exactly as a failure does. Extending adds to what is
left rather than replacing it, and a Job that had run out goes back into
the queue at the place it kept.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0])
			if err != nil {
				return err
			}
			// Zero is how the daemon is told to add its own default, so an
			// explicit zero is refused here rather than read as one.
			if cmd.Flags().Changed("ttl") && ttl < 1 {
				return fmt.Errorf("attempts count from one; --ttl %d is not a number of runs to add", ttl)
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				j, err := c.ExtendJob(ctx, id, ttl)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "job %d is %s with %s left\n",
					j.ID, j.State, plural(j.TTL, "attempt"))
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&ttl, "ttl", 0, "how many Runs to add (default: ten)")
	return cmd
}

func newJobsShowCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a Job, its Runs and the system prompt in force for it",
		Long: `Show a Job, its Runs and the system prompt in force for it.

The system prompt is Owl's standing unattended contract with the Project's
own clauses after it, exactly as the Agent is given it: nothing Owl injects
into a Run is hidden.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0])
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				d, err := c.GetJob(ctx, id)
				if err != nil {
					return err
				}
				printJob(env, d)
				return nil
			})
		},
	}
}

// printJob renders owl jobs show, ending with the system prompt so that a
// prompt of any length cannot push the rest of the report off the screen.
func printJob(env Env, d client.JobDetails) {
	for _, kv := range [][2]string{
		{"id", strconv.FormatInt(d.Job.ID, 10)},
		{"project", d.Job.Project},
		{"state", d.Job.State},
		{"attempts", strconv.Itoa(d.Job.TTL) + " left"},
		{"account", orNone(d.Job.Account)},
		{"prompt", d.Job.Prompt},
		{"branch", orNone(d.Job.Branch)},
		{"worktree", orNone(d.Job.Worktree)},
		{"planned", yesNo(d.Job.Planned)},
		{"source", d.Job.Source + ":" + d.Job.SourceRef},
		{"created", d.Job.Created.UTC().Format(time.RFC3339)},
		{"diff", diffLine(d.Diff)},
	} {
		_, _ = fmt.Fprintf(env.Stdout, "%s: %s\n", kv[0], terminalSafe(kv[1]))
	}
	if reason := whyHere(d); reason != "" {
		_, _ = fmt.Fprintf(env.Stdout, "reason: %s\n", terminalSafe(reason))
	}
	if len(d.Runs) == 0 {
		// "none" rather than the placeholder used for a missing value: there
		// is nothing missing about a Job that has not run yet.
		_, _ = fmt.Fprintln(env.Stdout, "runs: none")
	} else {
		_, _ = fmt.Fprintln(env.Stdout, "runs:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "RUN\tATTEMPT\tPHASE\tSTATE\tEXIT\tSTARTED\tENDED\tLOG")
		for _, r := range d.Runs {
			_, _ = fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.ID, r.Attempt, orNone(r.Phase), runState(r), exitStatus(r.ExitCode),
				r.Started.UTC().Format(time.RFC3339), stamp(r.Ended), r.LogPath)
		}
		_ = w.Flush()
	}
	printRunSkills(env, d.Runs)
	printRunPermissions(env, d.Runs)
	printChecks(env, d.Checks)
	printPhases(env, d.Phases)
	// The plan and the handoff are what an unattended Agent wrote (ADR-0026),
	// and the system prompt carries the Project's own clauses (ADR-0017): each
	// is read in the morning, and reaches the terminal as text, with the
	// newlines and tabs a document is printed with.
	if plan := strings.TrimRight(d.Job.Plan, "\n"); plan != "" {
		_, _ = fmt.Fprintf(env.Stdout, "\nplan:\n%s\n", terminalSafe(plan))
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "\nplan: %s\n", noValue)
	}
	if handoff := strings.TrimRight(d.Handoff, "\n"); handoff != "" {
		_, _ = fmt.Fprintf(env.Stdout, "\nhandoff:\n%s\n", terminalSafe(handoff))
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "\nhandoff: %s\n", noValue)
	}
	_, _ = fmt.Fprintf(env.Stdout, "\nsystem prompt:\n%s\n", terminalSafe(d.SystemPrompt))
	// A Project that asked for the agent Verifier gets a second Agent, with a
	// contract of its own: nothing Owl puts in front of one is hidden
	// (ADR-0017).
	if d.VerifierSystemPrompt != "" {
		_, _ = fmt.Fprintf(env.Stdout, "\nverifier system prompt:\n%s\n", terminalSafe(d.VerifierSystemPrompt))
	}
}

// printRunSkills reports what each Run read, so that what an Agent did is
// attributable to the instructions it had (ADR-0024). The most recent Run
// comes first, because that is the one a reader is asking about.
func printRunSkills(env Env, runs []client.Run) {
	var reported bool
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if len(r.Skills) == 0 {
			continue
		}
		if !reported {
			_, _ = fmt.Fprintln(env.Stdout, "\nskills:")
			reported = true
		}
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintf(w, "RUN %d\tSOURCE\tREF\tCOMMIT\n", r.ID)
		for _, s := range r.Skills {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Name, s.Source, orNone(s.Ref), short(s.Commit))
		}
		_ = w.Flush()
	}
}

// printRunPermissions reports what each Run's Agent was allowed to do, so that
// what it did is attributable to what it was granted (ADR-0035). The most
// recent Run comes first, as for what it read.
func printRunPermissions(env Env, runs []client.Run) {
	var reported bool
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if len(r.Permissions) == 0 {
			continue
		}
		if !reported {
			_, _ = fmt.Fprintln(env.Stdout, "\npermissions:")
			reported = true
		}
		_, _ = fmt.Fprintf(env.Stdout, "RUN %d: %s\n", r.ID, strings.Join(r.Permissions, ", "))
	}
}

// printPhases reports what each phase of the Job would run at, and where each
// setting came from, so a surprising value can be traced to the file that set
// it (ADR-0028).
func printPhases(env Env, phases []client.PhaseSettings) {
	if len(phases) == 0 {
		return
	}
	_, _ = fmt.Fprintln(env.Stdout, "\nphases:")
	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PHASE\tMODEL\tFROM\tEFFORT\tFROM")
	for _, p := range phases {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			p.Phase, orNone(p.Model), orNone(p.ModelFrom), orNone(p.Effort), orNone(p.EffortFrom))
	}
	_ = w.Flush()
}

// whyHere is why the Job is where it is: what the Job itself records - a setup
// command that failed, or the checks that refused the work - and otherwise why
// its most recent Run did not succeed.
func whyHere(d client.JobDetails) string {
	if d.Job.Reason != "" {
		return d.Job.Reason
	}
	if len(d.Runs) == 0 {
		return ""
	}
	return d.Runs[len(d.Runs)-1].Error
}

// printChecks reports what Verification said, every check of it: a Job that
// was refused says everything that is wrong at once (ADR-0030). A failing
// check's output follows it, indented.
func printChecks(env Env, checks []client.CheckResult) {
	if len(checks) == 0 {
		return
	}
	_, _ = fmt.Fprintln(env.Stdout, "\nchecks:")
	for _, c := range checks {
		verdict := "passed"
		if !c.Passed {
			verdict = "failed"
			if c.Reason != "" {
				verdict += " (" + c.Reason + ")"
			}
		}
		_, _ = fmt.Fprintf(env.Stdout, "- %s: %s\n", c.Name, verdict)
		// A check's output is evidence for a failure, and noise when it
		// passed. The agent Verifier's findings are its answer either way: one
		// that passed the work with a note wrote that note to be read
		// (ADR-0013).
		if c.Passed && c.Verifier != verifier.KindAgent {
			continue
		}
		for _, ln := range strings.Split(strings.TrimRight(c.Output, "\n"), "\n") {
			if ln == "" {
				continue
			}
			_, _ = fmt.Fprintf(env.Stdout, "    %s\n", ln)
		}
	}
}

// yesNo renders a flag the way a person reads one.
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orNone(s string) string {
	if s == "" {
		return noValue
	}
	return s
}

// runState is how a Run ended, or what it is doing while it has not.
func runState(r client.Run) string {
	switch {
	case r.Outcome != "":
		return r.Outcome
	case r.Paused:
		return "paused"
	case r.Stage == "" || r.Stage == "agent":
		return "running"
	default:
		// Where the Run is when its Agent is not the answer: starting,
		// verifying or finishing, in the daemon's own words.
		return r.Stage
	}
}

// exitStatus renders what the Agent exited with, or nothing for a Run that
// never got far enough to have a status.
func exitStatus(code int) string {
	if code < 0 {
		return noValue
	}
	return strconv.Itoa(code)
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return noValue
	}
	return t.UTC().Format(time.RFC3339)
}

// runID reads the id owl logs was given.
func runID(arg string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("run ids are whole numbers; %q is not one", arg)
	}
	return id, nil
}

// diffLine sums up what a Job's branch changed, or says there is nothing to
// sum up.
func diffLine(d client.DiffSummary) string {
	if len(d.Files) == 0 {
		return noValue
	}
	files := "files"
	if len(d.Files) == 1 {
		files = "file"
	}
	return fmt.Sprintf("%d %s changed, %d added, %d removed", len(d.Files), files, d.Insertions, d.Deletions)
}
