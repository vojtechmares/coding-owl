package cli

// owl project setup: the two questions a Project cannot run without answers
// to (issue #126).
//
// The answers are read from standard input rather than from a terminal
// device, so the command works typed and piped alike, and running out of
// input ends it with a message rather than a wait. That is the same reading
// `owl account add` does for a pasted token.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// maxAnswer bounds what is read as one answer, so that a mistaken
// `owl project setup < some-huge-file` is refused rather than held in memory.
const maxAnswer = 4 << 10

// configDocs is where the rest of what a configuration file can carry is
// written down. It is the guide's Configuration section, which is where it
// lives today; issue #124 gives it pages of its own.
const configDocs = "https://codingowl.dev/docs/guide#configuration"

func newProjectSetupCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "setup [project]",
		Short: "Give a Project the Account its Jobs run on",
		Long: `Give a Project the Account its Jobs run on.

The Project is the one named, or the one whose directory holds the path
named, or the one the command was run in.

A Project that names no Account cannot run a single Job. This asks which
Account to use and where its configuration file should live, and writes
those two settings and nothing else.

The file goes either in the repository, as .coding-owl.yaml to be
committed, or in Owl's configuration home, where it is local to this
machine and not committed. An in-repo file is read from the Project's base
branch, never from a working tree, so it does nothing until it is committed.

A Project that already has a configuration file is left alone.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var named, path string
			if len(args) == 1 {
				named = args[0]
				// Sent alongside the name it might be, because the daemon
				// resolves nothing on the caller's behalf: `.` and
				// `../other` mean nothing to a process started elsewhere.
				// Which of the two it is stays the daemon's to decide.
				abs, err := userPath(named)
				if err != nil {
					return err
				}
				path = abs
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				return projectSetup(ctx, env, c, named, path)
			})
		},
	}
}

func projectSetup(ctx context.Context, env Env, c *client.Client, named, path string) error {
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return errors.New("there is no account for this project's jobs to run on; owl account add <name> makes one")
	}
	ask := newAsker(env)
	account, err := pickAccount(env, ask, accounts)
	if err != nil {
		return err
	}
	inRepo, err := pickLocation(env, ask)
	if err != nil {
		return err
	}
	file, err := c.SetupProject(ctx, named, path, env.workingDir(), account, inRepo)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(env.Stdout, "\nwrote %s\n", terminalSafe(file.Path))
	if file.InRepo {
		_, _ = fmt.Fprintln(env.Stdout,
			"commit it to the base branch: a run reads a project's configuration from there, never from a working tree")
	} else {
		_, _ = fmt.Fprintln(env.Stdout,
			"it is read from where it is, so the project runs on that account from now; it is not committed anywhere")
	}
	_, _ = fmt.Fprintf(env.Stdout,
		"\nthat is the one setting a project cannot run without. What else a configuration\n"+
			"file can carry - the checks that judge a run, what an agent is allowed to do,\n"+
			"the skills it reads - is documented at %s\n"+
			"Adding them is a good first task for a coding agent: ask yours to add checks to\n"+
			"%s based on this project's own test suite.\n",
		configDocs, terminalSafe(file.Path))
	return nil
}

// pickAccount asks which Account the Project's Jobs run on, showing what each
// one is and whether it has a credential: an Account with none cannot run
// anything yet, which is worth seeing before it is chosen.
func pickAccount(env Env, ask *asker, accounts []client.Account) (string, error) {
	_, _ = fmt.Fprintln(env.Stdout, "Which account should this project's jobs run on?")
	choices := make([]string, 0, len(accounts))
	for i, a := range accounts {
		credential := ""
		if !a.HasCredential {
			credential = "  (no credential yet)"
		}
		_, _ = fmt.Fprintf(env.Stdout, "  %d) %s%s\n", i+1, terminalSafe(a.Name), credential)
		choices = append(choices, a.Name)
	}
	return ask.choose(env, "account", choices)
}

// pickLocation asks where the file goes. The in-repo form is first because it
// is the one a reader of the repository will find, and the one that travels to
// another machine.
func pickLocation(env Env, ask *asker) (bool, error) {
	_, _ = fmt.Fprintln(env.Stdout, "\nWhere should the configuration file go?")
	_, _ = fmt.Fprintln(env.Stdout, "  1) in the repository, as .coding-owl.yaml, to be committed")
	_, _ = fmt.Fprintln(env.Stdout, "  2) in Owl's configuration home, local to this machine and not committed")
	choice, err := ask.choose(env, "location", []string{"in-repo", "config-home"})
	if err != nil {
		return false, err
	}
	return choice == "in-repo", nil
}

// asker reads the answers. One reader serves every question, because a
// buffered one holds what it has read ahead and a second would lose it.
type asker struct{ in *bufio.Reader }

func newAsker(env Env) *asker {
	return &asker{in: bufio.NewReader(io.LimitReader(env.stdin(), maxAnswer))}
}

// choose asks for one of choices by number and returns the one picked. An
// answer that is not one of them is said to be wrong and asked for again; an
// answer that never comes ends the command, because a command nobody is
// answering should stop rather than wait.
func (a *asker) choose(env Env, what string, choices []string) (string, error) {
	for {
		_, _ = fmt.Fprintf(env.Stdout, "Choose 1-%d: ", len(choices))
		line, err := a.in.ReadString('\n')
		// An answer typed without a newline, or piped in, ends at end of
		// file, which is an answer like any other. Nothing at all is not.
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reading which %s to use: %w", what, err)
		}
		answer := strings.TrimSpace(line)
		if answer == "" && errors.Is(err, io.EOF) {
			return "", fmt.Errorf("no answer for which %s to use, and nothing left to read", what)
		}
		n, convErr := strconv.Atoi(answer)
		if convErr == nil && n >= 1 && n <= len(choices) {
			return choices[n-1], nil
		}
		_, _ = fmt.Fprintf(env.Stdout, "%q is not one of the choices; pick a number between 1 and %d.\n",
			terminalSafe(answer), len(choices))
		if errors.Is(err, io.EOF) {
			// There was an answer; it was the wrong one. Saying none came
			// would send the reader looking for the wrong mistake.
			return "", fmt.Errorf("%q is not one of the choices for which %s to use, and there is nothing left to read",
				terminalSafe(answer), what)
		}
	}
}
