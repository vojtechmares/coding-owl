package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/command"
)

// toolRunCommand is the one tool that is not a read of what Owl knows: it runs
// a program in a Project. Nothing happens without consent, there is no shell,
// and what it may run is a short allowlist (ADR-0022).
const toolRunCommand = "run_command"

// consentWindow is how long a command waits to be answered. The chat is
// somebody sitting in front of it; one that is not answered by now is one
// nobody is going to answer, and the Agent waiting for it is holding an
// exchange open.
const consentWindow = 5 * time.Minute

// Decision is what a person answered about a command they were asked about.
type Decision string

const (
	// AllowOnce runs this command and asks again about the next.
	AllowOnce Decision = "once"
	// AllowConversation runs this one and stops asking for the rest of the
	// conversation.
	AllowConversation Decision = "conversation"
	// Refuse runs nothing.
	Refuse Decision = "refuse"
)

// Decisions is every answer a person may give, in the order they are offered.
var Decisions = []Decision{AllowOnce, AllowConversation, Refuse}

// Proposal is a command the chat wants to run, waiting to be answered. It
// carries what will run rather than what the model wrote, so what a person
// agrees to is what happens.
type Proposal struct {
	// ID is what an answer names.
	ID string
	// Argv is the program and its arguments, exactly as they will be passed.
	Argv []string
	// Directory is where it will run.
	Directory string
}

// CommandRun is what came of one.
type CommandRun struct {
	// ID is the proposal it answers.
	ID string
	// Argv and Directory are what ran, and where.
	Argv      []string
	Directory string
	// Output is what it printed, bounded.
	Output string
	// ExitCode is how it ended.
	ExitCode int
	// Cut is whether it printed more than Owl kept.
	Cut bool
}

// Command is what the model is told it may ask for.
func commandDefinition() ToolDefinition {
	return ToolDefinition{
		Name: toolRunCommand,
		Description: "Run one read-only command in a Project or one of its worktrees and return what it " +
			"printed. There is no shell: the command is split into words, `" + strings.Join(command.Programs(), "`, `") +
			"` are the only programs, and each takes only the arguments Owl permits. The user is asked before " +
			"anything runs, and may refuse.",
		Schema: object(map[string]any{
			"command": map[string]any{
				"type": "string",
				"description": "The command, as it would be typed: for example `git log --oneline -n 20` " +
					"or `grep -rn owl internal`.",
			},
			"directory": map[string]any{
				"type": "string",
				"description": "Where to run it: a Project's path, or the worktree of one of its Jobs. " +
					"Use list_projects and get_job to find them.",
			},
		}, []string{"command", "directory"}),
	}
}

// AnswerCommand is a person answering a command the chat asked about. The
// answer reaches the exchange that is waiting for it, which then runs the
// command or does not.
func (s *Service) AnswerCommand(id string, d Decision) error {
	switch d {
	case AllowOnce, AllowConversation, Refuse:
	default:
		return invalid("%q is not an answer; a command is allowed once, allowed for the conversation, or refused", d)
	}
	s.askingMu.Lock()
	waiting, ok := s.asking[id]
	if ok {
		delete(s.asking, id)
	}
	s.askingMu.Unlock()
	if !ok {
		return invalid("no command called %s is waiting to be answered", id)
	}
	waiting <- d
	return nil
}

// runCommand is the whole of what the command tool does: read what the model
// asked for, refuse what the gate refuses, ask the user, and run it. Every
// refusal is an answer the model is given rather than an end to the exchange,
// because being told no is something it can act on.
func (s *Service) runCommand(ctx context.Context, conversation int64, call ToolCall,
	emit func(Delta) error,
) (ToolResult, error) {
	var args struct {
		Command   string `json:"command"`
		Directory string `json:"directory"`
	}
	if err := arguments(call, &args); err != nil {
		return refusedCall(call, err.Error()), nil
	}
	argv, err := command.Parse(args.Command)
	if err != nil {
		return refusedCall(call, err.Error()), nil
	}
	if err := command.Check(argv); err != nil {
		return refusedCall(call, err.Error()), nil
	}
	// Where it may run is settled before anybody is asked: a person should not
	// be shown a command that was never going to run.
	dir, err := s.tools.view.Where(ctx, args.Directory)
	if err != nil {
		return refusedCall(call, err.Error()), nil
	}

	allowed, err := s.consent(ctx, conversation, argv, dir, emit)
	if err != nil {
		return ToolResult{}, err
	}
	if !allowed {
		return refusedCall(call, fmt.Sprintf("the user refused to run %s", strings.Join(argv, " "))), nil
	}

	got, err := command.Run(ctx, dir, argv)
	if err != nil {
		return refusedCall(call, err.Error()), nil
	}
	ran := CommandRun{
		ID: call.ID, Argv: argv, Directory: dir,
		Output: got.Output, ExitCode: got.ExitCode, Cut: got.Cut,
	}
	// The person who allowed it sees what it did, whatever the model goes on
	// to say about it.
	if err := emit(Delta{Conversation: conversation, Ran: &ran}); err != nil {
		return ToolResult{}, err
	}
	return ToolResult{CallID: call.ID, Text: cut(said(ran), maxResult)}, nil
}

// said is what the model is told about a command that ran: what ran, how it
// ended, and what it printed. All three, because a command that exits badly
// and prints nothing is still an answer.
func said(ran CommandRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nexit %d\n", strings.Join(ran.Argv, " "), ran.ExitCode)
	if ran.Cut {
		b.WriteString("(this is the beginning of what it printed; there was more)\n")
	}
	if strings.TrimSpace(ran.Output) == "" {
		b.WriteString("(it printed nothing)\n")
		return b.String()
	}
	b.WriteString(ran.Output)
	return b.String()
}

// consent asks the user about a command, unless they have already said to stop
// asking in this conversation. It reports whether the command may run.
func (s *Service) consent(ctx context.Context, conversation int64, argv []string, dir string,
	emit func(Delta) error,
) (bool, error) {
	s.askingMu.Lock()
	granted := s.granted[conversation]
	s.askingMu.Unlock()
	if granted {
		return true, nil
	}

	id, err := askID()
	if err != nil {
		return false, err
	}
	answered := make(chan Decision, 1)
	s.askingMu.Lock()
	s.asking[id] = answered
	s.askingMu.Unlock()
	defer func() {
		s.askingMu.Lock()
		delete(s.asking, id)
		s.askingMu.Unlock()
	}()

	if err := emit(Delta{Conversation: conversation, Proposal: &Proposal{
		ID: id, Argv: argv, Directory: dir,
	}}); err != nil {
		return false, err
	}

	waited := time.NewTimer(consentWindow)
	defer waited.Stop()
	select {
	case d := <-answered:
		if d == AllowConversation {
			s.askingMu.Lock()
			s.granted[conversation] = true
			s.askingMu.Unlock()
		}
		return d != Refuse, nil
	case <-waited.C:
		// Nobody answered. Nothing runs: consent that was never given is not
		// consent (ADR-0022).
		return false, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// refusedCall is what the model is told about a command that did not run.
func refusedCall(call ToolCall, why string) ToolResult {
	return ToolResult{CallID: call.ID, Text: cut(why, maxResult), Failed: true}
}

// askID names one proposal, so that an answer reaches the command it is about
// and no other.
func askID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("naming a command to ask about: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
