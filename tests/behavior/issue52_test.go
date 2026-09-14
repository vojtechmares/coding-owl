package behavior_test

// Behavior tests for issue #52. TestS4 maps to scenario S4 in
// tests/behavior/issue-52.md; S1, S2, S3 and S5 live with the Claude Code
// Driver and the host executor, whose own APIs they exercise. This one drives
// the built owl binary against a daemon started with credentials of its own in
// its environment, and reads back what the stub agent was given.

import (
	"testing"
)

func TestS4ARunsAgentSeesTheAccountsTokenAndNoneOfTheDaemonsCredentials(t *testing.T) {
	l, s := accountLayout(t)
	// A daemon started from a shell that has the user's own credentials in
	// it, each of which Claude Code would use in place of the Account's.
	l = l.withEnv(
		"ANTHROPIC_API_KEY=the-daemons-own-api-key",
		"ANTHROPIC_AUTH_TOKEN=the-daemons-own-auth-token",
		"CLAUDE_CODE_OAUTH_TOKEN=the-daemons-own-oauth-token",
	)
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
