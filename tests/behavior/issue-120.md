# Issue #120: owl providers supported names the providers Owl drives

`owl providers list` reports what is already **configured**, so it is empty for
exactly the person who needs the answer: a first-time user has no way to learn
that `anthropic` and `openrouter` are the names `owl providers add <provider>`
takes, short of reading the `providers` help prose, which is
discoverable-by-accident rather than a discovery command.

The change is `owl providers supported`. It prints the providers Owl drives -
`chat.Providers` - with a note on each saying how its models are decided:
Anthropic's are Owl's own, so `--model` is not needed; OpenRouter's are
whatever the user's account has, so `--model` is required. It prints a
compiled-in list rather than asking the daemon, because the whole point is the
user who has configured nothing and may not have `owl daemon run` up yet.

The scenarios drive the built `owl` binary from the outside, in the XDG layout
`tests/behavior/issue-5.md` and `tests/behavior/issue-14.md` describe. Only S5
and S6 need a daemon, and they need one to show that what is configured makes
no difference to what is supported. No scenario reaches the network: a provider
is configured without one being contacted, as
`tests/behavior/issue-18.md` S2 already does.

## Scenarios

### S1 - the names are there before anything is configured, and with no daemon
Given the built binary, no daemon started and no provider configured
When `owl providers supported` runs
Then it exits 0 and names both `anthropic` and `openrouter`
And it says nothing about a socket, or a daemon it could not reach: it answered
    without one
And `owl providers list` in the same layout cannot answer at all, which is the
    gap this command fills

### S2 - each provider says how its models are decided
Given the output of S1
When the line for each provider is read
Then the `anthropic` line says its models are Owl's own and that `--model` is
    not needed
And the `openrouter` line says the models are the account's own and names
    `--model` as how they are given

### S3 - it says how to configure one
Given the output of S1
When it is read to the end
Then it names `owl providers add` and `--key-stdin`, so the command that
    answers "which names are there" leads to the one that uses them

### S4 - it is discoverable, and takes no arguments
Given the built binary
When `owl providers --help` runs
Then it lists `supported` among the commands, beside `add`, `list` and `remove`
And `owl providers supported nonsense` exits non-zero rather than ignoring the
    word

### S5 - what it lists is what `owl providers add` accepts
Given a running daemon
When each name `owl providers supported` printed is passed to
    `owl providers add <name> --key-stdin`
Then none of them is refused as a provider Owl does not drive
    (a refusal for a missing `--model` is S2's point, and is fine)
And a name it did not print, `openai`, is refused as one Owl does not drive

### S6 - what is configured does not change what is supported
Given a running daemon with `anthropic` configured and `openrouter` not
When `owl providers supported` runs
Then it still lists both
And its output is byte-identical to what it printed with nothing configured at
    all: this command reports what Owl drives, never what the user has
