package behavior_test

// Behavior tests for issue #110. Each TestS<n> maps to scenario S<n> in
// tests/behavior/issue-110.md. They drive the built owl binary against a daemon
// whose PATH puts the stub agent of issue #5 where Claude Code would be, and
// read back what that stub was invoked with: `owl account exec` runs the tool
// at the user's own terminal, so what reaches the tool is the whole of the
// behavior.

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// execAccount is a layout with a daemon up and one Account to run the tool
// against, which every scenario here needs before it can run anything.
func execAccount(t *testing.T) (*layout, *stub) {
	t.Helper()
	l, s := accountLayout(t)
	// The user's own credentials are in the shell every one of these commands
	// is typed in, because that is where a person types them (issue #52).
	l = l.withEnv(usersOwnCredentials...)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	return l, s
}

// toolArgs is what the stub tool was invoked with, without the path it was
// found at: what the user typed after the Account's name is what should be
// left.
func toolArgs(t *testing.T, inv invocation) []string {
	t.Helper()
	if len(inv.Argv) == 0 {
		t.Fatal("the stub tool recorded no arguments at all, not even its own name")
	}
	return inv.Argv[1:]
}

// mcpAdd is a command a user really types against an Account: it is the
// example in the issue, and it carries flags of the tool's own in both
// spellings a tool takes them in.
var mcpAdd = []string{"mcp", "add", "--scope", "user", "sentry", "--transport", "http", "https://mcp.sentry.dev/mcp"}

func TestS1AccountExecRunsTheToolAgainstTheAccountsOwnDirectory(t *testing.T) {
	l, s := execAccount(t)

	mustOwl(t, l, append([]string{"account", "exec", "work", "--"}, mcpAdd...)...)

	inv := s.invoked(t)
	if got := toolArgs(t, inv); !slices.Equal(got, mcpAdd) {
		t.Errorf("the tool was invoked with %v, want the arguments it was given, untouched: %v", got, mcpAdd)
	}
	dir := accountDir(l, "work")
	if got := inv.Env["CLAUDE_CONFIG_DIR"]; got != dir {
		t.Errorf("the tool ran with CLAUDE_CONFIG_DIR=%q, want the account's own directory %q", got, dir)
	}
	// Where it ran matters as well as what it was told: a tool writes beside
	// itself, and the directory the user happened to be in is not the
	// account's.
	if !samePath(inv.Dir, dir) {
		t.Errorf("the tool ran in %q, want the account's own directory %q", inv.Dir, dir)
	}
}

func TestS2WhatReachesTheToolIsExactlyWhatTheUserMeant(t *testing.T) {
	l, s := execAccount(t)
	spellings := []struct {
		why   string
		typed []string
		want  []string
	}{
		{
			// The terminator is Owl's rather than the tool's: a user writes
			// it out of habit, and a tool handed `-- --debug` reads the end
			// of its own options and then a word, not `--debug`.
			why:   "a dashed argument after the terminator",
			typed: []string{"--", "--debug", "mcp", "list"},
			want:  []string{"--debug", "mcp", "list"},
		},
		{
			// The spelling that stopping at the Account's name is there for:
			// `-s user` is the tool's flag and Owl never reads it, so there
			// is nothing to refuse.
			why:   "no terminator at all",
			typed: []string{"mcp", "add", "-s", "user", "sentry", "-t", "http", "https://mcp.sentry.dev/mcp"},
			want:  []string{"mcp", "add", "-s", "user", "sentry", "-t", "http", "https://mcp.sentry.dev/mcp"},
		},
		{
			// Exactly one is taken off, so somebody who means the tool to see
			// a terminator writes two and the tool sees one.
			why:   "a terminator the user means the tool to see",
			typed: []string{"--", "--", "literal"},
			want:  []string{"--", "literal"},
		},
	}

	for _, c := range spellings {
		res := runOwl(t, l, append([]string{"account", "exec", "work"}, c.typed...)...)
		if res.code != 0 {
			t.Fatalf("owl account exec %v exited %d; an argument of the tool's was read as Owl's?\nstdout:\n%s\nstderr:\n%s",
				c.typed, res.code, res.stdout, res.stderr)
		}
	}

	all := s.invocations(t)
	if len(all) != len(spellings) {
		t.Fatalf("the stub tool was invoked %d times, want one per spelling (%d)", len(all), len(spellings))
	}
	for i, c := range spellings {
		if got := toolArgs(t, all[i]); !slices.Equal(got, c.want) {
			t.Errorf("with %s, the tool was invoked with %v, want %v", c.why, got, c.want)
		}
	}
}

func TestS3AccountExecRunsOnTheAccountsCredentialAndNoneOfTheUsersOwn(t *testing.T) {
	l, s := execAccount(t)

	mustOwl(t, l, "account", "exec", "work", "--", "mcp", "list")

	inv := s.invoked(t)
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if got, ok := inv.Env[name]; ok {
			t.Errorf("the tool ran with %s=%q, want the user's own credential kept from it", name, got)
		}
	}
	if got := inv.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != testToken {
		t.Errorf("the tool ran with CLAUDE_CODE_OAUTH_TOKEN=%q, want the account's own token", got)
	}
}

func TestS4TheToolsExitStatusIsOwlsOwn(t *testing.T) {
	l, _ := execAccount(t)
	// A tool that fails says why itself, on the terminal it was given; the
	// status is what the shell reads afterwards. The daemon is already up on
	// the environment it started with, so this is the tool `owl` itself runs.
	const complaint = "no mcp servers configured for this account"
	l = l.withEnv("OWL_FAKE_CLAUDE_EXIT=3", "OWL_FAKE_CLAUDE_STDERR="+complaint)

	res := runOwl(t, l, "account", "exec", "work", "--", "mcp", "list")

	if res.code != 3 {
		t.Errorf("owl account exec exited %d for a tool that exited 3, want the tool's own status", res.code)
	}
	if got := strings.TrimSpace(res.stderr); got != complaint {
		t.Errorf("stderr = %q, want the tool's own line and nothing else", got)
	}
	if strings.Contains(res.stderr, "owl:") {
		t.Errorf("owl talked over the tool it ran:\n%s", res.stderr)
	}
}

func TestS5AccountExecRefusesAnAccountThatIsNotThere(t *testing.T) {
	l, s := accountLayout(t)
	daemonUp(t, l)

	res := runOwl(t, l, "account", "exec", "nothing", "--", "mcp", "list")

	if res.code == 0 {
		t.Fatalf("running the tool for an account that is not there exited 0\nstdout:\n%s", res.stdout)
	}
	if !strings.Contains(res.stderr, "nothing") {
		t.Errorf("stderr does not name the account:\n%s", res.stderr)
	}
	if _, err := os.Stat(s.argv); err == nil {
		t.Errorf("the tool was run for an account that is not there:\n%+v", s.invocations(t))
	}
}
