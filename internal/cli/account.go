package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/drivers"
	"github.com/vojtechmares/coding-owl/internal/store"
)

// noAccounts is what owl account list prints when there are none.
const noAccounts = "no accounts yet; owl account add <name> makes one"

// maxToken bounds what is read as a token, so a mistaken `owl account add
// --token-stdin < some-huge-file` is refused rather than held in memory.
const maxToken = 64 << 10

func newAccountCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage the subscriptions Owl runs work on",
		Long: `Manage the subscriptions Owl runs work on.

An Account is a subscription with a tool configuration directory of its own,
so that a night of Owl's work never touches your own setup. Its token is kept
in the OS keychain; the database holds only a reference to it (ADR-0019).

A Project names the Account its Jobs run on in its configuration file:

    account: work`,
	}
	cmd.AddCommand(newAccountAddCmd(env), newAccountListCmd(env), newAccountRemoveCmd(env))
	return cmd
}

func newAccountAddCmd(env Env) *cobra.Command {
	var driverName string
	var failover, tokenStdin bool
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a subscription for Owl to run work on",
		Long: `Add a subscription for Owl to run work on.

This runs the tool's own token setup against a configuration directory Owl
makes for the Account, and takes the long-lived token it prints. The token
goes to the OS keychain and never to Owl's database.

With --token-stdin the setup is skipped and the token is read from standard
input instead, for a machine that has one already.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			// The name becomes a directory of its own, so it is checked here
			// as well as by the daemon: this command builds that directory's
			// path before the daemon has seen the name.
			if err := account.CheckName(name); err != nil {
				return err
			}
			if driverName == "" {
				driverName = drivers.Default
			}
			if !drivers.Known(driverName) {
				return drivers.Unknown(driverName)
			}
			// The tool's own setup is a browser round trip the user does by
			// hand, and it writes into the Account's directory. A daemon that
			// is not there, and a name that is already taken, are both worth
			// finding out about before that rather than after it: re-running
			// the setup for an Account that exists would authorise a
			// subscription into its directory and then be refused.
			var setup setupCommand
			if !tokenStdin {
				if err := nameIsFree(cmd, env, name); err != nil {
					return err
				}
				// The setup runs the tool, so where the tool is comes from
				// the daemon's own file, as it does for a Run. A token typed
				// in runs nothing, and reads nothing.
				global, _, err := config.LoadGlobal(filepath.Join(env.Paths.ConfigDir, config.GlobalFileName))
				if err != nil {
					return err
				}
				d, _ := drivers.Lookup(driverName, global)
				setup = d.SetupToken
			}
			token, err := accountToken(cmd, env, setup, env.Paths.DataDir, name, tokenStdin)
			if err != nil {
				return err
			}
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				a, err := c.AddAccount(ctx, client.AddAccountRequest{
					Name:            name,
					Driver:          driverName,
					Token:           token,
					FailoverAllowed: failover,
				})
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "account %s added, on %s\n", a.Name, a.Driver)
				_, _ = fmt.Fprintf(env.Stdout, "configuration directory: %s\n", a.ConfigDir)
				_, _ = fmt.Fprintf(env.Stdout, "run its jobs by putting `account: %s` in a project's configuration\n", a.Name)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&driverName, "driver", drivers.Default, "coding tool this Account belongs to")
	cmd.Flags().BoolVar(&failover, "failover", false, "record that work may fail over to another Account (recorded and unused)")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "read the token from standard input instead of running the tool's setup")
	return cmd
}

// nameIsFree reports whether the daemon is there and has no Account of that
// name. The daemon refuses a duplicate itself; this is what keeps the refusal
// from arriving after the user has authorised a subscription.
func nameIsFree(cmd *cobra.Command, env Env, name string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
	defer cancel()
	accounts, err := client.New(env.Paths.SocketPath).ListAccounts(ctx)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if strings.EqualFold(a.Name, name) {
			return fmt.Errorf("%w: account %s is already there; remove it first, or add one under another name",
				store.ErrAccountNameTaken, a.Name)
		}
	}
	return nil
}

// setupCommand builds the tool's own token setup for an Account's directory.
type setupCommand func(configDir string) (agent.Invocation, error)

// accountToken is the long-lived token to store for an Account: what is on
// standard input, or what the tool's own setup printed for the user to paste.
func accountToken(cmd *cobra.Command, env Env, setup setupCommand, dataDir, name string, fromStdin bool) (string, error) {
	configDir := account.DirFor(dataDir, name)
	if fromStdin {
		token, err := io.ReadAll(io.LimitReader(env.stdin(), maxToken))
		if err != nil {
			return "", fmt.Errorf("reading the token from standard input: %w", err)
		}
		return checkToken(string(token))
	}
	inv, err := setup(configDir)
	if err != nil {
		return "", err
	}
	// The tool writes its whole credential state into that directory
	// (ADR-0019), so it exists and is the user's alone before the tool runs,
	// rather than being made at the process umask by whatever gets there
	// first.
	if err := account.EnsureDir(dataDir, name); err != nil {
		return "", err
	}
	_, _ = fmt.Fprintf(env.Stdout,
		"Owl will now run %s %s, which authorises a subscription and prints a token.\n",
		inv.Path, strings.Join(inv.Args, " "))
	_, _ = fmt.Fprintf(env.Stdout, "It runs against %s, so your own setup is left alone.\n\n", configDir)
	// The tool talks to the person at the terminal, so it gets the terminal:
	// its output is theirs to read and its questions theirs to answer.
	run := exec.CommandContext(cmd.Context(), inv.Path, inv.Args...)
	run.Dir = inv.Dir
	// Less what the Driver keeps from every Agent: the user's own credentials
	// in this shell would otherwise stand in for the Account's (ADR-0019).
	run.Env = inv.Environ(os.Environ())
	run.Stdin, run.Stdout, run.Stderr = env.stdin(), env.Stdout, env.Stderr
	if err := run.Run(); err != nil {
		return "", fmt.Errorf("running %s %s: %w", inv.Path, strings.Join(inv.Args, " "), err)
	}
	_, _ = fmt.Fprint(env.Stdout, "\nPaste the token it printed: ")
	line, err := bufio.NewReader(io.LimitReader(env.stdin(), maxToken)).ReadString('\n')
	// A token typed without a newline, or piped in, ends at end of file, which
	// is not a failure.
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading the token: %w", err)
	}
	return checkToken(line)
}

// checkToken refuses what is plainly not a token, so that an Account is never
// recorded with a credential nothing can run on.
func checkToken(s string) (string, error) {
	token := strings.TrimSpace(s)
	if token == "" {
		return "", errors.New("no token was given; run the tool's token setup and paste what it prints")
	}
	return token, nil
}

func newAccountListCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the Accounts Owl can run work on",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				accounts, err := c.ListAccounts(ctx)
				if err != nil {
					return err
				}
				if len(accounts) == 0 {
					_, _ = fmt.Fprintln(env.Stdout, noAccounts)
					return nil
				}
				w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
				_, _ = fmt.Fprintln(w, "NAME\tDRIVER\tCREDENTIAL\tFAILOVER\tDIRECTORY")
				for _, a := range accounts {
					_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						a.Name, a.Driver, credentialCell(a.HasCredential), yesNo(a.FailoverAllowed), a.ConfigDir)
				}
				return w.Flush()
			})
		},
	}
}

// credentialCell says whether the credential store still holds an Account's
// secret. An Account that has lost it cannot run anything, so the listing says
// so rather than showing a blank.
func credentialCell(has bool) string {
	if has {
		return "yes"
	}
	return "missing"
}

func newAccountRemoveCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an Account and its credential",
		Long: `Remove an Account and its credential.

The Account's token is taken out of the keychain with it. Its configuration
directory is left where it is: removing an Account is not a reason to destroy
a tool's state, and adding the Account again picks it up.

An Account any Job ran on is refused: what a Job drew on stays knowable for
its whole life (ADR-0023).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
				a, err := c.RemoveAccount(ctx, args[0])
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(env.Stdout, "account %s removed, with its credential\n", a.Name)
				_, _ = fmt.Fprintf(env.Stdout, "its configuration directory is still at %s\n", a.ConfigDir)
				return nil
			})
		},
	}
}
