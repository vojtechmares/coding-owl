// Package daemon runs the Coding Owl daemon: a ConnectRPC server on a unix
// socket, in the foreground, supervised by whoever started it (ADR-0002).
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/xdg"
)

// ErrAlreadyListening is returned by Run when another daemon answers on the
// socket path.
var ErrAlreadyListening = errors.New("a daemon is already listening")

// Options configures a daemon.
type Options struct {
	// Paths is the resolved filesystem layout, including the socket path.
	Paths xdg.Paths
	// Version is reported by GetStatus.
	Version string
	// Logger receives startup and shutdown lines. Nil discards them.
	Logger *slog.Logger
}

// Run listens on the socket and serves until ctx is cancelled, then shuts
// down and removes the socket. It never forks and never writes a PID file.
func Run(ctx context.Context, opts Options) error {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	sock := opts.Paths.SocketPath
	if err := xdg.CheckSocketPath(sock); err != nil {
		return err
	}
	for _, dir := range []string{opts.Paths.ConfigDir, opts.Paths.DataDir, opts.Paths.StateDir, filepath.Dir(sock)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	if err := removeStaleSocket(sock); err != nil {
		return err
	}

	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", sock, err)
	}
	started := time.Now()

	mux := http.NewServeMux()
	mux.Handle(codingowlv1connect.NewDaemonServiceHandler(&daemonService{
		version: opts.Version,
		socket:  sock,
		started: started,
	}))
	srv := &http.Server{Handler: mux}

	log.Info("daemon listening",
		"socket", sock,
		"pid", os.Getpid(),
		"version", opts.Version,
		"config", opts.Paths.ConfigDir,
		"data", opts.Paths.DataDir,
		"state", opts.Paths.StateDir,
	)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		os.Remove(sock)
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}

	log.Info("daemon stopping")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	<-serveErr
	os.Remove(sock)
	if err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return nil
}

// removeStaleSocket unlinks a socket file nothing is listening on, and
// returns ErrAlreadyListening when something is.
func removeStaleSocket(sock string) error {
	if _, err := os.Lstat(sock); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", sock, time.Second)
	if err == nil {
		conn.Close()
		return fmt.Errorf("%w on %s", ErrAlreadyListening, sock)
	}
	if err := os.Remove(sock); err != nil {
		return fmt.Errorf("removing stale socket %s: %w", sock, err)
	}
	return nil
}

type daemonService struct {
	codingowlv1connect.UnimplementedDaemonServiceHandler
	version string
	socket  string
	started time.Time
}

func (s *daemonService) GetStatus(context.Context, *connect.Request[codingowlv1.GetStatusRequest]) (*connect.Response[codingowlv1.GetStatusResponse], error) {
	return connect.NewResponse(&codingowlv1.GetStatusResponse{
		Version:    s.version,
		Uptime:     durationpb.New(time.Since(s.started)),
		SocketPath: s.socket,
	}), nil
}
