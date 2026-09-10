package client_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// TestReorderJobRefusesAPositionTooWideForTheWire pins the guard that stops a
// position being truncated onto the 32-bit field it travels in. It needs no
// daemon: the request never leaves the client.
func TestReorderJobRefusesAPositionTooWideForTheWire(t *testing.T) {
	c := client.New(filepath.Join(t.TempDir(), "owld.sock"))

	_, err := c.ReorderJob(context.Background(), 1, 1<<32+1)

	var status *client.StatusError
	if !errors.As(err, &status) {
		t.Fatalf("ReorderJob = %v, want a StatusError", err)
	}
	if status.Kind != client.KindInvalid {
		t.Errorf("kind = %v, want KindInvalid", status.Kind)
	}
	if !strings.Contains(err.Error(), "4294967297") {
		t.Errorf("error %q does not name the position that was asked for", err)
	}
	if errors.Is(err, client.ErrDaemonNotRunning) {
		t.Error("the request was sent to the daemon rather than refused here")
	}
}
