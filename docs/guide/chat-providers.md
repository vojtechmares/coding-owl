# Chat model providers

The desktop app carries a chat, and a model answers it. This page is about
where that model comes from: what a provider is here, how its key is
configured, and what each of the two Owl drives needs from you.

## What `owl providers` is for

Manage the model providers the desktop chat speaks to.

The chat lives in the desktop app and is answered by the daemon, which holds
the keys: they go to the OS keychain, and the database keeps only a reference
to them (ADR-0019, ADR-0022). The key is typed here rather than in the app, so
the app never holds one.

Owl speaks to anthropic and openrouter. Which models a provider offers is
Owl's to know for Anthropic and yours to say for OpenRouter, which carries
whatever your account has.

What the chat may do with a model is bounded: it reads what Owl knows -
Projects, Jobs, Runs, logs and diffs - and it never acts on its own. A command
it proposes runs only once you have agreed to it, from a fixed allowlist of
read-only programs, executed without a shell
([ADR-0022](../adr/0022-desktop-chat.md)).

## A provider is not an Account, and not a Driver

The three are easy to run together and mean different things
([CONTEXT.md](../../CONTEXT.md) defines the other two):

| | What it is | Configured with |
| --- | --- | --- |
| Model provider | Where the desktop chat's model comes from | `owl providers add` |
| Account | The subscription Owl runs an Agent as, with its own tool configuration and its own rate limits | `owl account add` |
| Driver | The plugin that knows how to operate one coding tool, such as Claude Code | Per Project, in its manifest |

An Account and a Driver are about work Owl does for you while you are away. A
model provider is only about the conversation you have with Owl afterwards:
nothing a Job runs on goes through one, and configuring one does not change
what an Agent runs as.

## Configuring a key

Configure a way to reach models.

The key is read from standard input rather than taken as a flag, so it is not
in the shell history or in what every process on the machine can see:

```
owl providers add <provider> --key-stdin < key.txt
```

It is put in the credential store; the database records only that it is there.
Configuring a provider that is already configured replaces it, which is how a
key is rotated.

The credential store is the OS keychain where there is one, and a file under
the data home readable only by its owner where there is not - a continuous
integration runner, say. The daemon's own configuration chooses, and says which
it used in its log at startup ([ADR-0019](../adr/0019-accounts.md)).

There is no other way in. No flag takes a key as an argument, and the desktop
app never asks for one, because it is not the process that would hold it.

## Anthropic

Owl knows Anthropic's model list itself, so there is no `--model` to name when
you configure anthropic:

```
owl providers add anthropic --key-stdin < key.txt
```

That offers the chat these models:

- `claude-opus-5`
- `claude-sonnet-5`
- `claude-haiku-4-5-20251001`

Requests go to `https://api.anthropic.com` unless `--base-url` says otherwise.

## OpenRouter

OpenRouter carries models from many vendors and speaks the OpenAI wire shape
for all of them, which is why there is no separate `openai` provider. Which of
them your key can reach is your own account's business rather than Owl's, so
`--model` is required for openrouter, once per model:

```
owl providers add openrouter --key-stdin \
  --model anthropic/claude-sonnet-4.5 \
  --model openai/gpt-5 \
  < key.txt
```

Requests go to `https://openrouter.ai/api/v1` unless `--base-url` says
otherwise. A base url has to be somewhere a request can be sent: http or https,
with a host, and without a query, a fragment, or a name and password - every
request puts a path after it, and a credential in a url would end up in the
database and in a listing, which is not where one lives.

## Listing and removing

```
owl providers list
```

reports each configured provider with the models it offers and where it is
reached, and nothing else: a key is never printed. With none configured it says
so, and how to configure one.

```
owl providers remove openrouter
```

stops the chat speaking to a provider and deletes its key from the credential
store along with it. Removing one that is not configured exits non-zero and
says so.

## See also

- [ADR-0019](../adr/0019-accounts.md) - Accounts, credentials, and where a
  secret goes when there is no keychain.
- [ADR-0022](../adr/0022-desktop-chat.md) - the desktop chat as a daemon
  service, and the rules its commands run under.
- [CONTEXT.md](../../CONTEXT.md) - the words Owl uses for the rest of it.
