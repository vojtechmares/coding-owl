// Package cli is the owl command tree. It is thin: every command that talks
// to the daemon does so through the shared client package.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/vojtechmares/coding-owl/internal/client"
	"github.com/vojtechmares/coding-owl/internal/daemon"
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

// Run executes args against the command tree and returns the exit code.
func Run(ctx context.Context, env Env, args []string) int {
	root := newRoot(env)
	root.SetArgs(args)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
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
		newGCCmd(env),
	)
	return root
}

func newDaemonCmd(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run and inspect the daemon",
	}
	cmd.AddCommand(newDaemonRunCmd(env), newDaemonStatusCmd(env))
	return cmd
}

func newDaemonRunCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the daemon in the foreground",
		Long: `Run the daemon in the foreground, logging to standard error.

It never forks, never detaches and never writes a PID file; supervision
belongs to launchd via brew services (ADR-0002).`,
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
			return daemon.Run(ctx, daemon.Options{
				Paths:   env.Paths,
				Version: version.Version,
				Logger:  slog.New(slog.NewTextHandler(env.Stderr, nil)),
			})
		},
	}
}

func newDaemonStatusCmd(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether the daemon is running and reachable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := xdg.CheckSocketPath(env.Paths.SocketPath); err != nil {
				return err
			}
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
