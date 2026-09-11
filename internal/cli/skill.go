package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// noSkills is what owl skills list prints for a Project that declares none.
const noSkills = "no skills yet; owl skills add <source> declares one"

// fetchTimeout bounds the Skills commands, which talk to somebody else's git
// server. Longer than an ordinary call, and short enough that a source that
// never answers is a failure rather than a wait.
const fetchTimeout = 5 * time.Minute

// shortCommit is how much of a commit a listing shows: enough to recognise,
// short enough to sit in a column.
const shortCommit = 12

func newSkillsCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage the Skills a Project gives its Agents",
		Long: `Manage the Skills a Project gives its Agents.

A Skill is reusable instruction - house style, a review checklist, domain
conventions - that every Job in a Project gets. Owl fetches it itself and
places it in the tool's own skills directory inside the Job's worktree,
where git cannot see it.

Skills are pinned by default: the configuration says which ref to follow,
and the lockfile beside it records the commit that ref resolved to. They
move when you say so, with owl skills update, or when they are declared to
update on their own.

Skills belong to a Project, so these commands are run inside one, or name
one with --project.`,
	}
	cmd.AddCommand(newSkillsAddCmd(env), newSkillsListCmd(env),
		newSkillsRemoveCmd(env), newSkillsUpdateCmd(env))
	return cmd
}

// skillRequest is the Project a Skills command is about.
func skillRequest(env Env, projectName string) client.SkillRequest {
	return client.SkillRequest{Project: projectName, WorkingDir: env.workingDir()}
}

// withFetch runs fn against the daemon under the longer deadline the Skills
// commands need, since they talk to somebody else's git server.
func withFetch(cmd *cobra.Command, env Env, fn func(context.Context, *client.Client) error) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), fetchTimeout)
	defer cancel()
	return withTimeout(ctx, env, fn)
}

func newSkillsAddCmd(env Env) *cobra.Command {
	var projectName, ref string
	var autoUpdate bool
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Declare a Skill for this Project",
		Long: `Declare a Skill for this Project.

The source is a repository: a git URL, an owner/repo shorthand, or a local
path. Owl resolves the ref, fetches what it points at, and records both the
declaration and what it resolved to.

Without --ref it follows the source's default branch. With --auto-update it
is re-resolved at the start of every Run; without it, it stays where it is
until owl skills update says otherwise.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withFetch(cmd, env, func(ctx context.Context, c *client.Client) error {
				s, files, err := c.AddSkill(ctx, skillRequest(env, projectName), args[0], ref, autoUpdate)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "skill %s added at %s, which is %s\n",
					s.Name, s.Ref, short(s.Commit))
				printSkillFiles(env, files)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project to declare it for (default: the Project the working directory is in)")
	cmd.Flags().StringVar(&ref, "ref", "", "branch or tag to follow (default: the source's own default branch)")
	cmd.Flags().BoolVar(&autoUpdate, "auto-update", false, "re-resolve it at the start of every Run, rather than pinning it")
	return cmd
}

func newSkillsListCmd(env Env) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Skills this Project declares",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				skills, _, err := c.ListSkills(ctx, skillRequest(env, projectName))
				if err != nil {
					return err
				}
				if len(skills) == 0 {
					_, _ = fmt.Fprintln(env.Stdout, noSkills)
					return nil
				}
				printSkills(env, skills)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project to list (default: the Project the working directory is in)")
	return cmd
}

func newSkillsRemoveCmd(env Env) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Stop giving this Project's Agents a Skill",
		Long: `Stop giving this Project's Agents a Skill.

It goes from the configuration and from the lockfile. What was fetched
stays in the cache, which nothing reclaims yet; the next Run simply stops
placing it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				s, files, err := c.RemoveSkill(ctx, skillRequest(env, projectName), args[0])
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "skill %s removed\n", s.Name)
				printSkillFiles(env, files)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project to remove from (default: the Project the working directory is in)")
	return cmd
}

func newSkillsUpdateCmd(env Env) *cobra.Command {
	var projectName string
	cmd := &cobra.Command{
		Use:   "update [name...]",
		Short: "Re-resolve Skills against their refs",
		Long: `Re-resolve Skills against their refs.

With no names, only the Skills declared to update on their own are
re-resolved: everything else is pinned, and moving it is deliberate. Name a
Skill and it is re-resolved whether it is pinned or not, which is how a
pinned Skill moves.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withFetch(cmd, env, func(ctx context.Context, c *client.Client) error {
				updated, all, files, err := c.UpdateSkills(ctx, skillRequest(env, projectName), args)
				if err != nil {
					return err
				}
				if len(updated) == 0 {
					_, _ = fmt.Fprintln(env.Stdout, "nothing moved: every skill is already at what its ref resolves to")
				} else {
					_, _ = fmt.Fprintln(env.Stdout, "updated:")
					for _, s := range updated {
						_, _ = fmt.Fprintf(env.Stdout, "%s is now %s, which is %s\n", s.Name, s.Ref, short(s.Commit))
					}
					_, _ = fmt.Fprintln(env.Stdout)
				}
				if len(all) > 0 {
					printSkills(env, all)
				}
				printSkillFiles(env, files)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&projectName, "project", "", "Project to update (default: the Project the working directory is in)")
	return cmd
}

// printSkills renders the listing the Skills commands share.
func printSkills(env Env, skills []client.Skill) {
	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tSOURCE\tREF\tCOMMIT\tMOVES")
	for _, s := range skills {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.Name, s.Source, orNone(s.Ref), short(s.Commit), moves(s.AutoUpdate))
	}
	_ = w.Flush()
}

// moves says whether a Skill may move on its own. Pinned is the default, and
// is what a reader should be able to see at a glance (ADR-0024).
func moves(autoUpdate bool) string {
	if autoUpdate {
		return "tracking"
	}
	return "pinned"
}

// short is a commit as a listing shows it, and "(none)" for one nothing has
// resolved yet.
func short(commit string) string {
	if commit == "" {
		return noValue
	}
	if len(commit) > shortCommit {
		return commit[:shortCommit]
	}
	return commit
}

// printSkillFiles says which files changed, and that a Project configured in
// its own repository needs them committed before a Run reads them: the daemon
// reads the base branch, never a working tree (ADR-0014).
func printSkillFiles(env Env, files client.SkillFiles) {
	if !files.InRepo {
		return
	}
	_, _ = fmt.Fprintf(env.Stdout, "commit %s and %s: a run reads them from the base branch\n",
		files.Manifest, files.Lock)
}
