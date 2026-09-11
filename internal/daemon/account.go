package daemon

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
	"github.com/vojtechmares/coding-owl/internal/account"
)

// accountService exposes the Accounts over ConnectRPC. It holds no logic of
// its own; everything lives in internal/account.
type accountService struct {
	codingowlv1connect.UnimplementedAccountServiceHandler
	accounts *account.Service
}

func (s *accountService) AddAccount(ctx context.Context, req *connect.Request[codingowlv1.AddAccountRequest]) (*connect.Response[codingowlv1.AddAccountResponse], error) {
	a, err := s.accounts.Add(ctx, account.AddRequest{
		Name:            req.Msg.GetName(),
		Driver:          req.Msg.GetDriver(),
		Token:           req.Msg.GetToken(),
		FailoverAllowed: req.Msg.GetFailoverAllowed(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.AddAccountResponse{Account: toAccountProto(a)}), nil
}

func (s *accountService) ListAccounts(ctx context.Context, _ *connect.Request[codingowlv1.ListAccountsRequest]) (*connect.Response[codingowlv1.ListAccountsResponse], error) {
	as, err := s.accounts.List(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	res := &codingowlv1.ListAccountsResponse{Accounts: make([]*codingowlv1.Account, 0, len(as))}
	for _, a := range as {
		res.Accounts = append(res.Accounts, toAccountProto(a))
	}
	return connect.NewResponse(res), nil
}

func (s *accountService) RemoveAccount(ctx context.Context, req *connect.Request[codingowlv1.RemoveAccountRequest]) (*connect.Response[codingowlv1.RemoveAccountResponse], error) {
	a, err := s.accounts.Remove(ctx, req.Msg.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&codingowlv1.RemoveAccountResponse{Account: toAccountProto(a)}), nil
}

func toAccountProto(a account.Account) *codingowlv1.Account {
	return &codingowlv1.Account{
		Name:            a.Name,
		Driver:          a.Driver,
		ConfigDir:       a.ConfigDir,
		HasCredential:   a.HasCredential,
		FailoverAllowed: a.FailoverAllowed,
		Created:         timestamppb.New(a.Created),
	}
}
