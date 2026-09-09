# ADR-0002: The daemon never self-daemonizes

- **Status:** Accepted
- **Date:** 2026-09-09

## Context

The daemon has to run in the background on a laptop, surviving logout and
restarting after a crash. The traditional Unix answer is for the process to
fork, detach from the terminal, write a PID file, and reopen its own log files.

Every platform Coding Owl targets already has a supervisor that does this
better: launchd on macOS (reachable through `brew services`), systemd on Linux,
and the container runtime if it is ever run in one.

## Decision

`owl daemon run` runs in the **foreground** and logs to stdout/stderr. It never
forks, never detaches, never writes a PID file. Process supervision is entirely
the supervisor's job.

The Homebrew formula declares it:

```ruby
service do
  run [opt_bin/"owl", "daemon", "run"]
  keep_alive true
  log_path var/"log/coding-owl.log"
  error_log_path var/"log/coding-owl.log"
  environment_variables PATH: std_service_path_env
end
```

`owl daemon install` writes launchd/systemd units for people who did not
install via Homebrew. Templates live in `deploy/`.

## Consequences

- Debugging is just running it: `owl daemon run --verbose` in a terminal shows
  exactly what the supervised process does.
- It is containerizable with no PID-1 gymnastics.
- Restart-on-crash, log rotation, and start-at-login are configured in one
  place per platform, and it is a place users already understand.
- Anyone who wants a background process without a supervisor has to background
  it in their shell. That is an acceptable ask.

## Alternatives considered

**A `--daemon` flag that self-backgrounds.** Rejected. It reimplements process
supervision badly, hides logs behind a file the daemon has to manage itself,
and gives no answer for restart-after-crash - which is the actual requirement.
