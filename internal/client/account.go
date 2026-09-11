package client

import (
	"context"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
)

// Account is a subscription Owl runs work on, as the daemon reports it
// (ADR-0019).
type Account struct {
	// Name identifies the Account, and is what a Project's configuration
	// names.
	Name string
	// Driver is the coding tool it belongs to.
	Driver string
	// ConfigDir is the Account's own tool configuration directory.
	ConfigDir string
	// HasCredential is whether the credential store still holds its secret.
	HasCredential bool
	// FailoverAllowed is recorded and unused.
	FailoverAllowed bool
	// Created is when the Account was added.
	Created time.Time
}

// AddAccountRequest is what owl account add carries. The token goes to the
// credential store and is never written to the database.
type AddAccountRequest struct {
	Name            string
	Driver          string
	Token           string
	FailoverAllowed bool
}

// AddAccount records an Account and stores its token.
func (c *Client) AddAccount(ctx context.Context, req AddAccountRequest) (Account, error) {
	res, err := c.accounts.AddAccount(ctx, connect.NewRequest(&codingowlv1.AddAccountRequest{
		Name:            req.Name,
		Driver:          req.Driver,
		Token:           req.Token,
		FailoverAllowed: req.FailoverAllowed,
	}))
	if err != nil {
		return Account{}, c.wrap(err)
	}
	return accountFromProto(res.Msg.GetAccount()), nil
}

// ListAccounts returns every Account, oldest first.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	res, err := c.accounts.ListAccounts(ctx, connect.NewRequest(&codingowlv1.ListAccountsRequest{}))
	if err != nil {
		return nil, c.wrap(err)
	}
	out := make([]Account, 0, len(res.Msg.GetAccounts()))
	for _, a := range res.Msg.GetAccounts() {
		out = append(out, accountFromProto(a))
	}
	return out, nil
}

// RemoveAccount takes an Account and its secret away, and returns it as it
// last stood.
func (c *Client) RemoveAccount(ctx context.Context, name string) (Account, error) {
	res, err := c.accounts.RemoveAccount(ctx, connect.NewRequest(&codingowlv1.RemoveAccountRequest{Name: name}))
	if err != nil {
		return Account{}, c.wrap(err)
	}
	return accountFromProto(res.Msg.GetAccount()), nil
}

func accountFromProto(a *codingowlv1.Account) Account {
	return Account{
		Name:            a.GetName(),
		Driver:          a.GetDriver(),
		ConfigDir:       a.GetConfigDir(),
		HasCredential:   a.GetHasCredential(),
		FailoverAllowed: a.GetFailoverAllowed(),
		Created:         a.GetCreated().AsTime(),
	}
}
