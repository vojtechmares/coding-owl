package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
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
owl jobs drop; blocked Jobs say what refused them; and unfinished work is
what garbage collection found and would not touch.`,
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
	// The machine comes first, because it is the answer to why the rest of
	// this report says what it says (ADR-0011).
	// Both carry what something outside Owl said - a tool that could not be
	// read, a Project's setup command - so both reach the terminal as text.
	_, _ = fmt.Fprintln(env.Stdout, terminalSafe(machineLine(o.Machine)))
	if why := heldBy(o); why != "" {
		_, _ = fmt.Fprintf(env.Stdout, "nothing is running: %s\n", terminalSafe(why))
	}
	_, _ = fmt.Fprintln(env.Stdout)
	if o.Empty() {
		_, _ = fmt.Fprintln(env.Stdout, nothingToReport)
		return
	}
	if len(o.Running) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "runs in progress:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "RUN\tJOB\tPROJECT\tPHASE\tAGENT\tSTARTED")
		for _, r := range o.Running {
			_, _ = fmt.Fprintf(w, "%d\t%d\t%s\t%s\t%s\t%s\n",
				r.Run.ID, r.Job.ID, r.Job.Project, orNone(r.Run.Phase), runState(r.Run),
				r.Run.Started.UTC().Format(time.RFC3339))
		}
		_ = w.Flush()
		_, _ = fmt.Fprintln(env.Stdout)
	}
	printAccounts(env, o.Accounts)
	if len(o.Counts) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "jobs:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "STATE\tCOUNT")
		for _, c := range o.Counts {
			_, _ = fmt.Fprintf(w, "%s\t%d\n", c.State, c.Count)
		}
		_ = w.Flush()
	}
	// Unfinished work comes first of the three lists: nothing is going to
	// resolve it on its own, where the other two are waiting on a decision
	// somebody can take whenever they like (ADR-0015).
	if len(o.Unfinished) > 0 {
		_, _ = fmt.Fprintln(env.Stdout)
		printUnfinished(env, o.Unfinished)
	}
	printJobList(env, "awaiting a decision", o.Awaiting, false)
	printJobList(env, "blocked", o.Blocked, true)
	// Out of attempts is its own list, not part of blocked: nothing is wrong
	// with this work, it has simply had its Runs (ADR-0025).
	printJobList(env, "out of attempts", o.Exhausted, false)
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

// machineLine says what the machine is doing, in the terms the policy is in:
// how long it has been since anybody touched it, and what it is drawing from.
func machineLine(m client.Machine) string {
	if !m.Read {
		return "machine: unknown - " + orUnsaid(m.Detail)
	}
	power := "on battery"
	if m.OnPower {
		power = "on AC power"
	}
	if m.Idle {
		return fmt.Sprintf("machine: idle - no input for %s, %s", since(m.Since), power)
	}
	return fmt.Sprintf("machine: in use - input %s ago, %s", since(m.Since), power)
}

// since is a length of time as a person reads it, without the false precision
// of the milliseconds a machine keeps it in.
func since(d time.Duration) string {
	if d < time.Second {
		return d.Truncate(time.Millisecond).String()
	}
	return d.Truncate(time.Second).String()
}

// heldBy is why nothing is running, empty when something is or when nothing is
// holding the work back.
func heldBy(o client.Overview) string {
	if len(o.Running) > 0 {
		return ""
	}
	switch {
	case !o.Machine.Read, !o.Machine.Idle:
		return orUnsaid(o.Machine.Detail)
	case !pending(o):
		return "nothing is queued"
	case o.Holding != "":
		// The machine is one Owl may work on and something is queued, so what
		// is left is whatever refused the Job at the head of it.
		return o.Holding
	}
	return ""
}

// pending reports whether any Job is waiting to run.
func pending(o client.Overview) bool {
	for _, c := range o.Counts {
		if c.State == "pending" && c.Count > 0 {
			return true
		}
	}
	return false
}

// orUnsaid is a reason, or the plainest thing that can be said without one.
func orUnsaid(detail string) string {
	if strings.TrimSpace(detail) == "" {
		return "the machine could not be read"
	}
	return detail
}

// printAccounts says what each Account has used against what it is held to,
// and which of them is waiting for a window to start again (ADR-0020).
func printAccounts(env Env, accounts []client.AccountCeiling) {
	if len(accounts) == 0 {
		return
	}
	_, _ = fmt.Fprintln(env.Stdout, "\naccounts:")
	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ACCOUNT\tWINDOW\tUSED\tCEILING\tRESETS")
	for _, a := range accounts {
		if len(a.Windows) == 0 {
			// An Account nothing has been read about yet is still one somebody
			// may be waiting on, so it is named rather than left out.
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				a.Name, noneYet, noneYet, noneYet, noneYet)
			continue
		}
		for _, u := range a.Windows {
			used, resets := noneYet, noneYet
			if u.Read {
				used, resets = share(u.Utilization), u.Resets.UTC().Format(time.RFC3339)
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				a.Name, u.Name, used, orNoCeiling(u.Ceiling), resets)
		}
	}
	_ = w.Flush()
	// Which Account is waiting, and until when, below the table it is in: a
	// person reading why nothing ran wants the sentence, not the row.
	for _, a := range accounts {
		if a.Waiting == "" {
			continue
		}
		_, _ = fmt.Fprintf(env.Stdout, "\nwaiting: %s\n", terminalSafe(a.Waiting))
	}
	_, _ = fmt.Fprintln(env.Stdout)
}

// noneYet is what a column says about a window nobody has read yet.
const noneYet = "(none)"

// share is how much of a window is spent, as a person reads it.
func share(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

// orNoCeiling is a ceiling, or that there is none.
func orNoCeiling(v float64) string {
	if v <= 0 {
		return noneYet
	}
	return share(v)
}
