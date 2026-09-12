# Issue #20: Ceiling: utilization from the stream, per-Account windows, scheduling gate, mid-Run termination, status explains waits

All scenarios drive the built `owl` binary from the outside against a running
daemon, with the stub agent of issue #5 first on its `PATH` in place of Claude
Code, the stub machine of issue #19 saying somebody is at it, and a Project
whose configuration is committed on its base branch. The last one drives the
desktop app's Go side in-process, as `tests/behavior/issue-9.md` describes.

Owl never leaves the user without headroom (ADR-0020). Utilization is
account-wide - it already counts what the user spent themselves - and it
arrives in a Run's own output stream, so Owl learns it by running rather than
by asking. An Account's ceiling is configured per window, and Owl neither
starts a Run on an Account above it nor lets one carry on past it.

The stream carries the figures as a line of its own, which the Driver reads and
nothing outside the Driver names:

```json
{"type":"system","subtype":"usage_limits","limits":{
  "five_hour":       {"utilization": 42, "resets_at": "2031-01-01T00:00:00Z"},
  "seven_day":       {"utilization": 10, "resets_at": "2031-01-08T00:00:00Z"},
  "seven_day_opus":  {"utilization": 80, "resets_at": "2031-01-08T00:00:00Z"}}}
```

`utilization` is a percentage of the window, and `resets_at` is when the window
starts again. The ceilings are percentages too:

```yaml
accounts:
  work:
    limits:
      fiveHourMax: 60
      weeklyMax: 50
```

"The weekly window" below means the most utilized of the seven-day windows,
which is what a weekly ceiling is compared against (ADR-0020).

One thing a scenario cannot drive: a Driver that does not report usage. The
daemon has one Driver and it reports usage, so an Account under a ceiling on a
tool that cannot keep one is refused by a rule these scenarios cannot reach,
and `internal/run` tests it directly instead.

## Scenarios

### S1 - what a Run reports is kept per Account and window
Given an Account, and a Job whose Agent reports the five-hour window at 42% and the seven-day one at 10%
When the Run finishes
Then `owl status` reports that Account at 42% of its five-hour window and 10% of its weekly one, with the times they reset

### S2 - an Account above its five-hour ceiling is not scheduled
Given an Account whose five-hour ceiling is 60%, and a Run that reported 75%
When `owl start` is called with a Job queued on that Account
Then it is refused, naming the Account, what it is at, its ceiling, and when the window resets
And the Job is still pending, with nothing held against it

### S3 - the weekly ceiling is compared against the most utilized seven-day window
Given an Account whose weekly ceiling is 50%, and a Run that reported `seven_day` at 10% and `seven_day_opus` at 80%
When `owl start` is called with a Job queued on that Account
Then it is refused, naming 80% rather than 10%

### S4 - a reading whose window has already reset is discarded
Given an Account over its ceiling on a reading that resets in the past
When `owl start` is called
Then the Run starts, because what Owl knew is no longer about this window
And `owl status` no longer reports the window it was about

### S5 - a Run that crosses the ceiling mid-flight is ended
Given an Account whose five-hour ceiling is 60%, and a Run in flight whose Agent then reports 75%
When the daemon reads that line
Then the Agent and everything it started are stopped
And the Run ends `interrupted`, saying the ceiling was crossed, and its Job is pending again
And the Job's branch and worktree are still there

### S6 - the Job it was ended for does not start again until the window resets
Given the Job of S5, pending again on an Account over its ceiling
When `owl start` is called
Then it is refused, naming when the window resets

### S7 - an Account with no ceiling runs whatever the figures say
Given an Account with no `limits` configured, and a Run that reported 95% of its five-hour window
When `owl start` is called with another Job queued on that Account
Then the Run starts, because nobody asked Owl to stand down

### S8 - figures below the ceiling stop nothing
Given an Account whose five-hour ceiling is 60%, and a Run that reported 42%
When `owl start` is called with another Job queued on that Account
Then the Run starts

### S9 - `owl status` shows each Account against its ceilings
Given two Accounts, one over its ceiling and one under
When `owl status` is run
Then it reports both, each window's utilization against its ceiling and when it resets
And it says which Account is waiting and until when

### S10 - a ceiling that is not a percentage is refused
Given global configuration whose `accounts` block sets a ceiling of `soon`, or of `200`
When the daemon reads it
Then it refuses to start, naming the file and the setting

### S11 - the app shows each Account against its ceilings
Given the desktop app on a daemon with an Account over its ceiling
When the app is asked for the overview
Then it carries that Account's windows, their utilization, their ceilings and their reset times
And the frontend sources render them in the window
