package cli

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// nothingToReport is what owl status prints when there is no work at all.
const nothingToReport = "nothing queued, running or waiting for a decision"

func newStatusCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report where the work stands",
		Long: `Report where the work stands.

This is the morning question: what ran while you were away, what is running
now, and what is waiting for you. Jobs in review want owl jobs accept or
owl jobs drop; blocked Jobs say what refused them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				o, err := c.GetOverview(ctx)
				if err != nil {
					return err
				}
				printOverview(env, o)
				return nil
			})
		},
	}
}

// printOverview renders owl status: what is running first, because it is
// happening now, then how the Jobs stand, then the two lists that want a
// person - what awaits a decision, and what is stuck.
func printOverview(env Env, o client.Overview) {
	if o.Empty() {
		_, _ = fmt.Fprintln(env.Stdout, nothingToReport)
		return
	}
	if len(o.Running) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "runs in progress:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "RUN\tJOB\tPROJECT\tPHASE\tSTARTED")
		for _, r := range o.Running {
			_, _ = fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\n",
				r.Run.ID, r.Job.ID, r.Job.Project, orNone(r.Run.Phase),
				r.Run.Started.UTC().Format(time.RFC3339))
		}
		_ = w.Flush()
		_, _ = fmt.Fprintln(env.Stdout)
	}
	if len(o.Counts) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "jobs:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "STATE\tCOUNT")
		for _, c := range o.Counts {
			_, _ = fmt.Fprintf(w, "%s\t%d\n", c.State, c.Count)
		}
		_ = w.Flush()
	}
	printJobList(env, "awaiting a decision", o.Awaiting, false)
	printJobList(env, "blocked", o.Blocked, true)
}

// printJobList names the Jobs a person has to do something about. The reason
// is only worth a column for the Jobs that are stuck on one.
func printJobList(env Env, heading string, jobs []client.Job, reason bool) {
	if len(jobs) == 0 {
		return
	}
	_, _ = fmt.Fprintf(env.Stdout, "\n%s:\n", heading)
	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	header := "JOB\tPROJECT\tPROMPT"
	if reason {
		header += "\tREASON"
	}
	_, _ = fmt.Fprintln(w, header)
	for _, j := range jobs {
		row := strconv.FormatInt(j.ID, 10) + "\t" + j.Project + "\t" + promptCell(j.Prompt)
		if reason {
			row += "\t" + orNone(promptCell(j.Reason))
		}
		_, _ = fmt.Fprintln(w, row)
	}
	_ = w.Flush()
}
