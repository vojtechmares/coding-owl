package client_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// TestS4TheClientRefusesAnOverlongSocketPathBeforeDialing maps to scenario S4
// in tests/behavior/issue-64.md: a socket path a unix socket address cannot
// hold is refused as such by the one client everything talks to the daemon
// through, rather than reported as a daemon that is not running.
func TestS4TheClientRefusesAnOverlongSocketPathBeforeDialing(t *testing.T) {
	sock := "/tmp/" + strings.Repeat("a", 110) + "/owld.sock"
	c := client.New(sock)

	_, err := c.DaemonStatus(context.Background())

	if err == nil {
		t.Fatal("DaemonStatus = nil, want a refusal: the path cannot be dialed")
	}
	if !strings.Contains(err.Error(), "too long") || !strings.Contains(err.Error(), sock) {
		t.Errorf("error %q does not say the socket path is too long and name it", err)
	}
	if errors.Is(err, client.ErrDaemonNotRunning) {
		t.Errorf("error %q blames a daemon that is not running rather than the path", err)
	}
}
