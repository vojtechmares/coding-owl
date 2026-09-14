# Issue #57: A daemon under brew services cannot find claude

The Claude Code Driver finds the tool by name on the daemon's `PATH`. Reported
at commit `701b936`: a daemon kept running by `brew services` gets Homebrew's
fixed service `PATH`, which does not hold `~/.local/bin`, where Claude Code's
own installer puts `claude`. Every Run then fails at the lookup, and nothing
documents it or offers a way around it.

The fix is that the daemon's own file gains an optional `claudePath`, which the
Driver uses instead of looking on `PATH` when it is set; that the lookup also
tries `~/.local/bin` after `PATH` when it is not; that the lookup's refusal
names both `PATH` and the setting; that the formula's service `PATH` gains
`~/.local/bin`; and that the install docs describe the limitation and the
setting. Detecting the tool at install time is out of scope.

The driver and config scenarios exercise those packages' own APIs. The formula
scenario runs the release script the way the issue #10 scenarios do. The
end-to-end scenario drives the built `owl` binary against a daemon whose `PATH`
does not hold the stub agent.

## Scenarios

### S1 - a configured path is used without consulting PATH
Given `claudePath` naming an executable, and a `PATH` with no `claude` on it
When the Driver checks the tool and builds an Agent
Then both use the configured path

### S2 - with nothing configured, the tool is found under the home directory's .local/bin
Given no `claudePath`, a `PATH` with no `claude` on it, and a `claude` at `~/.local/bin/claude`
When the Driver checks the tool and builds an Agent
Then both use the one under `~/.local/bin`

### S3 - a tool that is nowhere is refused naming both places to put it
Given no `claudePath`, a `PATH` with no `claude` on it, and none under `~/.local/bin`
When the Driver checks the tool
Then it refuses with an error naming `PATH` and `claudePath`

### S4 - the daemon's file may name the tool's path
Given a daemon file with `claudePath: /opt/tools/claude`
When it is parsed
Then the configuration carries that path
And a daemon file whose `claudePath` is not an absolute path is refused naming the setting

### S5 - the formula's service PATH holds the home directory's .local/bin
Given the release script rendering the formula
When the formula is read
Then its service block sets a `PATH` that ends in `.local/bin` under the home directory, after the standard service path

### S6 - a daemon whose PATH lacks the tool runs a Job with claudePath set
Given a daemon whose `PATH` holds no `claude`, whose file sets `claudePath` to the stub agent, and a pending Job
When the Job is run
Then the Run succeeds and the Job ends in `review`
And the stub agent was invoked by the configured path

### S7 - the install docs describe the limitation and the setting
Given the README and the formula's caveats
When they are read
Then the README names `brew services`, `~/.local/bin` and `claudePath`
And the caveats name `claudePath`
