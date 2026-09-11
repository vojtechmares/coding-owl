package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vojtechmares/coding-owl/internal/store"
)

// maxTurns bounds how many times a model may ask for a tool before Owl stops
// asking it again. A model that loops is a model that has not understood the
// question, and a daemon nobody is watching must not follow it for ever.
const maxTurns = 8

// maxAnswer bounds one answer. It is held in memory, written to one row and
// sent back to the model in every later exchange, so a provider that never
// stops talking is stopped here.
const maxAnswer = 256 << 10

// maxHistory bounds what of a conversation is sent to the model: the most
// recent turns up to this much text. A conversation grows without end, and a
// request that carried all of it would grow with it.
const maxHistory = 128 << 10

// keepTimeout bounds writing down what arrived after the caller has gone. The
// write outlives the request, so it needs an end of its own.
const keepTimeout = 5 * time.Second

// systemPrompt is what the model is told it is. It says what the tools are for
// and, plainly, that it cannot act: every tool this chat is given only reads.
// ADR-0022 keeps a shell out of the chat; what it may one day be allowed to
// propose is not this work.
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
	// Provider is where that model comes from, for a model two providers
	// offer: overlapping access is expected rather than a problem to solve
	// (ADR-0022). Empty takes whichever provider offers it.
	Provider string
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
	// A caller that wants the answer only in the conversation passes none, and
	// the pieces still have to go somewhere.
	if emit == nil {
		emit = func(Delta) error { return nil }
	}
	provider, client, err := s.clientFor(ctx, req.Model, req.Provider)
	if err != nil {
		return 0, err
	}
	conversation, history, err := s.open(ctx, req, text)
	if err != nil {
		return conversation, err
	}
	// Which conversation the answer belongs to is said before any of it
	// arrives, so a caller that started one knows where the pieces are going.
	if err := emit(Delta{Conversation: conversation}); err != nil {
		return conversation, err
	}

	answer, err := s.converse(ctx, client, provider, req.Model, history, conversation, emit)
	// Whatever arrived is what the user saw, so it is written down before the
	// failure is reported.
	if strings.TrimSpace(answer) != "" {
		// The user watched this arrive, so it is kept even when what stopped
		// the answer was the caller going away: a closed window must not take
		// half a conversation with it.
		keep, done := context.WithTimeout(context.WithoutCancel(ctx), keepTimeout)
		_, addErr := s.store.AddChatMessage(keep, store.ChatMessage{
			ConversationID: conversation, Role: RoleAssistant, Text: answer, Created: s.now().UTC(),
		})
		done()
		if addErr != nil {
			return conversation, addErr
		}
	}
	return conversation, err
}

// clientFor is the provider a model comes from, and a client for it.
func (s *Service) clientFor(ctx context.Context, model, from string) (store.ChatProvider, Client, error) {
	if strings.TrimSpace(model) == "" {
		return store.ChatProvider{}, nil, invalid("a message needs a model to send it to")
	}
	from = strings.ToLower(strings.TrimSpace(from))
	providers, err := s.store.ListChatProviders(ctx)
	if err != nil {
		return store.ChatProvider{}, nil, err
	}
	for _, p := range providers {
		if from != "" && !strings.EqualFold(p.Name, from) {
			continue
		}
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
	if from != "" {
		return store.ChatProvider{}, nil, invalid(
			"%s offers no model %q; `owl providers list` says what is configured", from, model)
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
	// The most recent turns, oldest first, up to what a request may carry: a
	// long conversation is still one request.
	var history []Turn
	var carried int
	for at := len(said) - 1; at >= 0; at-- {
		m := said[at]
		if carried+len(m.Text) > maxHistory && len(history) > 0 {
			break
		}
		carried += len(m.Text)
		history = append([]Turn{{Role: m.Role, Text: m.Text}}, history...)
	}
	return id, history, nil
}

// errTooLong stops a provider that will not stop talking. It is Owl's own, so
// the caller can tell it from what the provider said.
var errTooLong = errors.New("the answer is longer than Owl will keep")

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
			if answer.Len() >= maxAnswer {
				// The model has said more than Owl will keep. What it says
				// after this is not shown and not written down, so it is not
				// carried either.
				return errTooLong
			}
			answer.WriteString(piece)
			return emit(Delta{Conversation: conversation, Text: piece})
		})
		if errors.Is(err, errTooLong) {
			return answer.String(), fmt.Errorf("%s said more than %d bytes without stopping",
				provider.Name, maxAnswer)
		}
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
