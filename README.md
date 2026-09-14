# Coding Owl

## Installing

Owl ships as a Homebrew formula. Install it and keep the daemon running across
logins:

```
brew install vojtechmares/tap/coding-owl
brew services start coding-owl
```

The daemon runs Claude Code on your behalf, so `claude` has to be somewhere the
daemon can find it. A daemon started from your shell has your `PATH`. A daemon
kept running by `brew services` does not: it gets a fixed `PATH` of Homebrew's
own directories plus `~/.local/bin`, which is where Claude Code's installer
puts `claude`. If yours is somewhere else, name it in the daemon's own file,
`config.yaml` in Owl's configuration directory:

```yaml
apiVersion: codingowl.dev/v1
claudePath: /path/to/claude
```

When `claudePath` is set the daemon uses it and does not look on `PATH`. When
it is not, the daemon looks on `PATH` and then under `~/.local/bin`.
