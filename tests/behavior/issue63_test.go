package behavior_test

// Behavior tests for issue #63. TestS<n> maps to scenario S<n> in
// tests/behavior/issue-63.md. They drive the built owl binary and read what
// the daemon logs to standard error.

import (
	"strings"
	"testing"
)

func TestS1OwlDaemonRunVerboseIsAcceptedAndLogsAtDebugLevel(t *testing.T) {
	l := newLayout(t)

	p := startDaemonWith(t, l, "--verbose")
	waitForSocket(t, l.socket())
	waitForLog(t, p, "socket="+l.socket())

	if !strings.Contains(p.out(), "level=DEBUG") {
		t.Errorf("owl daemon run --verbose logged nothing at debug level:\n%s", p.out())
	}
}

func TestS2WithoutVerboseTheDaemonLogsAtInfoLevel(t *testing.T) {
	l := newLayout(t)

	p := startDaemon(t, l)
	waitForSocket(t, l.socket())
	waitForLog(t, p, "socket="+l.socket())

	if !strings.Contains(p.out(), "level=INFO") {
		t.Errorf("owl daemon run logged nothing at info level:\n%s", p.out())
	}
	if strings.Contains(p.out(), "level=DEBUG") {
		t.Errorf("owl daemon run logged at debug level without being asked to:\n%s", p.out())
	}
}

func TestS3OwlDaemonRunHelpDescribesTheFlag(t *testing.T) {
	l := newLayout(t)

	help := mustOwl(t, l, "daemon", "run", "--help").stdout

	if !strings.Contains(help, "--verbose") {
		t.Fatalf("owl daemon run --help does not list --verbose:\n%s", help)
	}
	if !strings.Contains(strings.ToLower(help), "debug") {
		t.Errorf("owl daemon run --help does not say --verbose turns the log up to debug:\n%s", help)
	}
}
