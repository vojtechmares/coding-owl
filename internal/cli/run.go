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
)

// nothingPending is what owl start prints when the queue holds nothing to run.
const nothingPending = "nothing pending to run"

// noValue stands in for a field a Job does not have yet.
const noValue = "(none)"

func newStartCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Run the Job at the head of the queue",
		Long: `Run the Job at the head of the queue.

The Job gets a git worktree and a branch of its own, so an Agent never
touches your checkout, and the Run continues in the daemon after this
command returns. Follow it with owl logs.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
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

func newJobsCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "Inspect Jobs and what running them involves",
	}
	cmd.AddCommand(newJobsShowCmd(env))
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
		{"prompt", d.Job.Prompt},
		{"branch", orNone(d.Job.Branch)},
		{"worktree", orNone(d.Job.Worktree)},
		{"planned", yesNo(d.Job.Planned)},
		{"source", d.Job.Source + ":" + d.Job.SourceRef},
		{"created", d.Job.Created.UTC().Format(time.RFC3339)},
	} {
		_, _ = fmt.Fprintf(env.Stdout, "%s: %s\n", kv[0], kv[1])
	}
	if reason := whyHere(d); reason != "" {
		_, _ = fmt.Fprintf(env.Stdout, "reason: %s\n", reason)
	}
	if len(d.Runs) == 0 {
		// "none" rather than the placeholder used for a missing value: there
		// is nothing missing about a Job that has not run yet.
		_, _ = fmt.Fprintln(env.Stdout, "runs: none")
	} else {
		_, _ = fmt.Fprintln(env.Stdout, "runs:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "RUN\tATTEMPT\tPHASE\tOUTCOME\tEXIT\tSTARTED\tENDED\tLOG")
		for _, r := range d.Runs {
			_, _ = fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.ID, r.Attempt, orNone(r.Phase), orRunning(r.Outcome), exitStatus(r.ExitCode),
				r.Started.UTC().Format(time.RFC3339), stamp(r.Ended), r.LogPath)
		}
		_ = w.Flush()
	}
	printChecks(env, d.Checks)
	printPhases(env, d.Phases)
	if plan := strings.TrimRight(d.Job.Plan, "\n"); plan != "" {
		_, _ = fmt.Fprintf(env.Stdout, "\nplan:\n%s\n", plan)
	} else {
		_, _ = fmt.Fprintf(env.Stdout, "\nplan: %s\n", noValue)
	}
	_, _ = fmt.Fprintf(env.Stdout, "\nsystem prompt:\n%s\n", d.SystemPrompt)
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
	if d.Job.Note != "" {
		return d.Job.Note
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
		if c.Passed {
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

// orRunning names the outcome of a Run that has not ended yet.
func orRunning(outcome string) string {
	if outcome == "" {
		return "running"
	}
	return outcome
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
