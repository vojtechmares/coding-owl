# Writing the changelog

## What goes in

| Commits | Where |
|---|---|
| `feat` | Added, or Changed when it changes something already released |
| `fix` | Fixed - unless what it fixes was never released, then it is part of that entry |
| `perf`, `refactor` | Changed, described by what it changes or improves |
| breaking (`!:` or `BREAKING CHANGE`, `changes.sh` marks them) | Changed or Removed, the entry starting with `**Breaking:**` |
| deprecations, removals, vulnerability fixes | Deprecated, Removed, Security |
| `build`, `chore`, `ci`, `docs`, `style`, `test` | Only when the user asks |
| the website - `website` scope, or only `website/` | Never |

`changes.sh` already leaves out the last two rows.

## How it reads

- Written for people who use Owl, not people who read its code: what they can
  do now, what behaves differently, what no longer goes wrong.
- Brief. One entry per change a user would notice - a feature, its follow-up
  fixes and its refactors are usually one entry. One or two sentences each.
- No commit types, scopes, hashes, file or function names, or test phrasing
  like "pin that". `lint-changelog.sh` catches the obvious ones.
- The voice of the entries already there, and the terms of `CONTEXT.md`
  (Project, Job, Run, Account, Verification, Handoff).
- Sections in the order Added, Changed, Deprecated, Removed, Fixed, Security,
  empty ones left out. Lines wrap at 80 columns, continuations indented by two
  spaces.

Existing entries in [Unreleased] are rewritten to these rules too, not only the
new ones.

## Example

From these commits:

```
268c407 fix(daemon): make the socket under a private umask and tighten its directory on every start
bbfa9ce test(daemon): pin that the socket is 0600 the moment it is made
12502eb fix(daemon): settle configuration, credentials and recovery before listening
0355dce test(daemon): pin that a daemon which cannot start leaves no socket behind
```

one entry, under Fixed if the daemon was released before, or folded into the
daemon's Added entry if not:

```md
- The daemon's socket is private to the user from the moment it exists, and a
  daemon that cannot start leaves no socket behind.
```

## Coverage table

How the changelog is checked for completeness, and part of the final report:
every commit `changes.sh` listed is accounted for. Under each entry, the commits
it covers, then the listed commits no entry covers, each with the reason.

```md
**Fixed: The daemon's socket is private to the user...**
268c407, 12502eb

**Not in the changelog**
- a1b2c3d feat(cli): ... - reverted by e4f5a6b before the release
```

That list stays short: refactors are in the changelog, and a fix to something
unreleased is covered by the entry it is part of.
