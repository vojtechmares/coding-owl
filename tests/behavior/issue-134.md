# Issue #134: `owl project show` reports a near-miss config file in the config-home fallback directory

ADR-0014 form 4 is `~/.config/coding-owl/<project-slug>/config.yaml`, and that
is the only name that location reads. `.coding-owl.yaml` is a valid name for
the in-repo forms (1-3) only, so a file put there under that name is never
discovered and the Project silently falls back to `config.Default()`: no
Account, defaults for everything else. That is the bug this issue was traced
from, and ADR-0014's own consequence - "a config that is not being picked up is
a support question" - says `owl project show` is where it has to be answered.

The change is a **report, and only a report**. When discovery has fallen all
the way through to the defaults and the Project's configuration directory holds
some other `*.yaml`/`*.yml` file, the existing `config:` line names it:

```
config: (none) - found .coding-owl.yaml in /tmp/.../coding-owl/api, but this location expects config.yaml
```

Nothing is loaded, renamed or moved, and the discovery order and file names of
ADR-0014 are untouched. The in-repo forms are out of scope: they have several
valid names already and are read from a base-branch tree listing, which is a
different failure.

The scenarios drive the built `owl` binary against a real daemon, in the XDG
layout `tests/behavior/issue-5.md` and `tests/behavior/issue-14.md` describe,
and read the `config` line of `owl project show`.

## Scenarios

### S1 - the near-miss file is named
Given a Project registered from a repository that carries no Owl file on its
    base branch, and no `config.yaml` in its configuration directory under the
    config home
And `.coding-owl.yaml` written into that directory instead
When `owl project show <name>` runs
Then it exits 0
And the `config` line still begins `(none)`, because nothing was loaded
And it names `.coding-owl.yaml` as the file that was found
And it names the directory the file is in
And it names `config.yaml` as what that location expects

### S2 - reported, never loaded
Given the Project of S1, and the `.coding-owl.yaml` there sets
    `branchPrefix: stray/` and an `account`
When `owl project show <name>` runs
Then the `branch prefix` line is `owl/`, the default, not `stray/`
And the `account` line is `(none)`: the file's Account was not adopted
And the file is still on disk under its own name, byte-identical to what was
    written: it was not renamed or rewritten
And no `config.yaml` appeared beside it: it was not copied into place either

### S3 - `.yml` is a near miss too
Given the Project of S1 with `coding-owl.yml` in its configuration directory
    instead of `.coding-owl.yaml`
When `owl project show <name>` runs
Then the `config` line names `coding-owl.yml` and `config.yaml`

### S4 - a configuration that was found says nothing
Given the Project of S1 with both `config.yaml` and `.coding-owl.yaml` in its
    configuration directory
When `owl project show <name>` runs
Then the `config` line is the path of `config.yaml` and nothing else
And it says neither `(none)` nor anything about a file that was found instead

### S5 - an in-repo configuration says nothing either
Given a Project whose base branch carries `.coding-owl.yaml`
And a `.coding-owl.yaml` left over in its configuration directory under the
    config home
When `owl project show <name>` runs
Then the `config` line is `main:.coding-owl.yaml` and nothing else: discovery
    never reached form 4, so there is nothing there that could have been used

### S6 - files that are not near misses are not reported
Given the Project of S1 whose configuration directory holds only
    `.coding-owl.lock.yaml` - which Owl writes there itself - and `notes.txt`
When `owl project show <name>` runs
Then the `config` line is exactly `(none)`

### S7 - one file is named when several are there
Given the Project of S1 whose configuration directory holds both `alpha.yaml`
    and `coding-owl.yaml`
When `owl project show <name>` runs
Then the `config` line names `coding-owl.yaml`, the likelier near miss, and not
    `alpha.yaml`, which merely sorts first

### S8 - a hostile filename reaches the terminal as text
Given the Project of S1 whose configuration directory holds a `*.yaml` file
    whose name contains an ANSI escape sequence
When `owl project show <name>` runs
Then the output carries no raw `0x1b` byte
And the escape is shown as `\x1b`, the bytes it is
