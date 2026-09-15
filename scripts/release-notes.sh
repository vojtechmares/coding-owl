#!/bin/bash
#
# Prints the CHANGELOG.md section for a release - everything under its
# `## [0.1.0] - 2026-09-15` heading - which is what the release is published
# with as its notes.
#
#   ./scripts/release-notes.sh v0.1.0
#
# Fails when the changelog has no section for the version, or an empty one.
#
# Environment:
#   CHANGELOG       the changelog to read (default: CHANGELOG.md at the root)
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHANGELOG="${CHANGELOG:-$ROOT/CHANGELOG.md}"

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

(($# == 1)) || die "usage: $0 <version>"
# Tags carry a v, changelog headings do not.
VERSION="${1#v}"
[[ -f "$CHANGELOG" ]] || die "no changelog at $CHANGELOG"

# Plain string comparison rather than a regex, so the dots in the version
# match only dots. The section ends at the next release or at the link
# references, and blank lines around it are trimmed. Kept to POSIX awk: this
# runs under mawk on the release runner and BSD awk on a Mac.
NOTES="$(awk -v heading="## [$VERSION]" '
	function flush() {
		for (; blanks > 0; blanks--) print ""
	}
	index($0, heading) == 1 {
		rest = substr($0, length(heading) + 1)
		if (rest == "" || substr(rest, 1, 1) == " ") {
			found = 1
			next
		}
	}
	found && (/^## / || /^\[[^]]+\]: /) { exit }
	found {
		if ($0 ~ /^[ \t]*$/) {
			if (printed) blanks++
			next
		}
		flush()
		print
		printed = 1
	}
' "$CHANGELOG")"

[[ -n "$NOTES" ]] || die "CHANGELOG.md has no section for $VERSION, or it is empty"
printf '%s\n' "$NOTES"
