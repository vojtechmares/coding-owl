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

// Env is what the command tree needs from its process.
type Env struct {
	Paths  xdg.Paths
	Stdout io.Writer
	Stderr io.Writer
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
	root.AddCommand(newDaemonCmd(env))
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
			st, err := client.New(env.Paths.SocketPath).DaemonStatus(cmd.Context())
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
	return Run(context.Background(), Env{Paths: paths, Stdout: os.Stdout, Stderr: os.Stderr}, args)
}
