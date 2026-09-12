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

// LogEnd says a followed Run's log ended: because the Run ended, or because
// the stream broke, and then Error names why. A follow the frontend stopped
// or replaced ends silently: nobody is listening.
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

// chatTimeout bounds one exchange with a model, which is a request through the
// daemon to somebody else rather than a request to the daemon.
const chatTimeout = 10 * time.Minute

// fetchTimeout bounds a request that fetches somebody else's repository, which
// is not a request to the daemon so much as one through it (ADR-0033).
const fetchTimeout = 5 * time.Minute

func (a *App) fetch() (context.Context, context.CancelFunc) {
	return context.WithTimeout(a.ctx, fetchTimeout)
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

// SkillUpdate is what updating a Project's Skills did: the ones that moved,
// every one the Project declares afterwards, and the files the user has to
// commit for a Project configured in its own repository (ADR-0014).
type SkillUpdate struct {
	Updated []client.Skill    `json:"updated"`
	All     []client.Skill    `json:"all"`
	Files   client.SkillFiles `json:"files"`
}

// Skills lists what a Project declares, with what each resolved to.
func (a *App) Skills(project string) ([]client.Skill, error) {
	ctx, cancel := a.call()
	defer cancel()
	skills, _, err := a.client.ListSkills(ctx, client.SkillRequest{Project: project})
	return skills, err
}

// UpdateSkills re-resolves a Project's Skills: with no names, the ones
// declared to move on their own; with names, those, pinned or not (ADR-0024).
func (a *App) UpdateSkills(project string, names []string) (SkillUpdate, error) {
	ctx, cancel := a.fetch()
	defer cancel()
	updated, all, files, err := a.client.UpdateSkills(ctx, client.SkillRequest{Project: project}, names)
	if err != nil {
		return SkillUpdate{}, err
	}
	return SkillUpdate{Updated: updated, All: all, Files: files}, nil
}

// Pause freezes the Run in progress and everything its Agent started, and
// starts the grace window (ADR-0011). The Run comes back with Paused set.
func (a *App) Pause() ([]client.Run, error) {
	ctx, cancel := a.call()
	defer cancel()
	// What the call did not reach is for a terminal; the app shows what is
	// paused by reading the Runs back (ADR-0009).
	runs, _, err := a.client.PauseRun(ctx)
	return runs, err
}

// Resume continues the frozen Run where it was, in the same Run.
func (a *App) Resume() ([]client.Run, error) {
	ctx, cancel := a.call()
	defer cancel()
	runs, _, err := a.client.ResumeRun(ctx)
	return runs, err
}

// Events the chat emits, and what each carries.
const (
	// EventChatDelta carries a ChatDelta, one piece of an answer.
	EventChatDelta = "chat:delta"
	// EventChatEnd carries a ChatEnd, when an answer has ended.
	EventChatEnd = "chat:end"
	// EventChatCommand carries a ChatCommand: a command the chat wants to run,
	// which runs nothing until AnswerCommand says so (ADR-0022).
	EventChatCommand = "chat:command"
	// EventChatCommandDone carries a ChatCommandDone: a command that has run,
	// and what it printed.
	EventChatCommandDone = "chat:command-done"
)

// Answers a person may give about a command, which AnswerCommand takes.
const (
	// AllowOnce runs this command and asks again about the next.
	AllowOnce = string(client.CommandAllowOnce)
	// AllowConversation runs this one and stops asking for the rest of the
	// conversation.
	AllowConversation = string(client.CommandAllowConversation)
	// Refuse runs nothing.
	Refuse = string(client.CommandRefuse)
)

// ChatCommand is a command the chat wants to run, waiting to be answered. It
// carries what will run rather than what the model wrote, so what a person
// agrees to is what happens.
type ChatCommand struct {
	ConversationID int64    `json:"conversationId"`
	RequestID      string   `json:"requestId"`
	Argv           []string `json:"argv"`
	Directory      string   `json:"directory"`
}

// ChatCommandDone is a command that has run, and what came of it. RequestID
// names the command: the id its ChatCommand carried, and an id of its own when
// nobody had to be asked because the conversation is already granted.
type ChatCommandDone struct {
	ConversationID int64    `json:"conversationId"`
	RequestID      string   `json:"requestId"`
	Argv           []string `json:"argv"`
	Directory      string   `json:"directory"`
	Output         string   `json:"output"`
	ExitCode       int      `json:"exitCode"`
	Cut            bool     `json:"cut"`
}

// ChatDelta is one piece of an answer as it arrives.
type ChatDelta struct {
	ConversationID int64  `json:"conversationId"`
	Text           string `json:"text"`
}

// ChatEnd says an answer has ended: because the model finished, or because it
// failed, and then Error names why. What arrived before a failure is what the
// user saw, and is kept.
type ChatEnd struct {
	ConversationID int64  `json:"conversationId"`
	Error          string `json:"error"`
}

// AnswerCommand answers a command the chat asked about, which is how consent
// is given: once, for the rest of the conversation, or not at all (ADR-0022).
// Nothing runs until this is called, and nothing runs at all if it refuses.
func (a *App) AnswerCommand(requestID, decision string) error {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.AnswerCommand(ctx, requestID, client.CommandDecision(decision))
}

// Models is every model the configured providers offer. A key is never among
// them: the daemon holds those (ADR-0022).
func (a *App) Models() ([]client.ChatModel, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.ListModels(ctx)
}

// Conversations lists them, the most recently spoken to first.
func (a *App) Conversations() ([]client.Conversation, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.ListConversations(ctx)
}

// Conversation is one conversation with what was said in it.
func (a *App) Conversation(id int64) (client.ConversationDetails, error) {
	ctx, cancel := a.call()
	defer cancel()
	return a.client.GetConversation(ctx, id)
}

// Send says something in a conversation and streams the answer to the frontend
// as EventChatDelta events, then EventChatEnd. It returns the conversation the
// answer belongs to, which is the one the daemon started when none was given:
// the daemon says which that is before any of the answer arrives, and Send
// waits for that much so the frontend knows where the pieces are going.
//
// The answer itself is streamed rather than returned because it arrives over
// time; what it ends up saying is in the conversation either way.
func (a *App) Send(conversation int64, model, text string) (int64, error) {
	return a.SendTo(conversation, "", model, text)
}

// SendTo is Send, naming which provider the model comes from: two providers
// may offer the same model, and overlapping access is expected rather than a
// problem to solve (ADR-0022).
func (a *App) SendTo(conversation int64, provider, model, text string) (int64, error) {
	ctx, cancel := context.WithTimeout(a.ctx, chatTimeout)
	// The daemon's first message carries the conversation and nothing else.
	opened := make(chan int64, 1)
	failed := make(chan error, 1)

	var announced sync.Once
	go func() {
		defer cancel()
		at := conversation
		_, err := a.client.SendMessage(ctx, client.SendMessageRequest{
			Conversation: conversation, Model: model, Provider: provider, Text: text,
		}, func(e client.SendMessageEvent) error {
			// The daemon says which conversation this is before any of the
			// answer arrives, whether the caller named one or not.
			if e.Conversation != 0 {
				at = e.Conversation
				announced.Do(func() { opened <- e.Conversation })
			}
			if p := e.Proposal; p != nil {
				a.emit.Emit(EventChatCommand, ChatCommand{
					ConversationID: at, RequestID: p.ID, Argv: p.Argv, Directory: p.Directory,
				})
			}
			if r := e.Ran; r != nil {
				a.emit.Emit(EventChatCommandDone, ChatCommandDone{
					ConversationID: at, RequestID: r.ID, Argv: r.Argv, Directory: r.Directory,
					Output: r.Output, ExitCode: r.ExitCode, Cut: r.Cut,
				})
			}
			if e.Delta == "" {
				return nil
			}
			a.emit.Emit(EventChatDelta, ChatDelta{ConversationID: at, Text: e.Delta})
			return nil
		})
		if err != nil {
			select {
			case failed <- err:
			default:
			}
		}
		message := ""
		if err != nil {
			message = err.Error()
		}
		a.emit.Emit(EventChatEnd, ChatEnd{ConversationID: at, Error: message})
	}()

	// A message the daemon refuses outright - a model nobody configured, a
	// conversation that is not there - is an error where the user typed it
	// rather than an event about a conversation that never started.
	select {
	case id := <-opened:
		return id, nil
	case err := <-failed:
		// An exchange over before this is reached leaves both ready at once,
		// and the conversation was announced before the failure: which of them
		// a select happened to take is not what the frontend needs to know.
		select {
		case id := <-opened:
			return id, nil
		default:
		}
		return conversation, err
	case <-ctx.Done():
		select {
		case id := <-opened:
			return id, nil
		default:
		}
		return conversation, ctx.Err()
	}
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
