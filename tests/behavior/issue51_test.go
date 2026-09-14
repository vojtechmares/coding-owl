package behavior_test

// Behavior tests for issue #51. TestS<n> maps to scenario S<n> in
// tests/behavior/issue-51.md; S4 lives with the daemon's own error mapping.
// These drive the built owl binary and the desktop app against a daemon with
// a chat exchange parked - on consent, or on a provider that does not answer -
// and then tell the daemon to stop.

import (
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vojtechmares/coding-owl/internal/desktop"
)

// shutdownDeadline is how long the daemon gives its server to shut down, and
// shutdownMargin is what a scenario allows on top of it for the exit to be
// seen.
const (
	shutdownDeadline = 5 * time.Second
	shutdownMargin   = 3 * time.Second
)

// parked is a daemon with one chat exchange in progress that cannot finish on
// its own, and the app whose events say what became of it.
type parked struct {
	d  *daemonProc
	ev *events
}

// exchanging brings up a daemon with a Project and a fake provider answering
// with those replies, and starts an exchange in the app.
func exchanging(t *testing.T, provider func(*testing.T, *fakeProvider), replies ...string) (*parked, *fakeProvider) {
	t.Helper()
	l := newLayout(t)
	globalConfig(t, l, fileStore)
	d := daemonUp(t, l)
	p := newFakeProvider(t, replies...)
	if provider != nil {
		provider(t, p)
	}
	addProvider(t, l, "anthropic", "sk-ant-test", p.server.URL)
	r := newRepo(t, l, "api")
	addProject(t, l, r)
	// The directory the model asks for is the Project's own.
	for i := range p.replies {
		p.replies[i] = strings.ReplaceAll(p.replies[i], "PROJECT_DIR", r.dir)
	}
	app, ev := desktopApp(t, l)
	if _, err := app.Send(0, anthropicModel(t, app), "what is uncommitted"); err != nil {
		t.Fatalf("the app could not send a message: %v", err)
	}
	return &parked{d: d, ev: ev}, p
}

// parkedOnConsent is an exchange whose model has asked to run a command and
// nobody has answered.
func parkedOnConsent(t *testing.T) *parked {
	t.Helper()
	x, _ := exchanging(t, nil, asksToRun("call-1", "git status --short", "PROJECT_DIR"))
	for {
		e := x.ev.next(t)
		switch e.name {
		case desktop.EventChatCommand:
			return x
		case desktop.EventChatEnd:
			t.Fatalf("the exchange ended before the model asked to run anything: %+v", e.data)
		}
	}
}

// parkedOnProvider is an exchange whose provider has been asked and has not
// answered.
func parkedOnProvider(t *testing.T) *parked {
	t.Helper()
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold) })
	x, p := exchanging(t, func(_ *testing.T, p *fakeProvider) { p.hold = hold }, anthropicText("never sent"))
	p.askedTimes(t, 1)
	return x
}

// stop tells the daemon to stop and returns how it exited, failing if it did
// not exit within the shutdown deadline plus a margin.
func (x *parked) stop(t *testing.T) int {
	t.Helper()
	if err := x.d.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	return x.d.exit(t, shutdownDeadline+shutdownMargin)
}

// ended waits for the app to report the end of the answer and returns the
// error it reported.
func (x *parked) ended(t *testing.T) string {
	t.Helper()
	for {
		e := x.ev.next(t)
		if e.name != desktop.EventChatEnd {
			continue
		}
		end, ok := e.data.(desktop.ChatEnd)
		if !ok {
			t.Fatalf("a chat end carried %T", e.data)
		}
		return end.Error
	}
}

// cancelled checks that an error says the exchange was cancelled.
func cancelled(t *testing.T, what, err string) {
	t.Helper()
	if err == "" {
		t.Fatalf("%s ended with no error, want it told the exchange was cancelled", what)
	}
	if !strings.Contains(strings.ToLower(err), "cancel") {
		t.Errorf("%s ended with %q, want an error that says it was cancelled", what, err)
	}
}

func TestS1DaemonStopsCleanlyWhileAnExchangeIsParkedOnConsent(t *testing.T) {
	x := parkedOnConsent(t)

	if code := x.stop(t); code != 0 {
		t.Errorf("daemon exited %d, want 0:\n%s", code, x.d.out())
	}
}

func TestS2ClientParkedOnConsentIsToldTheExchangeWasCancelled(t *testing.T) {
	x := parkedOnConsent(t)

	x.stop(t)

	cancelled(t, "the answer", x.ended(t))
}

func TestS3DaemonStopsCleanlyWhileAnExchangeIsParkedOnTheProvider(t *testing.T) {
	x := parkedOnProvider(t)

	if code := x.stop(t); code != 0 {
		t.Errorf("daemon exited %d, want 0:\n%s", code, x.d.out())
	}
	cancelled(t, "the answer", x.ended(t))
}
