# ADR-0022: Desktop chat is a daemon service with consent-gated, shell-free commands

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

The desktop app gains a chat, backed by an Anthropic API key or by OpenRouter
for any model it offers. Overlapping model access through OpenRouter is accepted
and not a problem to solve.

A chat that can only talk is much less useful here than one that can look at the
repository the user is asking about. But letting a model run commands on a
developer machine is the part that has to be got right rather than iterated on.

The CLI gets no chat or interactive mode in the MVP.

## Decision

**ChatService lives in the daemon**, exposed as a streaming RPC that only the
desktop app consumes for now. The daemon holds the model credentials and
persists conversations in `owl.db`.

**Nothing happens without consent.** The chat never acts autonomously. Each
action is proposed and confirmed in the Claude Desktop / ChatGPT style, with a
"remember for this conversation" grant.

**Commands are executed by the daemon**, never by asking an Agent to run them.
`claude -p "run: git status"` is explicitly not how this works.

**There is no shell.** The daemon parses the command into argv itself and calls
`execve` directly:

- `argv[0]` must be on the allowlist - `git`, `cat`, `grep`, `ls`, `head`,
  `tail`, `wc`
- arguments are validated per command - `git` permits `status`, `log`, `diff`,
  `show`, `branch`, and denies `checkout`, `reset`, `clean`, `push`
- anything unmatched is denied

Commands run with the working directory set to a Project or one of its
worktrees, chosen explicitly, never an arbitrary path.

## Consequences

- Shell metacharacters need no detection, because nothing interprets them. `;`,
  `&&`, `>` and backticks arrive as literal arguments and fail argument
  validation. The gate fails **closed**: an unanticipated case is denied.
- ADR-0009's pure-view rule survives. The desktop app still performs no action
  that is not a daemon RPC; the CLI simply does not surface this one yet.
- Model credentials live in one process alongside Account credentials
  (ADR-0019), rather than being split between daemon and app.
- The allowlist is a maintenance surface, and it will feel restrictive early.
  Widening it is a deliberate act, which is the intended asymmetry.
- The daemon gains an LLM client and model-provider configuration, making it
  more than a scheduler. That is a real increase in its surface area.
- Conversations in `owl.db` mean chat history survives app restarts and is
  available to any future client.

## Alternatives considered

**An operand denylist, then a shell.** Reject strings containing `&&`, `>`,
`;`, `|` and friends, and shell out otherwise. Simple, and preserves globbing.
Rejected because denylists fail open - one missed construct, or one destructive
flag on an allowlisted binary such as `git reset --hard`, and it has passed
something it should not have.

**Fixed command templates only**, with typed holes and no free-form input. No
injection surface at all. Rejected as too limiting: the chat could not grep for
an arbitrary pattern or read an unanticipated file, which is most of the value.

**An app-local chat** talking to providers directly. Cheapest by far, and needs
no daemon work. Rejected because it breaks the pure-view rule, splits
credentials across two processes, and cannot see anything Owl did.
