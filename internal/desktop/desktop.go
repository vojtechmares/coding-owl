// Package desktop is the Go side of the desktop app: the struct the app binds
// to its frontend. It is a pure view over the daemon (ADR-0009): every method
// is a call into the shared client, and the app holds no state the daemon
// does not. cmd/owl-desktop wires it to Wails; the tests drive it directly.
package desktop

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// Emitter is how the app tells its frontend something happened without being
// asked. Wails' event runtime is the one cmd/owl-desktop supplies.
type Emitter interface {
	Emit(name string, data any)
}

// Events the app emits, and what each carries.
const (
	// EventLogLine carries a LogLine, one line of a followed Run's output.
	EventLogLine = "run:log"
	// EventLogEnd carries a LogEnd, when a followed Run's log stops.
	EventLogEnd = "run:log:end"
)

// LogLine is one line of a Run's structured output.
type LogLine struct {
	RunID int64  `json:"runId"`
	Line  string `json:"line"`
}

// LogEnd says a Run's log stopped: because the Run ended, because it was
// stopped from the frontend, or because of an error, which is then named.
type LogEnd struct {
	RunID int64  `json:"runId"`
	Error string `json:"error"`
}

// Status is what the app knows about the daemon. Error is why Running is
// false, and names the socket that did not answer.
type Status struct {
	Running    bool   `json:"running"`
	Version    string `json:"version"`
	Uptime     string `json:"uptime"`
	SocketPath string `json:"socketPath"`
	Error      string `json:"error"`
}

// StartResult is what starting the queue did. Started is false when the queue
// held nothing to run, which is not an error.
type StartResult struct {
	Started bool       `json:"started"`
	Job     client.Job `json:"job"`
	Run     client.Run `json:"run"`
}

// App is the bound object. Its exported methods are what the frontend can
// call, and Wails generates their TypeScript signatures.
type App struct {
	socket string
	client *client.Client
	emit   Emitter

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	follows map[int64]*follow
}

// New returns an app for the daemon on socketPath, emitting through emit.
func New(socketPath string, emit Emitter) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
		socket:  socketPath,
		client:  client.New(socketPath),
		emit:    emit,
		ctx:     ctx,
		cancel:  cancel,
		follows: map[int64]*follow{},
	}
}

// Shutdown stops every log the app is following. Wails calls it when the
// window closes; a test calls it in cleanup.
func (a *App) Shutdown() {
	a.cancel()
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, f := range a.follows {
		f.stop()
		delete(a.follows, id)
	}
}

// callTimeout bounds one request to the daemon, so a frontend poll cannot
// hang on a daemon that stopped answering.
const callTimeout = 10 * time.Second

func (a *App) call() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, callTimeout)
}

// Status asks the daemon about itself. It never fails: a daemon that does not
// answer is a state to show, not an error to handle.
func (a *App) Status() Status {
	ctx, cancel := a.call()
	defer cancel()
	st, err := a.client.DaemonStatus(ctx)
	if err != nil {
		return Status{SocketPath: a.socket, Error: err.Error()}
	}
	return Status{
		Running:    true,
		Version:    st.Version,
		Uptime:     st.Uptime.Truncate(time.Second).String(),
		SocketPath: st.SocketPath,
	}
}

// Projects lists the registered Projects.
func (a *App) Projects() ([]client.Project, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.ListProjects(ctx)
}

// Overview is what owl status reports.
func (a *App) Overview() (client.Overview, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.GetOverview(ctx)
}

// Jobs lists the queue, or with all every Job whatever its state.
func (a *App) Jobs(all bool) ([]client.Job, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.ListJobs(ctx, all)
}

// Job is one Job with its Runs, plan, handoff, checks and diff summary.
func (a *App) Job(id int64) (client.JobDetails, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.GetJob(ctx, id)
}

// Start runs the Job at the head of the queue.
func (a *App) Start() (StartResult, error) {
	ctx, cancel := a.call()
	defer cancel()
	job, run, started, err := a.client.StartRun(ctx)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Started: started, Job: job, Run: run}, nil
}

// Accept keeps a Job's branch and reclaims its worktree (ADR-0015).
func (a *App) Accept(id int64, force bool) (client.Job, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.AcceptJob(ctx, id, force)
}

// Drop discards a Job's work (ADR-0015).
func (a *App) Drop(id int64, force bool) (client.Job, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.DropJob(ctx, id, force)
}

// follow is one log being followed: how to stop it, and whether the
// frontend has seen any of it yet.
type follow struct {
	stop      context.CancelFunc
	delivered atomic.Bool
}

// FollowLog streams a Run's output to the frontend as EventLogLine events,
// from its first line and on until the Run ends, then emits EventLogEnd. It
// returns once the first line has arrived or the stream has ended, so a Run
// that does not exist is refused here rather than reported as an event.
// Following a Run already being followed replaces the earlier follow.
func (a *App) FollowLog(runID int64) error {
	ctx, stop := context.WithCancel(a.ctx)
	f := &follow{stop: stop}
	a.mu.Lock()
	if old := a.follows[runID]; old != nil {
		old.stop()
	}
	a.follows[runID] = f
	a.mu.Unlock()

	// first settles once: nil when the first line arrives, otherwise with
	// how the stream ended before any line came.
	first := make(chan error, 1)
	go func() {
		defer func() {
			a.mu.Lock()
			// Only this follow's own entry: a replacement may already be
			// registered under the same Run.
			if a.follows[runID] == f {
				delete(a.follows, runID)
			}
			a.mu.Unlock()
			stop()
		}()
		err := a.client.StreamRunLog(ctx, runID, true, func(line string) error {
			if f.delivered.CompareAndSwap(false, true) {
				first <- nil
			}
			a.emit.Emit(EventLogLine, LogLine{RunID: runID, Line: line})
			return nil
		})
		if ctx.Err() != nil {
			// Stopped from the frontend, replaced, or the app is closing:
			// not a failure of the Run's log, and nobody is listening. A
			// caller still waiting for the first line is let go.
			if !f.delivered.Load() {
				first <- nil
			}
			return
		}
		if !f.delivered.Load() {
			// Nothing arrived before the end: the caller learns why. A
			// failure here is refused rather than announced.
			first <- err
			if err != nil {
				return
			}
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		a.emit.Emit(EventLogEnd, LogEnd{RunID: runID, Error: msg})
	}()
	return <-first
}

// StopLog stops following a Run's log. Stopping a Run nobody is following is
// not an error.
func (a *App) StopLog(runID int64) {
	a.mu.Lock()
	f := a.follows[runID]
	delete(a.follows, runID)
	a.mu.Unlock()
	if f != nil {
		f.stop()
	}
}
