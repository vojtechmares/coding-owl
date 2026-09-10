package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// noConfigFound is what owl project show prints when no configuration file was
// found anywhere in the discovery order and the defaults apply.
const noConfigFound = "(none)"

func newProjectCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Register repositories as Projects and inspect how they are configured",
	}
	cmd.AddCommand(
		newProjectAddCmd(env),
		newProjectListCmd(env),
		newProjectShowCmd(env),
		newProjectMoveCmd(env),
		newProjectRenameCmd(env),
		newProjectRemoveCmd(env),
	)
	return cmd
}

// userPath makes a path absolute against the user's working directory, since
// the daemon resolves nothing on the caller's behalf.
func userPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func newProjectAddCmd(env Env) *cobra.Command {
	var name, baseBranch string
	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Register a git repository as a Project",
		Long: `Register a git repository as a Project.

The Project is named after the directory unless --name says otherwise, and
the name has to be unique: it is the Project's identity and it names the
Project's configuration directory.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userPath(args[0])
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				p, err := c.AddProject(ctx, path, name, baseBranch)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "registered %s at %s (base branch %s)\n", p.Name, p.Path, p.BaseBranch)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "name for the Project (default: the directory basename)")
	cmd.Flags().StringVar(&baseBranch, "base-branch", "", "branch Jobs branch from and configuration is read from (default: the repository's current branch)")
	return cmd
}

func newProjectListCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered Projects",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				ps, err := c.ListProjects(ctx)
				if err != nil {
					return err
				}
				if len(ps) == 0 {
					_, _ = fmt.Fprintln(env.Stdout, "no projects registered")
					return nil
				}
				w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "NAME\tPATH\tBASE BRANCH")
				for _, p := range ps {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.Path, p.BaseBranch)
				}
				return w.Flush()
			})
		},
	}
}

func newProjectShowCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a Project and the configuration in force for it",
		Long: `Show a Project and the configuration in force for it.

The config line names the file the configuration was read from. In-repo
files are read from the Project's base branch, never from a working tree,
so editing one on another branch changes nothing here.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				d, err := c.GetProject(ctx, args[0])
				if err != nil {
					return err
				}
				source := d.Config.Source
				if source == "" {
					source = noConfigFound
				}
				for _, kv := range [][2]string{
					{"name", d.Project.Name},
					{"path", d.Project.Path},
					{"base branch", d.Project.BaseBranch},
					{"registered", d.Project.Registered.UTC().Format(time.RFC3339)},
					{"config", source},
					{"branch prefix", d.Config.BranchPrefix},
				} {
					_, _ = fmt.Fprintf(env.Stdout, "%s: %s\n", kv[0], kv[1])
				}
				return nil
			})
		},
	}
}

func newProjectMoveCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "move <name> <path>",
		Short: "Point a Project at a new path",
		Long: `Point a Project at a new path.

Only the path changes: the name is the Project's identity, so its queued
work, its history and its configuration directory stay where they are.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := userPath(args[1])
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				p, err := c.MoveProject(ctx, args[0], path)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "moved %s to %s\n", p.Name, p.Path)
				return nil
			})
		},
	}
}

func newProjectRenameCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <name> <new-name>",
		Short: "Rename a Project and move its configuration directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				p, err := c.RenameProject(ctx, args[0], args[1])
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "renamed %s to %s\n", args[0], p.Name)
				return nil
			})
		},
	}
}

func newProjectRemoveCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Deregister a Project",
		Long: `Deregister a Project.

Its configuration directory under the config home is left alone: it is
hand-written and nothing else can put it back. The Jobs queued against it
go with it, since a Job whose Project is gone has nowhere to run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				jobs, err := c.RemoveProject(ctx, args[0])
				if err != nil {
					return err
				}
				if jobs == 0 {
					_, _ = fmt.Fprintf(env.Stdout, "removed %s\n", args[0])
					return nil
				}
				_, _ = fmt.Fprintf(env.Stdout, "removed %s, and the %s queued against it\n", args[0], plural(jobs, "job"))
				return nil
			})
		},
	}
}
