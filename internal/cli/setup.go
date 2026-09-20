package cli

// owl setup: the walkthrough for a machine that has just installed Owl
// (issue #125).
//
// Nothing runs until three things are true - a daemon is up, Claude Code is
// somewhere the daemon will find it, and an Account is registered - and a
// first-time user has no way to know which of the three is missing, or that
// there were three. This walks them in that order, reports what is already
// true rather than redoing it, asks only about what is missing, and ends by
// naming what comes next.
//
// Two things it deliberately does not do. It never starts a daemon behind the
// user's back: on a Homebrew install it names `brew services start
// coding-owl`, and elsewhere on macOS it offers to write the launch agent
// `owl daemon install` writes - an offer, answered, never assumed. And it
// never guesses where `claude` is: it looks where a Run will look, and asks
// when that finds nothing.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
	"github.com/vojtechmares/coding-owl/internal/drivers"
	"github.com/vojtechmares/coding-owl/internal/yamledit"
)

func newSetupCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Walk through what Owl needs before it can run anything",
		Long: `Walk through what Owl needs before it can run anything.

Owl runs nothing until a daemon is up, Claude Code is somewhere the daemon
will find it, and an Account is registered. This checks the three in that
order, says which are already done, and asks only about the rest.

It never starts a daemon by itself: with Homebrew it names the brew services
command, and elsewhere on macOS it offers to write the launch agent that
"owl daemon install" writes. The answers are read from standard input, so it
works piped as well as typed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setup(cmd, env)
		},
	}
}

// setup walks the three things, and returns an error only when one of them is
// still not true at the end: an exit status is what a script reads.
func setup(cmd *cobra.Command, env Env) error {
	ask := newAsker(env)
	_, _ = fmt.Fprintln(env.Stdout, "Owl needs three things before it can run anything.")
	_, _ = fmt.Fprintln(env.Stdout)

	running, err := setupDaemon(cmd, env, ask)
	if err != nil {
		return err
	}
	if err := setupClaude(env, ask); err != nil {
		return err
	}
	if running {
		if err := setupAccount(cmd, env, ask); err != nil {
			return err
		}
	} else {
		_, _ = fmt.Fprintln(env.Stdout, "account: not checked, because that needs the daemon")
	}

	if !running {
		_, _ = fmt.Fprintln(env.Stdout)
		return errors.New("the daemon is not running, so this could not finish; run `owl setup` again once it is up")
	}
	_, _ = fmt.Fprintln(env.Stdout)
	_, _ = fmt.Fprintln(env.Stdout, "That is everything Owl needs.")
	_, _ = fmt.Fprintln(env.Stdout, "Next: register a repository to work in with `owl project add <path>`,")
	_, _ = fmt.Fprintln(env.Stdout, "which says how to give it the account its jobs run on.")
	return nil
}

// setupDaemon reports whether the daemon is reachable, and says what would
// start one when it is not. It starts nothing itself: what supervises the
// daemon is launchd, through Homebrew or through a launch agent, and never Owl
// (ADR-0002).
func setupDaemon(cmd *cobra.Command, env Env, ask *asker) (bool, error) {
	ctx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
	defer cancel()
	st, err := client.New(env.Paths.SocketPath).DaemonStatus(ctx)
	if err == nil {
		_, _ = fmt.Fprintf(env.Stdout, "daemon: running, version %s, on %s\n", st.Version, terminalSafe(st.SocketPath))
		return true, nil
	}
	if !errors.Is(err, client.ErrDaemonNotRunning) {
		return false, err
	}
	_, _ = fmt.Fprintf(env.Stdout, "daemon: not running; nothing is listening on %s\n", terminalSafe(env.Paths.SocketPath))
	switch {
	case fromHomebrew():
		_, _ = fmt.Fprintln(env.Stdout, "  this owl came from Homebrew, so brew services is what keeps it running:")
		_, _ = fmt.Fprintln(env.Stdout, "    brew services start coding-owl")
	case runtime.GOOS == "darwin":
		_, _ = fmt.Fprintln(env.Stdout, "  a launch agent would keep it running and start it again at login,")
		_, _ = fmt.Fprintln(env.Stdout, "  which is what `owl daemon install` writes.")
		yes, err := ask.confirm(env, "  Write it now [y/N]: ")
		if err != nil {
			return false, err
		}
		if yes {
			if err := installLaunchAgent(env); err != nil {
				return false, err
			}
			// The agent is loaded, but the daemon it starts is not up yet, and
			// waiting on it here would be Owl supervising itself. The rest of
			// the walkthrough is what needs no daemon.
			_, _ = fmt.Fprintln(env.Stdout, "  it takes a moment to come up")
			return false, nil
		}
		_, _ = fmt.Fprintln(env.Stdout, "  not written; `owl daemon install` writes it whenever you want it")
	default:
		_, _ = fmt.Fprintf(env.Stdout,
			"  on %s, run `owl daemon run` under your own service manager\n", runtime.GOOS)
	}
	return false, nil
}

// setupClaude reports where a Run will find Claude Code, and asks for it when
// nothing will. The lookup is the Driver's own, so what this reports is what a
// Run gets rather than a second search that could drift from it.
func setupClaude(env Env, ask *asker) error {
	globalPath := filepath.Join(env.Paths.ConfigDir, config.GlobalFileName)
	global, _, err := config.LoadGlobal(globalPath)
	if err != nil {
		return err
	}
	if path, err := claudecode.Find(global.ClaudePath); err == nil {
		_, _ = fmt.Fprintf(env.Stdout, "claude: %s\n", terminalSafe(path))
		return nil
	}
	_, _ = fmt.Fprintln(env.Stdout, "claude: not on PATH, and not under ~/.local/bin")
	_, _ = fmt.Fprintln(env.Stdout, "  install it from https://claude.com/claude-code, or name where it is now.")
	path, err := askExecutable(env, ask, "  Path to claude (empty to skip): ")
	if err != nil {
		return err
	}
	if path == "" {
		_, _ = fmt.Fprintln(env.Stdout, "  skipped; a run cannot start until the daemon can find it")
		return nil
	}
	// Written into the daemon's own file, because that is where claudePath is
	// read from (ADR-0014), and with the one key changed: the file is the
	// user's and may carry anything else already.
	file := yamledit.File{
		Path:  globalPath,
		Start: "apiVersion: " + config.APIVersion + "\n",
		// The daemon's configuration home is the user's alone: it is where a
		// credential store and an API key can be named.
		DirMode:  0o700,
		FileMode: 0o600,
	}
	if err := file.SetScalar("claudePath", path); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(env.Stdout, "  wrote claudePath: %s to %s\n", terminalSafe(path), terminalSafe(globalPath))
	_, _ = fmt.Fprintln(env.Stdout, "  the daemon reads that when it starts, so restart it before the setting counts")
	return nil
}

// askExecutable asks for the path to a program until it is given one that can
// be run, or until the answer is empty. A path that is not a program is said
// to be wrong and asked for again: the alternative is writing it down and
// failing at the start of the first Run, a long way from the mistake.
func askExecutable(env Env, ask *asker, prompt string) (string, error) {
	for {
		answer, last, err := ask.ask(env, prompt, "where claude is")
		if err != nil {
			return "", err
		}
		if answer == "" {
			return "", nil
		}
		path, err := usableProgram(answer)
		if err == nil {
			return path, nil
		}
		_, _ = fmt.Fprintf(env.Stdout, "  %v\n", err)
		if last {
			return "", fmt.Errorf("%w, and there is nothing left to read", err)
		}
	}
}

// usableProgram is the absolute path of a program that can be run, and says
// what is wrong with anything else. The path is resolved against the directory
// owl was run in, because a relative one would mean something different
// wherever the daemon was started from - which is what the setting exists to
// escape.
func usableProgram(answer string) (string, error) {
	path, err := userPath(answer)
	if err != nil {
		return "", fmt.Errorf("%s is not a path Owl can use: %w", terminalSafe(answer), err)
	}
	if err := config.CheckText("claudePath", path); err != nil {
		return "", err
	}
	// The same check the Driver makes of a configured path, so that what is
	// written here is what a Run will accept.
	if _, err := claudecode.Find(path); err != nil {
		return "", fmt.Errorf("%s is not a program Owl can run: %w", terminalSafe(path), err)
	}
	return path, nil
}

// setupAccount reports the Accounts there are, and registers one when there
// are none. It registers it exactly as `owl account add` does, because it is
// that command's own code.
func setupAccount(cmd *cobra.Command, env Env, ask *asker) error {
	var accounts []client.Account
	if err := withDaemon(cmd, env, func(ctx context.Context, c *client.Client) error {
		var err error
		accounts, err = c.ListAccounts(ctx)
		return err
	}); err != nil {
		return err
	}
	if len(accounts) > 0 {
		names := make([]string, 0, len(accounts))
		for _, a := range accounts {
			names = append(names, terminalSafe(a.Name))
		}
		_, _ = fmt.Fprintf(env.Stdout, "account: %s\n", strings.Join(names, ", "))
		return nil
	}
	_, _ = fmt.Fprintln(env.Stdout, "account: none registered")
	_, _ = fmt.Fprintln(env.Stdout, "  an account is a subscription with a tool configuration directory of its own,")
	_, _ = fmt.Fprintln(env.Stdout, "  so a night of Owl's work never touches your own setup. Name one to register:")
	name, err := askAccountName(env, ask)
	if err != nil {
		return err
	}
	// Nothing is waiting on the daemon while the tool's own token setup runs,
	// which is a browser round trip the user does by hand.
	return accountAdd(cmd, env, ask, name, drivers.Default, false, false)
}

// askAccountName asks what to call the first Account until it is given a name
// that can be one. The name becomes a directory, so a name that cannot be one
// is refused here rather than by the daemon after a subscription has been
// authorised into it.
func askAccountName(env Env, ask *asker) (string, error) {
	for {
		answer, last, err := ask.ask(env, "  Name for this account, such as work: ", "what to call the account")
		if err != nil {
			return "", err
		}
		bad := account.CheckName(answer)
		if bad == nil {
			return answer, nil
		}
		_, _ = fmt.Fprintf(env.Stdout, "  %v\n", bad)
		if last {
			return "", fmt.Errorf("%w, and there is nothing left to read", bad)
		}
	}
}

// fromHomebrew reports whether this owl was installed by Homebrew, which is
// what makes `brew services` the thing that keeps its daemon running rather
// than a launch agent of Owl's own. Homebrew installs into a Cellar and puts a
// symlink to it on PATH, so the binary's real path is what says so.
func fromHomebrew() bool {
	program, err := os.Executable()
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(program)
	if err != nil {
		return false
	}
	for _, element := range strings.Split(real, string(os.PathSeparator)) {
		if element == "Cellar" {
			return true
		}
	}
	return false
}
