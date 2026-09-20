# Issue #119: garbage collection reads its two settings again, rather than at startup

Most of the daemon's own configuration is read again as it is used, so an edit
to `config.yaml` takes effect on the next scheduling decision rather than on
the next restart: the caps, the idle policy and the grace window all work that
way, and each says so where it reads them. Two settings did not.
`garbageCollection.interval` and `garbageCollection.reviewAfter` were both
taken once, at construction - the interval into a ticker, `reviewAfter` into
the collector - so editing either did nothing at all until the daemon was
restarted, with no reason for the difference.

They are now read where they are used, like the rest. `reviewAfter` is read at
the start of every collection, so the next `owl gc`, the next `owl status` and
the next collection nobody asked for all use what the file says now. The
interval is read before every wait, which is as soon as it can be: a daemon
part-way through a wait finishes that wait first, and a change to the interval
therefore takes effect from the following collection onwards. Setting a long
interval stops the frequent collections at the end of the current one; setting
a short one after a long one is the case a restart is still faster than.

What stays settled at startup stays settled, and for the reasons already
written down: the credential store and `claudePath`, because a Run that is
already going should not discover either changed under it, and `idle.interval`,
which is how often this daemon looks at the machine for its lifetime.

A file that cannot be read is not a reason to stop collecting. The collection
that keeps the disk bounded goes on under Owl's own defaults, which is what
the idle policy already does with an unreadable file.

## Scenarios

### S1 - a longer reviewAfter, edited while the daemon runs, is used by the next collection
Given a daemon started with `garbageCollection.reviewAfter: 1ns`, and a Job waiting for a decision
And `owl gc` reports that Job as having waited too long
When `reviewAfter: 24h` is written into the daemon's `config.yaml`
And `owl gc` runs again, against the same daemon
Then the Job is no longer reported as unfinished work

### S2 - a shorter reviewAfter is used by the next collection too
Given a daemon started with `garbageCollection.reviewAfter: 24h`, and a Job waiting for a decision
And `owl gc` reports no unfinished work
When `reviewAfter: 1ns` is written into the daemon's `config.yaml`
And `owl gc` runs again, against the same daemon
Then the Job is reported as having been waiting for a decision
And the daemon was never restarted

### S3 - owl status reads the same setting, at the same moment
Given the daemon of S2, after the edit
When `owl status` runs
Then it lists the Job under unfinished work, as `owl gc` does

### S4 - a longer interval, edited while the daemon runs, stops the frequent collections
Given a daemon started with `garbageCollection.interval: 200ms`, which reclaims a stray worktree on its own
When `interval: 24h` is written into the daemon's `config.yaml`
And a stray worktree is left where the first one was reclaimed from
Then it is still there long after 200ms, because the daemon is now waiting a day
And `owl gc` still reclaims it when asked

### S5 - a configuration file that cannot be read leaves collection running on the defaults
Given a daemon started with `garbageCollection.reviewAfter: 1ns` reporting a waiting Job
When the `config.yaml` is replaced with something that is not a configuration file
And `owl gc` runs
Then it still exits 0 and still reclaims a stray worktree
And the Job is no longer reported, because Owl's own default for `reviewAfter` applies

### S6 - the settings that are settled at startup are still settled at startup
Given a daemon started with a `claudePath` naming the stub agent
When `claudePath` is changed to a path that is not a program, and a Job is run
Then the Run still uses the tool the daemon started with, and succeeds
