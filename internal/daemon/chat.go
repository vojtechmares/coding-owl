package daemon

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/chat"
)

// chatService exposes the chat over ConnectRPC. It holds no logic of its own;
// everything lives in internal/chat.
type chatService struct {
	codingowlv1connect.UnimplementedChatServiceHandler
	chat *chat.Service
	// daemon is the daemon's own lifetime, which every request's context is
	// derived from: it is how a stream ended by the daemon stopping is told
	// apart from one the caller left.
	daemon context.Context
}

func (s *chatService) AddProvider(ctx context.Context, req *connect.Request[codingowlv1.AddProviderRequest]) (*connect.Response[codingowlv1.AddProviderResponse], error) {
	p, err := s.chat.AddProvider(ctx, req.Msg.GetName(), req.Msg.GetKey(),
		req.Msg.GetBaseUrl(), req.Msg.GetModels())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.AddProviderResponse{Provider: toProviderProto(p)}), nil
}

func (s *chatService) ListProviders(ctx context.Context, _ *connect.Request[codingowlv1.ListProvidersRequest]) (*connect.Response[codingowlv1.ListProvidersResponse], error) {
	all, err := s.chat.ListProviders(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.ListProvidersResponse{Providers: make([]*codingowlv1.ChatProvider, 0, len(all))}
	for _, p := range all {
		res.Providers = append(res.Providers, toProviderProto(p))
	}
	return connect.NewResponse(res), nil
}

func (s *chatService) RemoveProvider(ctx context.Context, req *connect.Request[codingowlv1.RemoveProviderRequest]) (*connect.Response[codingowlv1.RemoveProviderResponse], error) {
	if err := s.chat.RemoveProvider(ctx, req.Msg.GetName()); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.RemoveProviderResponse{}), nil
}

func (s *chatService) ListModels(ctx context.Context, _ *connect.Request[codingowlv1.ListModelsRequest]) (*connect.Response[codingowlv1.ListModelsResponse], error) {
	models, err := s.chat.Models(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.ListModelsResponse{Models: make([]*codingowlv1.ChatModel, 0, len(models))}
	for _, m := range models {
		res.Models = append(res.Models, &codingowlv1.ChatModel{Provider: m.Provider, Id: m.ID})
	}
	return connect.NewResponse(res), nil
}

func (s *chatService) ListConversations(ctx context.Context, _ *connect.Request[codingowlv1.ListConversationsRequest]) (*connect.Response[codingowlv1.ListConversationsResponse], error) {
	all, err := s.chat.Conversations(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.ListConversationsResponse{
		Conversations: make([]*codingowlv1.Conversation, 0, len(all)),
	}
	for _, c := range all {
		res.Conversations = append(res.Conversations, toConversationProto(c))
	}
	return connect.NewResponse(res), nil
}

func (s *chatService) GetConversation(ctx context.Context, req *connect.Request[codingowlv1.GetConversationRequest]) (*connect.Response[codingowlv1.GetConversationResponse], error) {
	c, messages, err := s.chat.Conversation(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.GetConversationResponse{
		Conversation: toConversationProto(c),
		Messages:     make([]*codingowlv1.ChatMessage, 0, len(messages)),
	}
	for _, m := range messages {
		res.Messages = append(res.Messages, &codingowlv1.ChatMessage{
			Role: m.Role, Text: m.Text, Created: timestamppb.New(m.Created),
		})
	}
	return connect.NewResponse(res), nil
}

// SendMessage says something in a conversation and streams the answer back.
// The first message carries the conversation, so a caller that started one
// knows which it is before any of the answer arrives.
func (s *chatService) SendMessage(ctx context.Context, req *connect.Request[codingowlv1.SendMessageRequest], stream *connect.ServerStream[codingowlv1.SendMessageResponse]) error {
	var told bool
	id, err := s.chat.Send(ctx, chat.SendRequest{
		Conversation: req.Msg.GetConversationId(),
		Model:        req.Msg.GetModel(),
		Text:         req.Msg.GetText(),
		Provider:     req.Msg.GetProvider(),
	}, func(d chat.Delta) error {
		told = true
		return stream.Send(&codingowlv1.SendMessageResponse{
			ConversationId: d.Conversation, Delta: d.Text,
			Proposal: toProposalProto(d.Proposal), Ran: toCommandRunProto(d.Ran),
		})
	})
	// An exchange whose context ended was not the model's or Owl's doing,
	// whatever the failure it surfaced as - a consent nobody gave, a provider
	// call cut short. A caller that hangs up ends it; that is how a chat is
	// left, not something to report as a failure. The daemon stopping ends
	// it the same way, and that one the caller is still there to be told
	// about.
	if err != nil && ctx.Err() != nil {
		if s.daemon.Err() == nil {
			return nil
		}
		return connect.NewError(connect.CodeCanceled,
			errors.New("the exchange was cancelled: the daemon is stopping"))
	}
	if err != nil {
		return rpcError(err)
	}
	if !told {
		// An answer with nothing in it still says which conversation it was.
		return stream.Send(&codingowlv1.SendMessageResponse{ConversationId: id})
	}
	return nil
}

// AnswerCommand answers a command the chat asked about. The answer reaches the
// exchange that is waiting for it, which then runs the command or does not
// (ADR-0022).
func (s *chatService) AnswerCommand(_ context.Context, req *connect.Request[codingowlv1.AnswerCommandRequest]) (*connect.Response[codingowlv1.AnswerCommandResponse], error) {
	decision, ok := decisionFromProto[req.Msg.GetDecision()]
	if !ok {
		return nil, rpcError(&chat.InvalidError{Err: fmt.Errorf(
			"no answer was given; a command is allowed once, allowed for the conversation, or refused")})
	}
	if err := s.chat.AnswerCommand(req.Msg.GetId(), decision); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.AnswerCommandResponse{}), nil
}

// decisionFromProto is the answers a person may give, as they arrive.
var decisionFromProto = map[codingowlv1.CommandDecision]chat.Decision{
	codingowlv1.CommandDecision_COMMAND_DECISION_ALLOW_ONCE:         chat.AllowOnce,
	codingowlv1.CommandDecision_COMMAND_DECISION_ALLOW_CONVERSATION: chat.AllowConversation,
	codingowlv1.CommandDecision_COMMAND_DECISION_REFUSE:             chat.Refuse,
}

func toProposalProto(p *chat.Proposal) *codingowlv1.CommandProposal {
	if p == nil {
		return nil
	}
	return &codingowlv1.CommandProposal{Id: p.ID, Argv: p.Argv, Directory: p.Directory}
}

func toCommandRunProto(r *chat.CommandRun) *codingowlv1.CommandRun {
	if r == nil {
		return nil
	}
	return &codingowlv1.CommandRun{
		Id: r.ID, Argv: r.Argv, Directory: r.Directory,
		Output: r.Output, ExitCode: int32(r.ExitCode), Cut: r.Cut,
	}
}

func toProviderProto(p chat.Config) *codingowlv1.ChatProvider {
	return &codingowlv1.ChatProvider{
		Name: p.Name, BaseUrl: p.BaseURL, Models: p.Models, Created: timestamppb.New(p.Created),
	}
}

func toConversationProto(c chat.Conversation) *codingowlv1.Conversation {
	return &codingowlv1.Conversation{
		Id: c.ID, Title: c.Title, Model: c.Model,
		Created: timestamppb.New(c.Created), Updated: timestamppb.New(c.Updated),
	}
}
