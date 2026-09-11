# Issue #18: Chat, read-only: ChatService, Anthropic and OpenRouter, persisted conversations, Owl-state tools, desktop chat

Most scenarios drive the built `owl` binary from the outside against a running
daemon; the ones about the chat itself drive the desktop app's Go side in
process, as `tests/behavior/issue-9.md` describes, because the chat has no CLI
surface and that is the point of S4. The XDG layout, the temporary repository
and the harness Account are as `tests/behavior/issue-5.md` and
`tests/behavior/issue-14.md` describe them.

The chat is a daemon service the desktop app talks to (ADR-0022). The daemon
holds the model credentials, keeps the conversations in `owl.db`, and answers
questions about what Owl did from its own state. It never acts: the tools it
offers a model only read.

"A model provider" means a configured way to reach models: `anthropic`, or
`openrouter` with the models it is to offer. "The fake provider" means an HTTP
server the scenario runs itself, speaking the provider's own event stream, which
a provider is pointed at with `--base-url`. No scenario reaches the network.

## Scenarios

### S1 - a provider is configured from the CLI, and its key is not in the database
Given a running daemon
When `owl providers add anthropic --key sk-ant-test --base-url <fake>` runs
Then it exits 0 and says the provider was added
And `owl providers list` reports it with the models it offers
And the key is nowhere in `owl.db`

### S2 - an OpenRouter provider carries the models it is to offer
Given a running daemon
When `owl providers add openrouter --key sk-or-test --model anthropic/claude-sonnet-4.5 --model openai/gpt-5` runs
Then `owl providers list` reports both models against `openrouter`
And a provider added with no model at all is refused, naming the flag

### S3 - owl providers remove takes the key with it
Given a configured provider
When `owl providers remove anthropic` runs
Then `owl providers list` no longer reports it
And the credential store no longer holds its secret
And removing one that is not there exits non-zero and names it

### S4 - the CLI has no chat
Given the built binary
When `owl --help` runs, and `owl chat` runs
Then the help lists no chat command
And `owl chat` exits non-zero, saying it is not a command Owl has

### S5 - the app lists the models of every configured provider
Given a running daemon with `anthropic` and `openrouter` configured
When the app is asked for the models it can use
Then it reports the models of both, each with the provider it comes from

### S6 - a message streams back through the daemon
Given a configured provider whose fake answers in three pieces
When the app sends a message on a new conversation
Then it emits those pieces in order as they arrive
And says the answer has ended
And the answer it assembles is what the provider sent

### S7 - a conversation is there after the app restarts
Given the conversation of S6
When a new app is made for the same daemon and asked for that conversation
Then it carries the message that was sent and the answer that came back
And `owl.db` holds them, so nothing was kept in the app

### S8 - the app lists conversations, most recently spoken to first
Given two conversations, the older one spoken to last
When the app lists conversations
Then the older one is first, and each carries what it is about

### S9 - the model is told what tools it may use, and they only read
Given a configured provider
When the app sends a message
Then the request the provider received offers tools for projects, jobs, runs, logs and diffs
And no tool that writes, runs a command, or changes anything Owl holds

### S10 - a question about Owl's state is answered from Owl's state
Given a Project, a Job that ran and was blocked by a check, and a fake provider that asks for the job
When the app sends "why was job 1 blocked"
Then the daemon answers the model's tool call from its own state, with the check that failed and what it printed
And the model's next answer streams back

### S11 - a tool call for a Run's log is answered, bounded
Given the Job of S10 and a fake provider that asks for its Run's log
When the app sends a message
Then the tool result carries the log's last lines, and no more than the daemon will carry

### S12 - a tool call for what a Job changed is answered with the diff
Given a Job whose Agent committed a file, and a fake provider that asks for its diff
When the app sends a message
Then the tool result names the file and carries the line it added

### S13 - a tool the daemon does not have is refused, and the model is told
Given a fake provider that asks for a tool nobody offered
When the app sends a message
Then the daemon tells the model that tool is not one it has, rather than failing the stream
And nothing about Owl's state changed

### S14 - a provider that fails mid-answer ends the stream, and what arrived is kept
Given a fake provider that sends one piece and then breaks the connection
When the app sends a message
Then the app is told the answer ended with an error naming the provider
And the conversation keeps the piece that arrived

### S15 - the app never holds a provider key
Given a configured provider
When the app is asked for the models it can use, for its conversations, and for one conversation
Then nothing it is given carries the key
And the daemon refuses a message for a provider that is not configured, saying so
