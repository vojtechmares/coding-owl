package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// emptyQueue is what owl queue list prints when nothing is waiting.
const emptyQueue = "queue is empty"

// noPosition stands in for the position of a Job that has left the queue.
const noPosition = "-"

// promptWidth is how much of a prompt owl queue list shows before cutting it
// short, so that one long prompt cannot push the other columns off screen.
const promptWidth = 60

func newAddCmd(env Env) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "add <prompt>",
		Short: "Queue a Job against a Project",
		Long: `Queue a Job against a Project.

Run inside a registered Project, the Job is queued against that Project;
anywhere else, name one with --project. The queue is first in, first out:
the Job goes behind everything already waiting, and owl queue reorder is
how it gets ahead of them.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				j, err := c.AddJob(ctx, projectName, args[0], env.workingDir())
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "queued job %d in %s at position %d\n", j.ID, j.Project, j.Position)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project to queue against (default: the Project the working directory is in)")
	return cmd
}

func newQueueCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Show and reorder the queued Jobs",
	}
	cmd.AddCommand(newQueueListCmd(env), newQueueRemoveCmd(env), newQueueReorderCmd(env))
	return cmd
}

func newQueueListCmd(env Env) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the queued Jobs in the order they will run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				jobs, err := c.ListJobs(ctx, all)
				if err != nil {
					return err
				}
				if len(jobs) == 0 {
					_, _ = fmt.Fprintln(env.Stdout, emptyQueue)
					return nil
				}
				w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "POSITION\tID\tPROJECT\tSTATE\tPROMPT")
				for _, j := range jobs {
					position := noPosition
					if j.Position > 0 {
						position = strconv.Itoa(j.Position)
					}
					_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", position, j.ID, j.Project, j.State, promptCell(j.Prompt))
				}
				return w.Flush()
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "list every Job, including those that have left the queue")
	return cmd
}

func newQueueRemoveCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Cancel a pending Job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0])
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				j, err := c.CancelJob(ctx, id)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "cancelled job %d in %s\n", j.ID, j.Project)
				return nil
			})
		},
	}
	return cmd
}

func newQueueReorderCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reorder <id> <position>",
		Short: "Move a pending Job to a position in the queue",
		Long: `Move a pending Job to a position in the queue.

Positions count from one, and the Jobs the moved one passes shift to make
room. This is how a Job is made urgent: there is no priority (ADR-0025).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := jobID(args[0])
			if err != nil {
				return err
			}
			// The position travels to the daemon as a 32-bit number, so it is
			// read as one here: a wider number would arrive truncated, and
			// the user would be told about a position they never asked for.
			position, err := strconv.ParseInt(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("positions are whole numbers counting from one; %q is not one", args[1])
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				j, err := c.ReorderJob(ctx, id, int(position))
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "moved job %d to position %d\n", j.ID, j.Position)
				return nil
			})
		},
	}
	return cmd
}

// jobID reads the id an owl queue command was given.
func jobID(arg string) (int64, error) {
	id, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("job ids are whole numbers; %q is not one", arg)
	}
	return id, nil
}

// plural renders a count with its noun, so a message can say one job and two
// jobs without saying "job(s)".
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// promptCell renders a prompt as one short line, since a prompt is free text
// and the listing is a table.
func promptCell(prompt string) string {
	line := strings.Join(strings.Fields(prompt), " ")
	runes := []rune(line)
	if len(runes) <= promptWidth {
		return line
	}
	return string(runes[:promptWidth-3]) + "..."
}
