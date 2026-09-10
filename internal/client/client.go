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
		return errors.New(cerr.Message())
	}
	return err
}
