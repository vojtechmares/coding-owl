---
name: owl-release
description: Cuts a Coding Owl release from main - brings CHANGELOG.md up to date with every change since the last tag, commits and pushes it, then tags the ZeroVer version svu picks and pushes the tag, which publishes the release. Use when the user runs /owl-release.
disable-model-invocation: true
argument-hint: "[patch|minor|vX.Y.Z]"
---

# Owl release

Invoking this skill is the go-ahead to publish. It runs to the tag push without
stopping for confirmation, and stops only when a check fails.

Arguments: $ARGUMENTS

Run everything from the repository root. `changes.sh`, `lint-changelog.sh` and
`cut-changelog.sh` are in `.agents/skills/owl-release/scripts/`.

## Rules

- Releases come from `main`, from a clean tree that matches `origin/main`. When
  a check fails, stop and tell the user what to fix. Never stash, commit, move
  or delete their files to get past it, never switch branches, never force-push.
- Versions are ZeroVer (`v0.x.y`), worked out by `svu next --v0` inside
  `scripts/release.sh`, which refuses anything at or above `v1.0.0`. Never
  create or push a tag by hand.
- CHANGELOG.md follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
  A release's section is published as its GitHub release notes, so it is
  written for people - see [WRITING.md](WRITING.md).

## Workflow

1. **Preflight.** Run `scripts/release.sh --dry-run`, adding a bump or version
   if the arguments name one. It checks the branch, the tree and the remote,
   and prints `vCURRENT -> vNEXT`. vNEXT is the version for every step below. A
   note that CHANGELOG.md has no section for it yet is expected.

   If CHANGELOG.md already has a section for vNEXT, an earlier run stopped after
   pushing the changelog: run `lint-changelog.sh vNEXT` and go to step 6.

2. **Collect the changes.** `changes.sh` lists every commit since the last tag
   the changelog has to cover. Read a commit (`git show <hash>`) when its
   subject does not say enough. If the arguments ask for docs, build or chore
   changes, use `changes.sh --all`.

3. **Write [Unreleased].** Edit CHANGELOG.md as [WRITING.md](WRITING.md) says:
   add what is missing, rewrite what reads like a commit log, keep it brief.
   Then build the coverage table from [WRITING.md](WRITING.md). A listed commit
   that no entry covers and no reason explains is a missing entry - write it.
   Run `lint-changelog.sh` until it passes, and shorten what it warns about.

4. **Cut the release.** `cut-changelog.sh vNEXT` dates the section with
   today's date, opens a new [Unreleased] and points the links at the tag. Then
   run `lint-changelog.sh vNEXT`, and `scripts/release-notes.sh vNEXT` to check
   the release notes read well on their own.

5. **Commit and push the changelog.**

   ```sh
   git add CHANGELOG.md
   git commit --signoff -m "docs(changelog): release vNEXT"
   git push origin main
   ```

   If the push is rejected, main moved: stop and tell the user. Do not rebase,
   pull or force.

6. **Tag and publish.** Run `scripts/release.sh vNEXT --yes`. The explicit
   version cannot drift from the one in the changelog, and `--yes` because
   invoking the skill was the approval. It runs every check again, refuses a
   version the changelog has no section for, then tags and pushes.

7. **Report.** The version, the workflow link `release.sh` printed, the
   release notes, and the coverage table, so the user can check after the fact.

If step 6 fails after step 5 landed, keep the changelog commit: fix the cause
and run step 6 again. Never revert the commit.
