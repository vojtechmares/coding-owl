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
	"syscall"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/durationpb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/account"
	"github.com/vojtechmares/coding-owl/internal/chat"
	"github.com/vojtechmares/coding-owl/internal/config"
	"github.com/vojtechmares/coding-owl/internal/credential"
	"github.com/vojtechmares/coding-owl/internal/driver/claudecode"
	"github.com/vojtechmares/coding-owl/internal/executor/host"
	"github.com/vojtechmares/coding-owl/internal/gc"
	"github.com/vojtechmares/coding-owl/internal/git"
	"github.com/vojtechmares/coding-owl/internal/idle"
	"github.com/vojtechmares/coding-owl/internal/idle/system"
	"github.com/vojtechmares/coding-owl/internal/project"
	"github.com/vojtechmares/coding-owl/internal/queue"
	"github.com/vojtechmares/coding-owl/internal/run"
	"github.com/vojtechmares/coding-owl/internal/skill"
	"github.com/vojtechmares/coding-owl/internal/store"
	agentverifier "github.com/vojtechmares/coding-owl/internal/verifier/agent"
	"github.com/vojtechmares/coding-owl/internal/verifier/command"
	"github.com/vojtechmares/coding-owl/internal/xdg"
)

// databaseName is the SQLite file under the data directory, worktreesDir
// holds one worktree per Job, and logsDir one captured stream per Run
// (ADR-0014).
const (
	databaseName = "owl.db"
	// credentialsName is the file a credential store keeps secrets in, where
	// the platform has no keychain to keep them in instead (ADR-0019).
	credentialsName = "credentials.json"
	worktreesDir    = "worktrees"
	// skillsDir holds the fetched Skills, each under its content digest, and
	// ownedDir the exclude file Owl owns for each Job's worktree (ADR-0033).
	skillsDir = "skills"
	ownedDir  = "worktree-config"
	logsDir   = "logs"
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
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	// The socket's directory is tightened on every start, whether it was just
	// made or found in place: a runtime directory somebody else made too open
	// would otherwise let anybody reach the socket's name.
	if err := os.Chmod(filepath.Dir(sock), 0o700); err != nil {
		return fmt.Errorf("restricting %s: %w", filepath.Dir(sock), err)
	}
	if err := removeStaleSocket(sock); err != nil {
		return err
	}

	dbPath := filepath.Join(opts.Paths.DataDir, databaseName)
	db, migration, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	log.Info("database ready", "path", dbPath, "schema", migration.Schema, "applied", migration.Applied)

	// Owl works with git from a certain version on, and a git that is too old
	// would only be found out at the second Run of a Job. It is asked once,
	// here, and a daemon that cannot rebase says so rather than starting.
	if err := git.CheckVersion(); err != nil {
		return err
	}

	// Everything that can fail without a listener - the daemon's own
	// configuration, the credential store, the recovery of an earlier
	// daemon's Runs - is settled before the socket exists, so that a daemon
	// which cannot start never owns one and leaves nothing behind.
	//
	// Where an Account's secret is kept is settled once, at startup: a Run
	// that cannot read a credential is not the moment to discover that the
	// daemon's configuration changed under it (ADR-0019).
	configPath := filepath.Join(opts.Paths.ConfigDir, config.GlobalFileName)
	global, _, err := config.LoadGlobal(configPath)
	if err != nil {
		return err
	}
	kind, err := credential.ParseKind(global.CredentialStore)
	if err != nil {
		return err
	}
	creds, err := credential.Open(kind, filepath.Join(opts.Paths.DataDir, credentialsName))
	if err != nil {
		return err
	}
	log.Info("credentials", "store", creds.Name())

	projects := project.NewService(db, opts.Paths.ConfigDir)
	accounts := account.NewService(db, creds, opts.Paths.DataDir)
	skills := skill.NewService(skill.NewCache(filepath.Join(opts.Paths.DataDir, skillsDir)))
	worktrees := filepath.Join(opts.Paths.DataDir, worktreesDir)
	collector := gc.NewService(gc.Options{
		Store:             db,
		WorktreeDir:       worktrees,
		WorktreeConfigDir: filepath.Join(opts.Paths.DataDir, ownedDir),
		ReviewAfter:       global.GarbageCollection.ReviewAfter,
		Logger:            log,
	})
	runs := run.NewService(run.Options{
		Store:             db,
		Projects:          projects,
		Accounts:          accounts,
		Collector:         collector,
		Skills:            skills,
		WorktreeConfigDir: filepath.Join(opts.Paths.DataDir, ownedDir),
		// Where the tool is, when the daemon's PATH does not say: a daemon
		// under brew services gets a fixed PATH without ~/.local/bin.
		Driver:        claudecode.NewWithPath(global.ClaudePath),
		Executor:      host.New(),
		Verifier:      command.New(),
		AgentVerifier: agentverifier.New(claudecode.NewWithPath(global.ClaudePath), host.New()),
		WorktreeDir:   worktrees,
		LogDir:        filepath.Join(opts.Paths.StateDir, logsDir),
		ConfigPath:    configPath,
		Logger:        log,
	})
	// Agents outlive the request that started them, so they are stopped when
	// the daemon stops rather than when a caller hangs up.
	defer func() { _ = runs.Close() }()
	if err := runs.Recover(ctx); err != nil {
		return fmt.Errorf("closing the runs of an earlier daemon: %w", err)
	}

	ln, err := listenPrivate(sock)
	if err != nil {
		return err
	}
	// Until serving takes the listener over, a failure on the way out closes
	// it and removes the socket, so that nothing is left for the next start
	// to find stale.
	listening := ln
	defer func() {
		if listening != nil {
			_ = listening.Close()
			_ = os.Remove(sock)
		}
	}()
	// Only the owning user may connect; there is no authentication on the
	// handler (ADR-0004 defers auth to a later TCP transport). The socket was
	// made under a umask that allows nobody else, so this is what it already
	// is; it is said once more in case the platform ignored the umask.
	if err := os.Chmod(sock, 0o600); err != nil {
		return fmt.Errorf("restricting %s: %w", sock, err)
	}
	started := time.Now()

	mux := http.NewServeMux()
	mux.Handle(codingowlv1connect.NewDaemonServiceHandler(&daemonService{
		version: opts.Version,
		socket:  sock,
		started: started,
	}))
	mux.Handle(codingowlv1connect.NewProjectServiceHandler(&projectService{
		projects: projects,
	}))
	mux.Handle(codingowlv1connect.NewAccountServiceHandler(&accountService{
		accounts: accounts,
	}))
	mux.Handle(codingowlv1connect.NewGarbageCollectionServiceHandler(&gcService{
		gc: collector,
	}))
	mux.Handle(codingowlv1connect.NewSkillServiceHandler(&skillService{
		skills: project.NewSkillService(projects, skills),
	}))
	chats := chat.NewService(db, creds, chat.NewTools(&chatView{
		projects: projects, jobs: queue.NewService(db, queue.Local{}), runs: runs,
	}))
	mux.Handle(codingowlv1connect.NewChatServiceHandler(&chatService{chat: chats, daemon: ctx}))
	mux.Handle(codingowlv1connect.NewJobServiceHandler(&jobService{
		jobs: queue.NewService(db, queue.Local{}),
		runs: runs,
	}))
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// Every request lives within the daemon's own lifetime, so that
		// stopping the daemon ends what is parked - a chat exchange waiting
		// minutes for consent, or for a provider - rather than waiting on it
		// past the shutdown deadline and reporting a failure that was nothing
		// of the kind. What a request must finish writing after it is ended,
		// it does under a context of its own.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	log.Info("daemon listening",
		"socket", sock,
		"pid", os.Getpid(),
		"version", opts.Version,
		"config", opts.Paths.ConfigDir,
		"data", opts.Paths.DataDir,
		"state", opts.Paths.StateDir,
	)

	// Garbage collection asks the daemon what it is carrying, so that an
	// active Job between its Run ending and the Job being moved on is not
	// mistaken for one a dead daemon left behind; and it takes the same lock
	// that serialises deciding a Job's fate (ADR-0015).
	collector.Decides(runs.Carrying, runs.DisposalLock())

	// Garbage collection runs on start and on an interval, so that disk stays
	// bounded and nothing quietly rots without anybody asking (ADR-0015). It
	// is stopped on the way out of this function rather than only when the
	// context is done, so that a daemon returning for any other reason - a
	// server that stopped serving - does not wait on it forever.
	collectCtx, stopCollecting := context.WithCancel(ctx)
	collecting := collect(collectCtx, collector, global.GarbageCollection.Interval, log)
	defer func() {
		stopCollecting()
		<-collecting
	}()

	// Owl works on the machine nobody is using: the daemon keeps the machine
	// in view, starts work when it is Idle, and gives it back the moment
	// somebody returns (ADR-0011). It is stopped on the way out for the same
	// reason garbage collection is.
	detector := system.New()
	log.Info("watching the machine", "detector", detector.Name(), "every", idleInterval(global.Idle))
	watchCtx, stopWatching := context.WithCancel(ctx)
	watching := make(chan struct{})
	go func() {
		defer close(watching)
		runs.Watch(watchCtx, detector, idleInterval(global.Idle))
	}()
	defer func() {
		stopWatching()
		<-watching
	}()

	serveErr := make(chan error, 1)
	listening = nil // the server owns the listener from here on
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		_ = os.Remove(sock)
		return fmt.Errorf("serving: %w", err)
	case <-ctx.Done():
	}

	log.Info("daemon stopping")
	// Watching stops before the Runs do, so that nothing starts or is frozen
	// while they are being ended, and the ordinary refusals of a daemon on its
	// way out are never reported as anything happening.
	stopWatching()
	<-watching
	// Runs are stopped first of the rest: an owl logs -f is an ordinary
	// request that only ends when the Run it follows does, so shutting the
	// server down first would wait for a Run rather than for a request.
	if err := runs.Close(); err != nil {
		log.Error("stopping the runs in progress", "error", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	<-serveErr
	_ = os.Remove(sock)
	if err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return nil
}

// collect runs garbage collection now and then every interval until ctx is
// done, and returns a channel that closes once it has stopped. A collection
// that fails is logged and the next one still happens: the task exists to keep
// the disk bounded, and one bad night must not end it.
func collect(ctx context.Context, collector *gc.Service, interval time.Duration, log *slog.Logger) <-chan struct{} {
	if interval <= 0 {
		interval = gc.DefaultInterval
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			report, err := collector.Collect(ctx)
			switch {
			case errors.Is(err, context.Canceled):
				// The daemon is stopping, which is not a failed collection.
			case err != nil:
				log.Error("garbage collection", "error", err)
			default:
				log.Info("garbage collection",
					"reclaimed", len(report.Reclaimed), "accepted", len(report.Accepted),
					"pruned", len(report.Pruned), "unfinished", len(report.Unfinished))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

// listenPrivate listens on a unix socket that is never more permissive than
// 0600: the process umask is set to allow nobody but the owner for as long as
// it takes to create the socket, and restored once it exists. The umask is
// process-wide, which is why the window is kept to the one call.
func listenPrivate(sock string) (net.Listener, error) {
	old := syscall.Umask(0o077)
	ln, err := net.Listen("unix", sock)
	syscall.Umask(old)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", sock, err)
	}
	return ln, nil
}

// removeStaleSocket unlinks a socket that nothing is listening on, which is
// what a crashed daemon leaves behind. It returns ErrAlreadyListening when a
// daemon answers, and refuses to touch anything that is not a socket or that
// is not clearly dead.
func removeStaleSocket(sock string) error {
	st, err := os.Lstat(sock)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&fs.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket; refusing to remove it", sock)
	}
	conn, err := net.DialTimeout("unix", sock, time.Second)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("%w on %s", ErrAlreadyListening, sock)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("%s did not answer but may be live (%v); refusing to remove it", sock, err)
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

// idleInterval is how often the machine is looked at. Unlike what counts as
// Idle, which is read on every look, this is the daemon's for its lifetime.
func idleInterval(cfg config.Idle) time.Duration {
	if cfg.Interval > 0 {
		return cfg.Interval
	}
	return idle.DefaultInterval
}
