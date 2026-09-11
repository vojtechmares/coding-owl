package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// maxTurns bounds how many times a model may ask for a tool before Owl stops
// asking it again. A model that loops is a model that has not understood the
// question, and a daemon nobody is watching must not follow it for ever.
const maxTurns = 8

// systemPrompt is what the model is told it is. It says what the tools are for
// and, plainly, that it cannot act: the chat proposes nothing and runs nothing
// in the MVP (ADR-0022).
const systemPrompt = `You are the assistant inside Coding Owl, a tool that runs coding agents unattended.

The user is asking about work Owl has done: Projects it knows, Jobs it queued, Runs it carried out, what Verification said, and what a Job's branch changed. Answer from what the tools tell you rather than from what you assume, and say plainly when the tools do not say.

The tools only read. You cannot start, stop, accept or change anything, and you must not claim to have done so or offer to. If the user asks for an action, say what they would run themselves.

Be brief. Name Jobs and Runs by their ids, and quote what a check printed rather than summarising it away.`

// SendRequest is one thing said in a conversation.
type SendRequest struct {
	// Conversation is the conversation to say it in, and zero starts one.
	Conversation int64
	// Model is what to say it to, from the configured providers.
	Model string
	// Text is what the user said.
	Text string
}

// Delta is a piece of an answer as it arrives.
type Delta struct {
	// Conversation is the conversation the answer belongs to, which a caller
	// that started one needs before the first piece arrives.
	Conversation int64
	// Text is the piece.
	Text string
}

// Send says something in a conversation and streams the answer back through
// emit, a piece at a time. It returns the conversation the answer belongs to,
// which is the one it started when the request carried none.
//
// What arrives before a failure is kept: a conversation records what was said,
// including half an answer, because that is what the user saw.
func (s *Service) Send(ctx context.Context, req SendRequest, emit func(Delta) error) (int64, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return 0, invalid("a message needs something to say")
	}
	provider, client, err := s.clientFor(ctx, req.Model)
	if err != nil {
		return 0, err
	}
	conversation, history, err := s.open(ctx, req, text)
	if err != nil {
		return conversation, err
	}

	answer, err := s.converse(ctx, client, provider, req.Model, history, conversation, emit)
	// Whatever arrived is what the user saw, so it is written down before the
	// failure is reported.
	if strings.TrimSpace(answer) != "" {
		if _, addErr := s.store.AddChatMessage(ctx, store.ChatMessage{
			ConversationID: conversation, Role: RoleAssistant, Text: answer, Created: s.now().UTC(),
		}); addErr != nil {
			return conversation, addErr
		}
	}
	return conversation, err
}

// clientFor is the provider a model comes from, and a client for it.
func (s *Service) clientFor(ctx context.Context, model string) (store.ChatProvider, Client, error) {
	if strings.TrimSpace(model) == "" {
		return store.ChatProvider{}, nil, invalid("a message needs a model to send it to")
	}
	providers, err := s.store.ListChatProviders(ctx)
	if err != nil {
		return store.ChatProvider{}, nil, err
	}
	for _, p := range providers {
		for _, offered := range p.Models {
			if offered != model {
				continue
			}
			key, err := s.creds.Get(ctx, p.CredentialRef)
			if err != nil {
				return store.ChatProvider{}, nil, fmt.Errorf(
					"reading the key for %s: %w", p.Name, err)
			}
			client, err := s.client(p, key)
			if err != nil {
				return store.ChatProvider{}, nil, err
			}
			return p, client, nil
		}
	}
	return store.ChatProvider{}, nil, invalid(
		"no configured provider offers the model %q; `owl providers list` says what is configured", model)
}

// open finds or starts the conversation and records what was said in it,
// returning everything said so far.
func (s *Service) open(ctx context.Context, req SendRequest, text string) (int64, []Turn, error) {
	now := s.now().UTC()
	id := req.Conversation
	if id == 0 {
		c, err := s.store.StartConversation(ctx, store.Conversation{
			Title: titleOf(text), Model: req.Model, Created: now, Updated: now,
		})
		if err != nil {
			return 0, nil, err
		}
		id = c.ID
	} else if _, err := s.store.GetConversation(ctx, id); err != nil {
		if errors.Is(err, store.ErrConversationNotFound) {
			return 0, nil, invalid("no conversation %d", id)
		}
		return 0, nil, err
	}
	if _, err := s.store.AddChatMessage(ctx, store.ChatMessage{
		ConversationID: id, Role: RoleUser, Text: text, Created: now,
	}); err != nil {
		return id, nil, err
	}
	if err := s.store.TouchConversation(ctx, id, req.Model, now); err != nil {
		return id, nil, err
	}
	said, err := s.store.ListChatMessages(ctx, id)
	if err != nil {
		return id, nil, err
	}
	history := make([]Turn, 0, len(said))
	for _, m := range said {
		history = append(history, Turn{Role: m.Role, Text: m.Text})
	}
	return id, history, nil
}

// converse asks the model, carries out the tools it asks for, and asks again
// until it has an answer. It returns what the model said, whatever it was that
// stopped the conversation.
func (s *Service) converse(ctx context.Context, client Client, provider store.ChatProvider,
	model string, history []Turn, conversation int64, emit func(Delta) error,
) (string, error) {
	var answer strings.Builder
	for turn := 0; turn < maxTurns; turn++ {
		reply, err := client.Stream(ctx, Request{
			Model:    model,
			System:   systemPrompt,
			Messages: history,
			Tools:    s.tools.Definitions(),
		}, func(piece string) error {
			answer.WriteString(piece)
			return emit(Delta{Conversation: conversation, Text: piece})
		})
		if err != nil {
			return answer.String(), fmt.Errorf("%s: %w", provider.Name, err)
		}
		if len(reply.ToolCalls) == 0 {
			return answer.String(), nil
		}
		// What the model asked for goes back to it as another turn, so the
		// next answer is given with the results in front of it.
		history = append(history, Turn{Role: RoleAssistant, ToolCalls: reply.ToolCalls, Text: reply.Text})
		results := make([]ToolResult, 0, len(reply.ToolCalls))
		for _, call := range reply.ToolCalls {
			results = append(results, s.tools.Call(ctx, call))
		}
		history = append(history, Turn{Role: RoleUser, ToolResults: results})
	}
	return answer.String(), fmt.Errorf(
		"%s asked for tools %d times without answering", provider.Name, maxTurns)
}
