package client

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// ChatProvider is a configured way to reach models, as the daemon reports it.
// Its key is never here: the daemon holds it (ADR-0022).
type ChatProvider struct {
	Name    string
	BaseURL string
	Models  []string
	Created time.Time
}

// ChatModel is one model a user may speak to, and the provider it comes from.
type ChatModel struct {
	Provider string
	ID       string
}

// Conversation is a chat, as it is listed.
type Conversation struct {
	ID               int64
	Title            string
	Model            string
	Created, Updated time.Time
}

// ChatMessage is one turn of a conversation.
type ChatMessage struct {
	Role    string
	Text    string
	Created time.Time
}

// ConversationDetails is one conversation with what was said in it.
type ConversationDetails struct {
	Conversation Conversation
	Messages     []ChatMessage
}

// AddProvider configures a way to reach models. The key goes to the daemon's
// credential store; nothing keeps it here.
func (c *Client) AddProvider(ctx context.Context, name, key, baseURL string, models []string) (ChatProvider, error) {
	res, err := c.chat.AddProvider(ctx, connect.NewRequest(&codingowlv1.AddProviderRequest{
		Name: name, Key: key, BaseUrl: baseURL, Models: models,
	}))
	if err != nil {
		return ChatProvider{}, c.wrap(err)
	}
	return providerFromProto(res.Msg.GetProvider()), nil
}

// ListProviders is what is configured, without any key.
func (c *Client) ListProviders(ctx context.Context) ([]ChatProvider, error) {
	res, err := c.chat.ListProviders(ctx, connect.NewRequest(&codingowlv1.ListProvidersRequest{}))
	if err != nil {
		return nil, c.wrap(err)
	}
	out := make([]ChatProvider, 0, len(res.Msg.GetProviders()))
	for _, p := range res.Msg.GetProviders() {
		out = append(out, providerFromProto(p))
	}
	return out, nil
}

// RemoveProvider takes a provider and its key away.
func (c *Client) RemoveProvider(ctx context.Context, name string) error {
	_, err := c.chat.RemoveProvider(ctx, connect.NewRequest(&codingowlv1.RemoveProviderRequest{Name: name}))
	if err != nil {
		return c.wrap(err)
	}
	return nil
}

// ListModels is every model the configured providers offer.
func (c *Client) ListModels(ctx context.Context) ([]ChatModel, error) {
	res, err := c.chat.ListModels(ctx, connect.NewRequest(&codingowlv1.ListModelsRequest{}))
	if err != nil {
		return nil, c.wrap(err)
	}
	out := make([]ChatModel, 0, len(res.Msg.GetModels()))
	for _, m := range res.Msg.GetModels() {
		out = append(out, ChatModel{Provider: m.GetProvider(), ID: m.GetId()})
	}
	return out, nil
}

// ListConversations returns them, the most recently spoken to first.
func (c *Client) ListConversations(ctx context.Context) ([]Conversation, error) {
	res, err := c.chat.ListConversations(ctx, connect.NewRequest(&codingowlv1.ListConversationsRequest{}))
	if err != nil {
		return nil, c.wrap(err)
	}
	out := make([]Conversation, 0, len(res.Msg.GetConversations()))
	for _, conversation := range res.Msg.GetConversations() {
		out = append(out, conversationFromProto(conversation))
	}
	return out, nil
}

// GetConversation returns one with what was said in it.
func (c *Client) GetConversation(ctx context.Context, id int64) (ConversationDetails, error) {
	res, err := c.chat.GetConversation(ctx, connect.NewRequest(&codingowlv1.GetConversationRequest{Id: id}))
	if err != nil {
		return ConversationDetails{}, c.wrap(err)
	}
	out := ConversationDetails{Conversation: conversationFromProto(res.Msg.GetConversation())}
	for _, m := range res.Msg.GetMessages() {
		out.Messages = append(out.Messages, ChatMessage{
			Role: m.GetRole(), Text: m.GetText(), Created: m.GetCreated().AsTime(),
		})
	}
	return out, nil
}

// SendMessageRequest is one thing said in a conversation.
type SendMessageRequest struct {
	// Conversation is the conversation to say it in, and zero starts one.
	Conversation int64
	// Model is what to say it to.
	Model string
	// Provider is where that model comes from, for a model two providers
	// offer. Empty takes whichever provider offers it.
	Provider string
	// Text is what the user said.
	Text string
}

// CommandProposal is a command the chat wants to run, waiting to be answered.
// It carries what will run rather than what the model wrote (ADR-0022).
type CommandProposal struct {
	// ID is what an answer names.
	ID string
	// Argv is the program and its arguments, exactly as they will be passed.
	Argv []string
	// Directory is where it will run.
	Directory string
}

// CommandRun is what came of one.
type CommandRun struct {
	// ID names the command this is about: the same id the proposal carried,
	// and an id of its own when nobody had to be asked.
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

// CommandDecision is what a person answered about a command.
type CommandDecision string

const (
	// CommandAllowOnce runs this command and asks again about the next.
	CommandAllowOnce CommandDecision = "once"
	// CommandAllowConversation runs this one and stops asking for the rest of
	// the conversation.
	CommandAllowConversation CommandDecision = "conversation"
	// CommandRefuse runs nothing.
	CommandRefuse CommandDecision = "refuse"
)

// SendMessageEvent is one thing the daemon said while answering: a piece of
// the answer, a command it wants to run, or a command that has run.
type SendMessageEvent struct {
	// Conversation is the conversation it belongs to, carried by every event
	// including the first, which carries only that.
	Conversation int64
	// Delta is a piece of the answer, empty when this event is about a
	// command.
	Delta string
	// Proposal is a command waiting to be answered, nil when it is not one.
	Proposal *CommandProposal
	// Ran is a command that has run, nil when it is not one.
	Ran *CommandRun
}

// SendMessage says something in a conversation and calls on for each thing the
// daemon says while answering, with the conversation it belongs to. It returns
// that conversation, which is the one the daemon started when none was named.
func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest, on func(SendMessageEvent) error) (int64, error) {
	conversation := req.Conversation
	stream, err := c.chat.SendMessage(ctx, connect.NewRequest(&codingowlv1.SendMessageRequest{
		ConversationId: req.Conversation, Model: req.Model, Text: req.Text, Provider: req.Provider,
	}))
	if err != nil {
		return conversation, c.wrap(err)
	}
	defer func() { _ = stream.Close() }()
	id := conversation
	for stream.Receive() {
		msg := stream.Msg()
		if msg.GetConversationId() != 0 {
			id = msg.GetConversationId()
		}
		if on == nil {
			continue
		}
		if err := on(SendMessageEvent{
			Conversation: id,
			Delta:        msg.GetDelta(),
			Proposal:     proposalFromProto(msg.GetProposal()),
			Ran:          commandRunFromProto(msg.GetRan()),
		}); err != nil {
			return id, err
		}
	}
	if err := stream.Err(); err != nil {
		return id, c.wrap(err)
	}
	return id, nil
}

// AnswerCommand answers a command the chat asked about. Nothing runs until it
// does, and nothing runs at all if the answer is to refuse.
func (c *Client) AnswerCommand(ctx context.Context, id string, d CommandDecision) error {
	decision, ok := decisionToProto[d]
	if !ok {
		return fmt.Errorf("%q is not an answer; a command is allowed once, allowed for the conversation, or refused", d)
	}
	_, err := c.chat.AnswerCommand(ctx, connect.NewRequest(&codingowlv1.AnswerCommandRequest{
		Id: id, Decision: decision,
	}))
	return c.wrap(err)
}

// decisionToProto is the answers a person may give, on the wire.
var decisionToProto = map[CommandDecision]codingowlv1.CommandDecision{
	CommandAllowOnce:         codingowlv1.CommandDecision_COMMAND_DECISION_ALLOW_ONCE,
	CommandAllowConversation: codingowlv1.CommandDecision_COMMAND_DECISION_ALLOW_CONVERSATION,
	CommandRefuse:            codingowlv1.CommandDecision_COMMAND_DECISION_REFUSE,
}

func proposalFromProto(p *codingowlv1.CommandProposal) *CommandProposal {
	if p == nil {
		return nil
	}
	return &CommandProposal{ID: p.GetId(), Argv: p.GetArgv(), Directory: p.GetDirectory()}
}

func commandRunFromProto(r *codingowlv1.CommandRun) *CommandRun {
	if r == nil {
		return nil
	}
	return &CommandRun{
		ID: r.GetId(), Argv: r.GetArgv(), Directory: r.GetDirectory(),
		Output: r.GetOutput(), ExitCode: int(r.GetExitCode()), Cut: r.GetCut(),
	}
}

func providerFromProto(p *codingowlv1.ChatProvider) ChatProvider {
	return ChatProvider{
		Name: p.GetName(), BaseURL: p.GetBaseUrl(), Models: p.GetModels(),
		Created: p.GetCreated().AsTime(),
	}
}

func conversationFromProto(c *codingowlv1.Conversation) Conversation {
	return Conversation{
		ID: c.GetId(), Title: c.GetTitle(), Model: c.GetModel(),
		Created: c.GetCreated().AsTime(), Updated: c.GetUpdated().AsTime(),
	}
}
