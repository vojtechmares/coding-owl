package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/agent"
	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/drivers"
)

func newAccountExecCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <name> -- <command>...",
		Short: "Run an Account's coding tool against that Account",
		Long: `Run an Account's coding tool against that Account's own configuration.

An Account's configuration directory is the tool's configuration directory, so
the tool's own commands are how an Account is configured - its MCP servers,
its plugins, its settings. Owl models none of those: it points the tool at the
right Account and gets out of the way.

    owl account exec work -- mcp add --scope user sentry --transport http https://mcp.sentry.dev/mcp
    owl account exec work -- plugin install some-plugin
    owl account exec work -- mcp list

Everything after -- is the tool's, passed through untouched, and the tool's
exit status is this command's.

Two things worth knowing about MCP servers an Agent is meant to use. An
unattended Agent denies anything that is not on its allowlist, so a server's
tools have to be granted as mcp__<server>__* in the Account's settings.json or
in the Project's allowedTools. And a repository's own .mcp.json servers wait
for an approval nobody is awake to give, so user scope - what --scope user
writes, here in the Account's directory - is the one that works for a Run.`,
		// The Account, and then at least one word of the tool's own command:
		// `owl account exec work` on its own names no command to run.
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, toolArgs := args[0], toolCommand(args[1:])
			// Checked here as well as by the daemon, so a name Owl could
			// never have made an Account for is refused by the same words
			// wherever it is typed.
			if err := account.CheckName(name); err != nil {
				return err
			}
			// `owl account exec work --` satisfies the argument count and
			// then names nothing to run. Refused before the Account is asked
			// for, so that a command which was never going to run does not
			// fetch a credential on its way to saying so.
			if len(toolArgs) == 0 {
				return errors.New("name a command to run, as in: owl account exec " + name + " -- mcp list")
			}
			inv, err := accountInvocation(cmd, env, name, toolArgs)
			if err != nil {
				return err
			}
			return runInvocation(cmd.Context(), env, inv)
		},
	}
	// The tool's own flags are not Owl's to read: parsing stops at the
	// Account's name, so `-s user` reaches the tool rather than being
	// refused here as an unknown flag.
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// toolCommand is the command the user meant, with the option terminator taken
// off the front.
//
// Because this command stops parsing options at the Account's name - so that
// the tool's own flags are not read as Owl's - the `--` a user writes out of
// habit is left in the arguments rather than consumed as a terminator. Passing
// it on would have the tool read its own first argument as the end of its
// options: `-- --version` is not `--version`. Exactly one is dropped, so
// somebody who genuinely means to pass a `--` through writes two.
func toolCommand(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

// accountInvocation builds the tool's command for an Account: which Driver
// that Account is on, the directory it is configured in, and the credential it
// draws on, all of which the daemon holds. It is deliberately a separate step
// from running it - the daemon call is bounded by a timeout that an
// interactive tool session would outlast.
func accountInvocation(cmd *cobra.Command, env Env, name string, args []string) (agent.Invocation, error) {
	var (
		acct  client.Account
		token string
	)
	if err := withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
		var err error
		acct, token, err = c.AccountCredential(ctx, name)
		return err
	}); err != nil {
		return agent.Invocation{}, err
	}
	// Where the tool is, is the daemon's own file to say, as it is for a Run:
	// a daemon kept running by brew services has a PATH of its own.
	global, _, err := config.LoadGlobal(filepath.Join(env.Paths.ConfigDir, config.GlobalFileName))
	if err != nil {
		return agent.Invocation{}, err
	}
	// The Driver the Account is recorded on, not the default: an Account on
	// another tool must not be handed this one's.
	d, ok := drivers.Lookup(acct.Driver, global)
	if !ok {
		return agent.Invocation{}, drivers.Unknown(acct.Driver)
	}
	return d.Exec(acct.ConfigDir, token, args)
}

// runInvocation runs the tool at the user's own terminal and gives back its
// exit status as Owl's own.
//
// The child inherits this process's own standard input, output and error, so
// a tool that draws a prompt or asks a question gets the real terminal to do
// it on. Nothing is put between them: no process group of its own, as an
// Agent gets (ADR-0011), because here the user is at the keyboard and their
// interrupt should reach the tool the way it would if they had run it.
func runInvocation(ctx context.Context, env Env, inv agent.Invocation) error {
	defer holdInterrupts()()

	run := exec.CommandContext(ctx, inv.Path, inv.Args...)
	run.Dir = inv.Dir
	// Built the one way every invocation's environment is built, so that what
	// is kept from an Agent is kept here too: the daemon's own credentials
	// never stand in for the Account's (ADR-0019).
	run.Env = inv.Environ(os.Environ())
	run.Stdin, run.Stdout, run.Stderr = env.stdin(), env.Stdout, env.Stderr
	err := run.Run()
	if err == nil {
		return nil
	}
	// The tool ran and said what it had to say; its status is the answer and
	// Owl has nothing to add underneath it.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &exitCodeError{code: statusOf(exitErr)}
	}
	return fmt.Errorf("running %s %s: %w", inv.Path, strings.Join(inv.Args, " "), err)
}

// statusOf is the status to exit with for a tool that has ended.
//
// A tool killed by a signal never exited with a status at all, and os/exec
// reports -1 for it, which would leave the shell reading 255 with nothing said
// about why. A shell says 128 plus the signal for this, and so does Owl, so
// that a tool that segfaulted is told apart from one that chose to fail.
func statusOf(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return err.ExitCode()
}

// holdInterrupts keeps the interrupt keys from ending Owl while a program it
// started has the terminal, and returns the func that gives them back.
//
// The terminal sends them to the whole foreground group, so the program has
// already had them: what this prevents is Owl acting on them too, ending this
// process first and handing the shell its prompt back while the program is
// still drawing on the same screen. Owl waits instead, and reports how the
// program ended. It is what git and less do, for the same reason.
//
// A handler is installed rather than the disposition set to ignore, which
// looks like the same thing here and is not: an ignored signal survives the
// exec into the child, and a program written to the old convention - take the
// signal only if it is not already ignored - would then be one the user
// cannot interrupt at all. A Go handler is reset to the default across exec,
// so the child starts as it would have from a shell.
func holdInterrupts() func() {
	held := make(chan os.Signal, 1)
	// SIGQUIT as well as SIGINT: Go's default for it is to dump every
	// goroutine's stack, which would land in the middle of whatever the tool
	// is showing.
	signal.Notify(held, os.Interrupt, syscall.SIGQUIT)
	return func() { signal.Stop(held) }
}
