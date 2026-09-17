# Issue #110: owl account exec runs an Account's coding tool against that Account

An Account's configuration directory is the coding tool's own configuration
directory (ADR-0019), so every command the tool has for writing user-scope
configuration - `mcp add`, `plugin install`, whatever it grows next - already
works against an Account. Nothing in Owl said so. A user who wanted an MCP
server or a plugin on an Account had to know the directory layout, export the
tool's variable themselves, and get the spelling right.

The change is `owl account exec <name> -- <args...>`. The Account's Driver
builds the tool's invocation, the way it already does for `claude setup-token`,
and Owl runs it at the user's own terminal against that Account's configuration
directory and credential. The arguments are the tool's and reach it untouched,
a leading dash among them included, and the tool's exit status is the command's.
Owl models none of what the tool writes there: the configuration directory is
the interface, and this only makes it reachable.

Owl stops reading options at the Account's name, so that a flag of the tool's
is never refused as an unknown flag of Owl's. That also means the `--` a user
writes is Owl's to take off rather than something the option parser has already
consumed, and a tool handed one as its own first argument would read it as the
end of its options: `-- --version` is not `--version`. Exactly one is taken
off, so two pass one through.

The scenarios drive the built `owl` binary against a running daemon, with the
stub agent of issue #5 standing in for Claude Code (see
`tests/behavior/issue-5.md`). The stub records the argv it was called with, the
directory it ran in, and every `CLAUDE_` and `ANTHROPIC_` variable it was given,
and it exits with the status `$OWL_FAKE_CLAUDE_EXIT` names, so a scenario can
see both what the tool was handed and what came back from it. No scenario runs
the real tool, and none spends a token.

## Scenarios

### S1 - the tool runs against the Account's own configuration directory, with the arguments it was given
Given a running daemon and an Account named `work`
When `owl account exec work -- mcp add --scope user sentry --transport http https://mcp.sentry.dev/mcp` runs
Then the tool was invoked with exactly those arguments, the dashed ones included, and nothing of Owl's added to them
And it was given `CLAUDE_CONFIG_DIR` holding that Account's configuration directory
And it ran in that directory, rather than in whatever directory the command was typed in

### S2 - what reaches the tool is exactly what the user meant, in every spelling
Given the same daemon and Account
When `owl account exec work -- --debug mcp list` runs
Then the tool was invoked with `--debug mcp list`: the `--` a user writes out of habit is Owl's terminator, and a tool handed `-- --debug` would read the end of its own options followed by a word rather than a flag
And when the command is written without a terminator at all, as `owl account exec work mcp add -s user sentry -t http https://mcp.sentry.dev/mcp`, it exits 0 rather than being refused for a flag that was never Owl's, and the tool is invoked with exactly those arguments
And when it is written with two, as `owl account exec work -- -- literal`, the tool is invoked with `-- literal`: exactly one is taken off, so a user who means the tool to see a terminator writes two

### S3 - the tool runs on the Account's credential and none of the user's own
Given the same daemon and Account, and a shell carrying `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN` of the user's own
When `owl account exec work -- mcp list` runs
Then the tool was given no `ANTHROPIC_API_KEY` and no `ANTHROPIC_AUTH_TOKEN`
And the `CLAUDE_CODE_OAUTH_TOKEN` it was given is the Account's own token, not the one the shell carried

### S4 - the tool's exit status is Owl's own, and Owl says nothing over it
Given the same daemon and Account, and a tool that writes a line to standard error and exits 3
When `owl account exec work -- mcp list` runs
Then `owl` exits 3, rather than flattening it to its own 1
And standard error holds the line the tool wrote and nothing else: no `owl: ` line of Owl's own underneath it

### S5 - an Account nobody has is refused by name, and no tool is run
Given a running daemon with no Account named `nothing`
When `owl account exec nothing -- mcp list` runs
Then it exits non-zero and names the Account that is not there
And the tool was not run at all: a command that cannot say which Account it is for must not reach for one
