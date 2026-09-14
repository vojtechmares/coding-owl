package behavior_test

// Behavior tests for issue #54. TestS5 and TestS6 map to scenarios S5 and S6
// in tests/behavior/issue-54.md; S1 to S4 live with the config package, whose
// own API they exercise. These drive the built owl binary and the daemon
// against files carrying a key they do not have.

import (
	"strings"
	"testing"
	"time"
)

func TestS5ProjectShowReportsAnUnknownKeyInTheProjectsFile(t *testing.T) {
	l := newLayout(t)
	daemonUp(t, l)
	r := newRepo(t, l, "api")
	r.commit(".coding-owl.yaml", "apiVersion: codingowl.dev/v1\nmaxParalellRuns: 2\n", "add config with a typo")
	addProject(t, l, r)

	res := runOwl(t, l, "project", "show", "api")

	if res.code == 0 {
		t.Fatalf("a project file with an unknown key was accepted:\n%s", res.stdout)
	}
	for _, want := range []string{"maxParalellRuns", ".coding-owl.yaml"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not name %q:\n%s", want, res.stderr)
		}
	}
}

func TestS6TheDaemonRefusesToStartOnAFileWithAKeyItDoesNotHave(t *testing.T) {
	l := newLayout(t)
	globalConfig(t, l, "apiVersion: codingowl.dev/v1\nchecks:\n  - name: tests\n    run: make test\n")

	p := startDaemon(t, l)

	if code := p.exit(t, 10*time.Second); code == 0 {
		t.Fatalf("the daemon started on a file with a checks block:\n%s", p.out())
	}
	for _, want := range []string{"checks", "config.yaml"} {
		if !strings.Contains(p.out(), want) {
			t.Errorf("the daemon's output does not name %q:\n%s", want, p.out())
		}
	}
}
