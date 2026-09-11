// Package client is the one way clients talk to the daemon (ADR-0004). The
// CLI and the desktop app both use it, so they cannot diverge.
package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"connectrpc.com/connect"

	codingowlv1 "github.com/vojtechmares/coding-owl/gen/codingowl/v1"
	"github.com/vojtechmares/coding-owl/gen/codingowl/v1/codingowlv1connect"
)

// ErrDaemonNotRunning is wrapped into errors returned when nothing answers on
// the socket. Check with errors.Is.
var ErrDaemonNotRunning = errors.New("daemon not running")

// Kind is what the daemon said went wrong, without exposing the transport.
type Kind int

const (
	// KindUnknown is anything the daemon did not classify.
	KindUnknown Kind = iota
	// KindNotFound means the thing asked for does not exist.
	KindNotFound
	// KindAlreadyExists means the name asked for is taken.
	KindAlreadyExists
	// KindInvalid means the request itself was not usable.
	KindInvalid
	// KindInternal means Owl failed, rather than the caller.
	KindInternal
)

// StatusError is a failure the daemon reported, carrying both the message it
// wrote and how it classified it.
type StatusError struct {
	Kind    Kind
	Message string
}

func (e *StatusError) Error() string { return e.Message }

// DaemonStatus is what the daemon reports about itself.
type DaemonStatus struct {
	Version    string
	Uptime     time.Duration
	SocketPath string
}

// Client talks to one daemon over its unix socket.
type Client struct {
	socket   string
	daemon   codingowlv1connect.DaemonServiceClient
	projects codingowlv1connect.ProjectServiceClient
	jobs     codingowlv1connect.JobServiceClient
	accounts codingowlv1connect.AccountServiceClient
	gc       codingowlv1connect.GarbageCollectionServiceClient
	skills   codingowlv1connect.SkillServiceClient
}

// New returns a client for the daemon listening on socketPath. It does not
// connect until a call is made.
func New(socketPath string) *Client {
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	// The host is a placeholder: every request goes to the socket.
	return &Client{
		socket:   socketPath,
		daemon:   codingowlv1connect.NewDaemonServiceClient(httpClient, "http://owl"),
		projects: codingowlv1connect.NewProjectServiceClient(httpClient, "http://owl"),
		jobs:     codingowlv1connect.NewJobServiceClient(httpClient, "http://owl"),
		accounts: codingowlv1connect.NewAccountServiceClient(httpClient, "http://owl"),
		gc:       codingowlv1connect.NewGarbageCollectionServiceClient(httpClient, "http://owl"),
		skills:   codingowlv1connect.NewSkillServiceClient(httpClient, "http://owl"),
	}
}

// DaemonStatus asks the daemon for its version, uptime and socket path.
func (c *Client) DaemonStatus(ctx context.Context) (*DaemonStatus, error) {
	res, err := c.daemon.GetStatus(ctx, connect.NewRequest(&codingowlv1.GetStatusRequest{}))
	if err != nil {
		return nil, c.wrap(err)
	}
	return &DaemonStatus{
		Version:    res.Msg.GetVersion(),
		Uptime:     res.Msg.GetUptime().AsDuration(),
		SocketPath: res.Msg.GetSocketPath(),
	}, nil
}

// wrap turns a failed dial into ErrDaemonNotRunning, naming the socket, and
// strips the Connect envelope off everything else so the CLI prints the
// message the daemon wrote rather than a code-prefixed version of it.
func (c *Client) wrap(err error) error {
	if err == nil {
		return nil
	}
	if connect.CodeOf(err) == connect.CodeUnavailable ||
		errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("%w at %s: %w", ErrDaemonNotRunning, c.socket, errors.Unwrap(err))
	}
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return &StatusError{Kind: kindOf(cerr.Code()), Message: cerr.Message()}
	}
	return err
}

// kindOf translates the Connect code into Owl's own vocabulary, so callers do
// not have to know the transport to tell the cases apart.
func kindOf(code connect.Code) Kind {
	switch code {
	case connect.CodeNotFound:
		return KindNotFound
	case connect.CodeAlreadyExists:
		return KindAlreadyExists
	case connect.CodeInvalidArgument:
		return KindInvalid
	case connect.CodeInternal:
		return KindInternal
	default:
		return KindUnknown
	}
}
