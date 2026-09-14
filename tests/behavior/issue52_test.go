package behavior_test

// Behavior tests for issue #52. TestS4 and TestS6 map to scenarios S4 and S6
// in tests/behavior/issue-52.md; S1, S2, S3 and S5 live with the Claude Code
// Driver and the host executor, whose own APIs they exercise. These drive the
// built owl binary with credentials of the user's own in the environment, and
// read back what the stub agent was given.

import (
	"slices"
	"testing"
)

// usersOwnCredentials are what a shell with the user's own Claude Code setup
// in it carries, each of which the tool would use in place of an Account's.
var usersOwnCredentials = []string{
	"ANTHROPIC_API_KEY=the-daemons-own-api-key",
	"ANTHROPIC_AUTH_TOKEN=the-daemons-own-auth-token",
	"CLAUDE_CODE_OAUTH_TOKEN=the-daemons-own-oauth-token",
}

func TestS6AccountAddRunsTheTokenSetupWithoutTheUsersOwnCredentials(t *testing.T) {
	l, s := accountLayout(t)
	l = l.withEnv(usersOwnCredentials...)
	daemonUp(t, l)

	res := runOwlStdin(t, l, "pasted-token\n", "account", "add", "work")

	if res.code != 0 {
		t.Fatalf("owl account add exited %d\nstdout:\n%s\nstderr:\n%s", res.code, res.stdout, res.stderr)
	}
	inv := s.invoked(t)
	if !slices.Contains(inv.Argv, "setup-token") {
		t.Fatalf("the tool was invoked as %v, want its token setup", inv.Argv)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if got, ok := inv.Env[name]; ok {
			t.Errorf("the token setup ran with %s=%q, want the user's own credential kept from it", name, got)
		}
	}
	if got := inv.Env["CLAUDE_CONFIG_DIR"]; got != accountDir(l, "work") {
		t.Errorf("the token setup ran with CLAUDE_CONFIG_DIR=%q, want the account's own directory %q",
			got, accountDir(l, "work"))
	}
}

func TestS4ARunsAgentSeesTheAccountsTokenAndNoneOfTheDaemonsCredentials(t *testing.T) {
	l, s := accountLayout(t)
	// A daemon started from a shell that has the user's own credentials in
	// it.
	l = l.withEnv(usersOwnCredentials...)
	daemonUp(t, l)
	addAccount(t, l, "work", testToken)
	accountProject(t, l, "api", "apiVersion: codingowl.dev/v1\naccount: work\n")

	finishedJob(t, l)

	inv := s.invoked(t)
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if got, ok := inv.Env[name]; ok {
			t.Errorf("the agent ran with %s=%q, want the daemon's own credential kept from it", name, got)
		}
	}
	if got := inv.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != testToken {
		t.Errorf("the agent ran with CLAUDE_CODE_OAUTH_TOKEN=%q, want the account's own token", got)
	}
}
