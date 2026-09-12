package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/client"
)

// An Account is something to report even on a machine with no work at all: a
// person who set a ceiling wants to see it before there is anything to hold to
// it (ADR-0020).
func TestStatusReportsAnAccountWhenThereIsNoWork(t *testing.T) {
	var out bytes.Buffer
	resets := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)

	printOverview(Env{Stdout: &out}, client.Overview{Accounts: []client.AccountCeiling{{
		Name: "work",
		Windows: []client.UsageWindow{
			{Name: "five-hour", Read: true, Utilization: 42, Ceiling: 60, Resets: resets},
			{Name: "weekly", Ceiling: 50},
		},
	}}})

	got := out.String()
	for _, want := range []string{"accounts:", "work", "42%", "60%", resets.Format(time.RFC3339)} {
		if !strings.Contains(got, want) {
			t.Errorf("status does not carry %q:\n%s", want, got)
		}
	}
	// A window nothing has been read about says so rather than reporting a
	// figure nobody gave it, and still shows the ceiling that is set on it.
	var weekly string
	for _, ln := range strings.Split(got, "\n") {
		if strings.Contains(ln, "weekly") {
			weekly = ln
		}
	}
	if weekly == "" {
		t.Fatalf("status does not report the weekly window:\n%s", got)
	}
	if !strings.Contains(weekly, "(none)") || !strings.Contains(weekly, "50%") {
		t.Errorf("the weekly row is %q, want no figure and the ceiling that is set", weekly)
	}
}
