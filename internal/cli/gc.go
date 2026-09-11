package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// nothingCollected is what owl gc prints when the collection found nothing at
// all to do.
const nothingCollected = "nothing to do: every worktree is accounted for"

// collectTimeout bounds owl gc, which walks every worktree and asks git about
// every Project. That is more work than an ordinary call, and the daemon does
// it in one pass.
const collectTimeout = 10 * time.Minute

func newGCCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "gc",
		Short: "Reclaim finished worktrees and report what is unfinished",
		Long: `Reclaim finished worktrees and report what is unfinished.

The daemon runs this on its own when it starts and on an interval; this is
the same task on demand. It takes back the worktrees of Jobs nothing needs
any more, clears the worktree entries git is still counting, and finishes a
Job whose work you have already merged into its Project's base branch.

It never deletes work nobody has committed. A worktree holding changes, a
Job that has waited a long time for a decision, and a Job left behind by a
daemon that died are reported for you to deal with, and are what owl status
shows under unfinished work.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), collectTimeout)
			defer cancel()
			return withTimeout(ctx, env, func(ctx context.Context, c *client.Client) error {
				collection, err := c.Collect(ctx)
				if err != nil {
					return err
				}
				printCollection(env, collection)
				return nil
			})
		},
	}
}

// printCollection renders owl gc: what it took back, what it finished, what it
// pruned, and last what it would not touch - which is the part a person has to
// act on, and so is the part they are left looking at.
func printCollection(env Env, c client.Collection) {
	if c.Empty() {
		_, _ = fmt.Fprintln(env.Stdout, nothingCollected)
		return
	}
	if len(c.Reclaimed) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "reclaimed:")
		w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
		for _, r := range c.Reclaimed {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", r.Path, r.Why)
		}
		_ = w.Flush()
		_, _ = fmt.Fprintln(env.Stdout)
	}
	if len(c.Accepted) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "accepted:")
		for _, a := range c.Accepted {
			_, _ = fmt.Fprintf(env.Stdout, "job %d: %s is in %s\n", a.Job, a.Branch, a.Base)
		}
		_, _ = fmt.Fprintln(env.Stdout)
	}
	if len(c.Pruned) > 0 {
		_, _ = fmt.Fprintln(env.Stdout, "pruned:")
		for _, p := range c.Pruned {
			_, _ = fmt.Fprintf(env.Stdout, "%s: stale worktree entries cleared\n", p)
		}
		_, _ = fmt.Fprintln(env.Stdout)
	}
	printUnfinished(env, c.Unfinished)
}

// printUnfinished names what garbage collection would not touch. Nothing is
// going to resolve these on its own, which is why they are reported at all
// (ADR-0015).
func printUnfinished(env Env, unfinished []client.Unfinished) {
	if len(unfinished) == 0 {
		return
	}
	_, _ = fmt.Fprintln(env.Stdout, "unfinished work:")
	for _, u := range unfinished {
		_, _ = fmt.Fprintln(env.Stdout, u.Describe())
	}
}
