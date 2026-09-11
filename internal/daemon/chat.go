package daemon

import (
	"context"
	"errors"

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
		})
	})
	// A caller that hangs up ends the stream; that is how a chat is left, not
	// something to report as a failure.
	if errors.Is(err, context.Canceled) {
		return nil
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
