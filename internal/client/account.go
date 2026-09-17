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

// Instructions are an Account's standing instructions: text every Run on that
// Account reads, whichever Project the Run is for (ADR-0037).
type Instructions struct {
	// Account is whose they are.
	Account string
	// Driver is the coding tool that Account is on.
	Driver string
	// File is what that Driver's tool calls the file, and is empty for a tool
	// that reads none: a caller with an empty File has nothing to show and
	// nothing to save.
	File string
	// Path is where the file is, and is empty when File is.
	Path string
	// Text is what it says, and is empty for an Account given none.
	Text string
}

// AccountCredential returns an Account with the secret to run its tool on,
// which is what running that tool as the Account needs. It is the one call
// that hands a secret back: `owl account exec` runs the tool at the user's own
// terminal, so the daemon cannot run it for them.
func (c *Client) AccountCredential(ctx context.Context, name string) (Account, string, error) {
	res, err := c.accounts.AccountCredential(ctx, connect.NewRequest(&codingowlv1.AccountCredentialRequest{Name: name}))
	if err != nil {
		return Account{}, "", c.wrap(err)
	}
	return accountFromProto(res.Msg.GetAccount()), res.Msg.GetToken(), nil
}

// AccountInstructions returns an Account's standing instructions.
func (c *Client) AccountInstructions(ctx context.Context, name string) (Instructions, error) {
	res, err := c.accounts.GetAccountInstructions(ctx, connect.NewRequest(&codingowlv1.GetAccountInstructionsRequest{Name: name}))
	if err != nil {
		return Instructions{}, c.wrap(err)
	}
	return instructionsFromProto(res.Msg.GetInstructions()), nil
}

// SetAccountInstructions writes them, and returns them as they now stand.
// Text that is blank takes them away.
func (c *Client) SetAccountInstructions(ctx context.Context, name, text string) (Instructions, error) {
	res, err := c.accounts.SetAccountInstructions(ctx, connect.NewRequest(&codingowlv1.SetAccountInstructionsRequest{
		Name: name, Text: text,
	}))
	if err != nil {
		return Instructions{}, c.wrap(err)
	}
	return instructionsFromProto(res.Msg.GetInstructions()), nil
}

func instructionsFromProto(in *codingowlv1.Instructions) Instructions {
	return Instructions{
		Account: in.GetAccount(),
		Driver:  in.GetDriver(),
		File:    in.GetFile(),
		Path:    in.GetPath(),
		Text:    in.GetText(),
	}
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
