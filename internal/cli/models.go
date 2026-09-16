package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// newModelsCmd lists what a phase's model may be set to. The set belongs to
// the Driver rather than to Owl (ADR-0018), so it is asked for rather than
// printed from a list this command keeps: a Driver added later lists its own
// models here without this command changing.
func newModelsCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List the models a Job's phases can run on",
		Long: `List the models a Job's phases can run on.

A model is named vendor-first, as "anthropic/claude-opus". A versionless name
is an alias: it follows whatever the vendor currently calls its latest of that
model, so a Project written today keeps up on its own. A name with a version in
it pins that version and stays there.

Set one per phase in a Project's configuration:

  phases:
    plan:
      model: anthropic/claude-opus
    execute:
      model: anthropic/claude-sonnet

"owl jobs show" prints what each phase of a Job resolved to, and where it came
from.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
			defer cancel()
			driverName, models, err := client.New(env.Paths.SocketPath).DriverModels(ctx)
			if err != nil {
				return err
			}
			if len(models) == 0 {
				_, _ = fmt.Fprintf(env.Stdout, "%s names no models\n", terminalSafe(driverName))
				return nil
			}
			_, _ = fmt.Fprintf(env.Stdout, "models %s runs:\n\n", terminalSafe(driverName))
			w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "MODEL\tKIND\tABOUT")
			for _, m := range models {
				// A Driver's own words reach the terminal as text, as
				// everything Owl did not write does (issue #102).
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
					terminalSafe(m.Name), modelKind(m), terminalSafe(m.About))
			}
			_ = w.Flush()
			return nil
		},
	}
}

// modelKind says whether a name follows the vendor's latest or stays where it
// is, which is the one thing a reader has to know to choose between two names
// that otherwise look alike.
func modelKind(m client.DriverModel) string {
	if m.Alias {
		return "alias"
	}
	return "pinned"
}
