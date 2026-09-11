package client

import (
	"context"
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

// SendMessage says something in a conversation and calls onDelta for each
// piece of the answer as it arrives. It returns the conversation the answer
// belongs to, which is the one the daemon started when none was named.
func (c *Client) SendMessage(ctx context.Context, conversation int64, model, text string, onDelta func(string) error) (int64, error) {
	stream, err := c.chat.SendMessage(ctx, connect.NewRequest(&codingowlv1.SendMessageRequest{
		ConversationId: conversation, Model: model, Text: text,
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
		if delta := msg.GetDelta(); delta != "" && onDelta != nil {
			if err := onDelta(delta); err != nil {
				return id, err
			}
		}
	}
	if err := stream.Err(); err != nil {
		return id, c.wrap(err)
	}
	return id, nil
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
