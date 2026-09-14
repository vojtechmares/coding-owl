# Issue #52: The daemon's own credentials leak into Agents of every Account

An Agent inherits the daemon's whole environment, and an Account adds only its
own configuration directory and, when it has one, its token. Reported at
commit `701b936`: an `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or
`CLAUDE_CODE_OAUTH_TOKEN` in the daemon's own environment reaches every Agent
whatever its Account - Claude Code prefers an API key over a token - and an
Account with no token of its own runs on the daemon's. That contradicts the
isolation an Account promises (ADR-0019).

The fix is that an invocation can name variables the executor takes out of the
inherited environment before adding the invocation's own, and that the Claude
Code Driver names those three. Nothing else in the environment is touched (out
of scope).

The driver and executor scenarios exercise those packages' own APIs. The
end-to-end scenario drives the built `owl` binary against a daemon started
with credentials in its environment, with the stub agent of issue #5 standing
in for Claude Code and reporting the environment it was given.

## Scenarios

### S1 - an Agent for an Account is given none of the daemon's credentials
Given `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN` set in the process building the invocation, and an Account with a token
When the Claude Code Driver builds the Agent's invocation
Then the invocation names all three variables as unset
And the only variables it adds are the Account's own `CLAUDE_CONFIG_DIR` and `CLAUDE_CODE_OAUTH_TOKEN`, the latter holding the Account's token

### S2 - an Account with no token runs with none, not the daemon's
Given the same variables set, and an Account whose token is empty
When the Driver builds the Agent's invocation
Then the invocation names all three variables as unset
And it adds `CLAUDE_CONFIG_DIR` and no `CLAUDE_CODE_OAUTH_TOKEN`

### S3 - the executor takes what an invocation unsets out of the Agent's environment
Given `ANTHROPIC_API_KEY` set in the daemon's own environment, and an invocation that unsets it and adds a variable of its own
When the host executor starts the Agent
Then the Agent sees `ANTHROPIC_API_KEY` empty
And the Agent sees the variable the invocation added

### S4 - a Run's Agent sees the Account's token and none of the daemon's credentials
Given a daemon started with `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN` in its environment, an Account with a token of its own, and a Project on that Account with a pending Job
When the Job is run
Then the Agent was given no `ANTHROPIC_API_KEY` and no `ANTHROPIC_AUTH_TOKEN`
And the `CLAUDE_CODE_OAUTH_TOKEN` it was given is the Account's token, not the daemon's

### S5 - the token setup flow runs without the daemon's credentials too
Given the same variables set
When the Driver builds the invocation that sets up an Account's token
Then the invocation names all three variables as unset
And it adds only the Account's `CLAUDE_CONFIG_DIR`
