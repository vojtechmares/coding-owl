#!/bin/bash
#
# Cuts a release in CHANGELOG.md: the [Unreleased] section becomes the
# release's, dated, a new empty [Unreleased] goes above it, and the link
# references at the bottom point at the tag.
#
#   cut-changelog.sh v0.1.0              dated today
#   cut-changelog.sh v0.1.0 2026-09-15   dated explicitly
#
# The links are GitHub URLs for the remote: [Unreleased] compares the new tag
# with HEAD, and the release compares the previous release's tag with its own,
# or points at its own tag when there is no previous release.
#
# Environment:
#   CHANGELOG       the changelog to change (default: CHANGELOG.md at the root)
#   REMOTE          git remote the links point at (default: origin)
#
set -euo pipefail

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

case "${1:-}" in
-h | --help)
	sed -n '3,17p' "$0" | sed 's/^# \{0,1\}//'
	exit 0
	;;
esac
(($# == 1 || $# == 2)) || die "usage: $0 <version> [date]"

VERSION="${1#v}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "'$1' is not a version like v0.1.0"
DATE="${2:-$(date +%F)}"
[[ "$DATE" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "'$DATE' is not a date like 2026-09-15"

CHANGELOG="${CHANGELOG:-$(git rev-parse --show-toplevel)/CHANGELOG.md}"
REMOTE="${REMOTE:-origin}"
[[ -f "$CHANGELOG" ]] || die "no changelog at $CHANGELOG"

UNRELEASED="$(grep -c '^## \[Unreleased\][[:space:]]*$' "$CHANGELOG" || true)"
[[ "$UNRELEASED" == 1 ]] || die "expected one '## [Unreleased]' heading, found $UNRELEASED"
if grep -qF "## [$VERSION]" "$CHANGELOG"; then
	die "CHANGELOG.md already has a section for $VERSION"
fi

# The first release heading below [Unreleased], and whether [Unreleased] has
# anything to release.
read -r ENTRIES PREVIOUS < <(awk '
	/^## \[Unreleased\]/ { inside = 1; next }
	/^## \[/ {
		if (inside && previous == "" && match($0, /^## \[[0-9]+\.[0-9]+\.[0-9]+\]/))
			previous = substr($0, 5, RLENGTH - 5)
		inside = 0
		next
	}
	/^\[[^]]+\]: / { inside = 0 }
	inside && /^[-*] / { entries++ }
	END { print entries + 0, previous }
' "$CHANGELOG")
((ENTRIES > 0)) || die "[Unreleased] has no entries - there is nothing to release"

# Never print the remote URL itself: it can carry a token.
URL="$(git remote get-url "$REMOTE" 2>/dev/null)" || die "no such remote: $REMOTE"
URL="${URL%.git}"
case "$URL" in
git@github.com:*) REPO="${URL#git@github.com:}" ;;
ssh://git@github.com/*) REPO="${URL#ssh://git@github.com/}" ;;
https://github.com/* | https://*@github.com/*) REPO="${URL#*github.com/}" ;;
*) die "remote $REMOTE is not on GitHub - the links are GitHub compare URLs" ;;
esac
BASE="https://github.com/$REPO"

UNRELEASED_LINK="[Unreleased]: $BASE/compare/v$VERSION...HEAD"
if [[ -n "$PREVIOUS" ]]; then
	RELEASE_LINK="[$VERSION]: $BASE/compare/v$PREVIOUS...v$VERSION"
else
	RELEASE_LINK="[$VERSION]: $BASE/releases/tag/v$VERSION"
fi

TMP="$(mktemp "$CHANGELOG.XXXXXX")"
trap 'rm -f "$TMP"' EXIT

awk -v heading="## [$VERSION] - $DATE" -v unreleased="$UNRELEASED_LINK" -v release="$RELEASE_LINK" '
	/^## \[Unreleased\]/ {
		print "## [Unreleased]"
		print ""
		print heading
		blank = 0
		next
	}
	/^\[[Uu]nreleased\]: / {
		print unreleased
		print release
		linked = 1
		blank = 0
		next
	}
	{
		print
		blank = ($0 ~ /^[ \t]*$/)
	}
	END {
		if (!linked) {
			if (!blank) print ""
			print unreleased
			print release
		}
	}
' "$CHANGELOG" >"$TMP"

# Written over rather than moved into place, so the file keeps its mode.
cat "$TMP" >"$CHANGELOG"

printf 'Cut [%s] - %s from [Unreleased] (%d entries)\n' "$VERSION" "$DATE" "$ENTRIES"
printf '  %s\n  %s\n' "$UNRELEASED_LINK" "$RELEASE_LINK"
