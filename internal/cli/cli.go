// Package cli is the owl command tree. It is thin: every command that talks
// to the daemon does so through the shared client package.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/daemon"
	"github.com/vojtechmares/coding-owl/internal/launchd"
	"github.com/vojtechmares/coding-owl/internal/version"
	"github.com/vojtechmares/coding-owl/internal/xdg"
)

// statusTimeout bounds owl daemon status so a wedged daemon cannot hang it.
const statusTimeout = 5 * time.Second

// callTimeout bounds every other command that talks to the daemon, for the
// same reason.
const callTimeout = 30 * time.Second

// Env is what the command tree needs from its process.
type Env struct {
	Paths  xdg.Paths
	Stdout io.Writer
	Stderr io.Writer
	// Stdin is where a command reads what only a person can give it, which is
	// the token an Account is authenticated with. Nil asks the process.
	Stdin io.Reader
	// WorkingDir is the directory owl was run in, which is what tells owl add
	// which Project it was called from. Empty asks the process.
	WorkingDir string
}

// workingDir is the directory to resolve a Project from. A directory that
// cannot be determined is not an error in itself: --project may name one
// anyway, and the daemon says so when nothing does.
func (e Env) workingDir() string {
	if e.WorkingDir != "" {
		return e.WorkingDir
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// stdin is where to read from. A command that needs one and has none would
// otherwise read from whatever the process was given, which is what a person
// at a terminal expects.
func (e Env) stdin() io.Reader {
	if e.Stdin != nil {
		return e.Stdin
	}
	return os.Stdin
}

// withDaemon runs fn against the daemon under the ordinary timeout, which a
// wedged daemon cannot outlast.
func withDaemon(cmd *cobra.Command, env Env, fn func(context.Context, *client.Client) error) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), callTimeout)
	defer cancel()
	return withTimeout(ctx, env, fn)
}

// withTimeout runs fn against the daemon under a deadline the caller chose,
// for the commands that wait on work rather than on an answer.
func withTimeout(ctx context.Context, env Env, fn func(context.Context, *client.Client) error) error {
	return fn(ctx, client.New(env.Paths.SocketPath))
}

// exitCodeError is a command that has already said whatever there was to say
// and carries only the status to exit with. It is how a command that runs
// something else on the user's behalf passes that program's status back out,
// rather than flattening every outcome to Owl's own 1.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exited with status %d", e.code) }

// Run executes args against the command tree and returns the exit code.
func Run(ctx context.Context, env Env, args []string) int {
	root := newRoot(env)
	root.SetArgs(args)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		// A command that ran another program on the user's behalf reports
		// that program's status and nothing else: the program has already
		// written whatever it had to say to the terminal, and `owl: exited
		// with status 1` underneath it would be Owl talking over it.
		var exit *exitCodeError
		if errors.As(err, &exit) {
			return exit.code
		}
		_, _ = fmt.Fprintf(env.Stderr, "owl: %v\n", err)
		return 1
	}
	return 0
}

func newRoot(env Env) *cobra.Command {
	root := &cobra.Command{
		Use:           "owl",
		Short:         "Coding Owl runs coding agents on your machine while it is idle",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newDaemonCmd(env),
		newProjectCmd(env),
		newAddCmd(env),
		newQueueCmd(env),
		newStartCmd(env),
		newLogsCmd(env),
		newJobsCmd(env),
		newStatusCmd(env),
		newAccountCmd(env),
		newProvidersCmd(env),
		newGCCmd(env),
		newSkillsCmd(env),
		newModelsCmd(env),
		newPauseCmd(env),
		newSetupCmd(env),
		newResumeCmd(env),
	)
	return root
}

func newDaemonCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run and inspect the daemon",
	}
	cmd.AddCommand(newDaemonRunCmd(env), newDaemonStatusCmd(env), newDaemonInstallCmd(env))
	return cmd
}

func newDaemonRunCmd(env Env) *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the daemon in the foreground",
		Long: `Run the daemon in the foreground, logging to standard error.

It never forks, never detaches and never writes a PID file; supervision
belongs to launchd via brew services (ADR-0002). Debugging is just running
it: --verbose turns the log up to debug, which shows what the daemon is
looking at and why it does nothing when it does nothing.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			// The first signal asks the daemon to stop; the second is the
			// user saying they meant it, and takes its default effect rather
			// than being swallowed while a Project's commands finish.
			go func() {
				<-ctx.Done()
				stop()
			}()
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			return daemon.Run(ctx, daemon.Options{
				Paths:   env.Paths,
				Version: version.Version,
				Logger:  slog.New(slog.NewTextHandler(env.Stderr, &slog.HandlerOptions{Level: level})),
			})
		},
	}
	cmd.Flags().BoolVar(&verbose, "verbose", false, "turn the log up to debug")
	return cmd
}

func newDaemonStatusCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the daemon is running and reachable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), statusTimeout)
			defer cancel()
			st, err := client.New(env.Paths.SocketPath).DaemonStatus(ctx)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(env.Stdout, "version: %s\n", st.Version)
			_, _ = fmt.Fprintf(env.Stdout, "uptime: %s\n", st.Uptime.Round(time.Second))
			_, _ = fmt.Fprintf(env.Stdout, "socket: %s\n", st.SocketPath)
			return nil
		},
	}
}

func newDaemonInstallCmd(env Env) *cobra.Command {
	var print bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the daemon as a launchd agent",
		Long: `Install the daemon as a launchd agent.

This is for machines that did not get Owl from Homebrew; with Homebrew,
"brew services start coding-owl" does the same job. The agent runs
"owl daemon run" in the foreground and launchd keeps it running - Owl never
supervises itself (ADR-0002).

The agent carries the PATH and the XDG variables in force when it is
installed, so the daemon looks at the same layout you do. Installing again
replaces it, which is how you point it at a new binary.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if print {
				agent, err := describeLaunchAgent(env)
				if err != nil {
					return err
				}
				body, err := launchd.Render(agent)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprint(env.Stdout, body)
				return nil
			}
			return installLaunchAgent(env)
		},
	}
	cmd.Flags().BoolVar(&print, "print", false, "print the launch agent instead of installing it")
	return cmd
}

// describeLaunchAgent is the agent this machine would install, and says why it
// cannot be described where it cannot.
func describeLaunchAgent(env Env) (launchd.Agent, error) {
	if runtime.GOOS != "darwin" {
		return launchd.Agent{}, fmt.Errorf("owl daemon install writes a launchd agent, which is macOS's; on %s run owl daemon run under your own service manager", runtime.GOOS)
	}
	program, err := os.Executable()
	if err != nil {
		return launchd.Agent{}, fmt.Errorf("finding the owl binary to run: %w", err)
	}
	// Deliberately not resolved through its symlinks: /opt/homebrew/bin/owl
	// survives an upgrade and the cellar path it points at does not.
	// Installing again is how the agent is pointed somewhere else.
	return launchd.Describe(program, env.Paths.StateDir, os.Getenv), nil
}

// installLaunchAgent writes the launch agent and loads it, and says what it
// did. It is a function of its own because `owl setup` offers the same thing
// to a machine whose daemon is not running (issue #125), and an offer that
// installed something else would be a second thing to keep right.
func installLaunchAgent(env Env) error {
	agent, err := describeLaunchAgent(env)
	if err != nil {
		return err
	}
	home := os.Getenv("HOME")
	if home == "" {
		return errors.New("HOME is not set, so there is nowhere to write the launch agent")
	}
	path, err := launchd.Install(agent, home, os.Getuid())
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(env.Stdout, "wrote the launch agent to %s\n", path)
	_, _ = fmt.Fprintf(env.Stdout, "launchd is running %s, and will start it again at login\n",
		strings.Join(agent.Arguments, " "))
	_, _ = fmt.Fprintln(env.Stdout, "check on it with: owl daemon status")
	return nil
}

// Main resolves the environment and runs args, returning the exit code. It is
// what cmd/owl calls.
func Main(args []string) int {
	paths, err := xdg.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "owl: %v\n", err)
		return 1
	}
	return Run(context.Background(), Env{
		Paths: paths, Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin,
	}, args)
}
