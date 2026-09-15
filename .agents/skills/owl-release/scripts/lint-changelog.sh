#!/bin/bash
#
# Lints one section of CHANGELOG.md against Keep a Changelog, and against
# reading like a commit log rather than something written for people.
#
#   lint-changelog.sh           the [Unreleased] section
#   lint-changelog.sh v0.1.0    the section for a release
#
# Errors - exit 1:
#   - the section is missing or has no entries; a release heading that is not
#     `## [0.1.0] - YYYY-MM-DD`
#   - a `###` other than Added, Changed, Deprecated, Removed, Fixed, Security,
#     a duplicate one, or an empty one
#   - an entry outside a `###`, or text in a `###` that is not an entry
#   - an entry with a commit type prefix, a commit hash, or a test's "pin that"
#   - any release heading in the file without a link reference
# Warnings - an entry longer than 250 characters.
#
# Environment:
#   CHANGELOG       the changelog to lint (default: CHANGELOG.md at the root)
#
set -euo pipefail

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

case "${1:-}" in
-h | --help)
	sed -n '3,21p' "$0" | sed 's/^# \{0,1\}//'
	exit 0
	;;
esac
(($# <= 1)) || die "usage: $0 [version]"

CHANGELOG="${CHANGELOG:-$(git rev-parse --show-toplevel)/CHANGELOG.md}"
[[ -f "$CHANGELOG" ]] || die "no changelog at $CHANGELOG"
NAME="$(basename "$CHANGELOG")"

TARGET="Unreleased"
if (($#)); then
	TARGET="${1#v}"
	[[ "$TARGET" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "'$1' is not a version like v0.1.0"
fi

ERRORS=0
WARNINGS=0
error() {
	printf '%s:%s: error: %s\n' "$NAME" "$1" "$2"
	ERRORS=$((ERRORS + 1))
}
warning() {
	printf '%s:%s: warning: %s\n' "$NAME" "$1" "$2"
	WARNINGS=$((WARNINGS + 1))
}

# One record per line of interest, tab separated: kind, line number, text.
# Wrapped entries are joined onto the line they started on.
RECORDS="$(awk -v target="$TARGET" '
	function emit(kind, line, text) { printf "%s\t%d\t%s\n", kind, line, text }
	function flush() {
		if (item != "") emit("ITEM", itemline, item)
		item = ""
	}
	/^\[[^]]+\]: / {
		flush()
		label = $0
		sub(/^\[/, "", label)
		sub(/\]: .*$/, "", label)
		emit("LINK", NR, label)
		next
	}
	/^## / {
		flush()
		insection = 0
		version = ""
		if (match($0, /^## \[[^]]+\]/)) version = substr($0, 5, RLENGTH - 5)
		emit("RELEASE", NR, version)
		intarget = (tolower(version) == tolower(target))
		if (intarget) emit("HEADING", NR, $0)
		next
	}
	!intarget { next }
	/^### / {
		flush()
		name = substr($0, 5)
		sub(/[ \t]+$/, "", name)
		emit("SECTION", NR, name)
		insection = 1
		next
	}
	/^[-*] / {
		flush()
		item = substr($0, 3)
		itemline = NR
		if (!insection) {
			emit("STRAY", NR, item)
			item = ""
		}
		next
	}
	/^[ \t]*$/ { flush(); next }
	/^[ \t]+[^ \t]/ && item != "" {
		text = $0
		sub(/^[ \t]+/, "", text)
		item = item " " text
		next
	}
	insection { flush(); emit("TEXT", NR, $0) }
	END { flush() }
' "$CHANGELOG")"

TYPES="Added Changed Deprecated Removed Fixed Security"
# In a variable, because bash 3.2 and later bash disagree on quotes inside =~.
RELEASE_HEADING='^## \[[0-9]+\.[0-9]+\.[0-9]+\] - [0-9]{4}-[0-9]{2}-[0-9]{2}( \[YANKED\])?$'
HEADING_LINE=""
SEEN_SECTIONS=" "
CURRENT_SECTION=""
CURRENT_SECTION_LINE=""
CURRENT_SECTION_ITEMS=0
ITEMS=0
RELEASES=""
LINKS=" "

end_section() {
	if [[ -n "$CURRENT_SECTION" ]] && ((CURRENT_SECTION_ITEMS == 0)); then
		error "$CURRENT_SECTION_LINE" "### $CURRENT_SECTION has no entries - drop the heading"
	fi
}

check_item() {
	local line="$1" text="$2" token
	if printf '%s' "$text" | grep -qE '^[a-z]+(\([^)]*\))?!?: '; then
		error "$line" "reads like a commit subject - describe the change for people: $text"
	fi
	for token in $(printf '%s' "$text" | tr -c '[:alnum:]' '\n' | grep -xE '[0-9a-f]{7,40}' || true); do
		if [[ "$token" =~ [a-f] && "$token" =~ [0-9] ]]; then
			error "$line" "mentions what looks like a commit hash ($token)"
		fi
	done
	if printf '%s' "$text" | grep -qiE '(^|[^a-z])pins? that '; then
		error "$line" "reads like a test commit (\"pin that\") - say what changed for people"
	fi
	if ((${#text} > 250)); then
		warning "$line" "entry is ${#text} characters - keep the changelog brief"
	fi
}

while IFS=$'\t' read -r KIND LINE TEXT; do
	case "$KIND" in
	LINK) LINKS="$LINKS$(printf '%s' "$TEXT" | tr '[:upper:]' '[:lower:]') " ;;
	RELEASE) RELEASES="$RELEASES$LINE"$'\t'"$TEXT"$'\n' ;;
	HEADING)
		if [[ -n "$HEADING_LINE" ]]; then
			error "$LINE" "a second section for [$TARGET] - the first is on line $HEADING_LINE"
			continue
		fi
		HEADING_LINE="$LINE"
		if [[ "$TARGET" == "Unreleased" ]]; then
			[[ "$TEXT" == "## [Unreleased]" ]] || error "$LINE" "expected '## [Unreleased]', got '$TEXT'"
		elif [[ ! "$TEXT" =~ $RELEASE_HEADING ]]; then
			error "$LINE" "expected '## [$TARGET] - YYYY-MM-DD', got '$TEXT'"
		fi
		;;
	SECTION)
		end_section
		CURRENT_SECTION="$TEXT"
		CURRENT_SECTION_LINE="$LINE"
		CURRENT_SECTION_ITEMS=0
		if [[ " $TYPES " != *" $TEXT "* ]]; then
			error "$LINE" "### $TEXT is not a Keep a Changelog type ($TYPES)"
		elif [[ "$SEEN_SECTIONS" == *" $TEXT "* ]]; then
			error "$LINE" "### $TEXT appears twice - merge them"
		fi
		SEEN_SECTIONS="$SEEN_SECTIONS$TEXT "
		;;
	ITEM)
		ITEMS=$((ITEMS + 1))
		CURRENT_SECTION_ITEMS=$((CURRENT_SECTION_ITEMS + 1))
		check_item "$LINE" "$TEXT"
		;;
	STRAY)
		ITEMS=$((ITEMS + 1))
		error "$LINE" "entry is not under a ### heading: $TEXT"
		;;
	TEXT) error "$LINE" "text under ### $CURRENT_SECTION that is not an entry: $TEXT" ;;
	esac
done <<<"$RECORDS"
end_section

if [[ -z "$HEADING_LINE" ]]; then
	error 1 "no section for [$TARGET]"
elif ((ITEMS == 0)); then
	error "$HEADING_LINE" "[$TARGET] has no entries"
fi

while IFS=$'\t' read -r LINE VERSION; do
	[[ -n "$LINE" ]] || continue
	if [[ -z "$VERSION" ]]; then
		error "$LINE" "release heading without a [version]"
	elif [[ "$LINKS" != *" $(printf '%s' "$VERSION" | tr '[:upper:]' '[:lower:]') "* ]]; then
		error "$LINE" "[$VERSION] has no link reference at the bottom of the file"
	fi
done <<<"$RELEASES"

if ((ERRORS)); then
	printf '%d error(s), %d warning(s) in [%s]\n' "$ERRORS" "$WARNINGS" "$TARGET"
	exit 1
fi
printf '[%s] is fine (%d entries, %d warning(s))\n' "$TARGET" "$ITEMS" "$WARNINGS"
