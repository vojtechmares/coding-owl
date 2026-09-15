#!/bin/bash
#
# Lists the commits since the last release that the changelog has to cover,
# grouped by Conventional Commit type, oldest first.
#
#   changes.sh          feat, fix, perf, refactor, and anything unconventional
#   changes.sh --all    build, chore, ci, docs, style and test as well
#
# Website commits are never listed: a `website` scope, or a commit that only
# touches website/. Breaking changes are marked [breaking]. Without a release
# tag, the range is the whole history.
#
set -euo pipefail

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

ALL=0
case "${1:-}" in
"") ;;
--all) ALL=1 ;;
-h | --help)
	sed -n '3,12p' "$0" | sed 's/^# \{0,1\}//'
	exit 0
	;;
*) die "unknown argument: $1" ;;
esac

cd "$(git rev-parse --show-toplevel)"

LAST="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)"
if [[ -n "$LAST" ]]; then
	RANGE="$LAST..HEAD"
	SINCE="since $LAST"
else
	RANGE="HEAD"
	SINCE="since the first commit - there is no release yet"
fi

# A pathspec leaves out the commits that only touch website/.
PATHS=(-- . ':(exclude)website')
TOTAL="$(git rev-list --no-merges --count "$RANGE")"
# A breaking change says so in its subject (feat!:) or in its body.
BREAKING="$(git log --no-merges --format=%h -E --grep='BREAKING[ -]CHANGE' "$RANGE" "${PATHS[@]}" | tr '\n' ' ')"

printf 'Changes %s (%s commits in %s)\n' "$SINCE" "$TOTAL" "$RANGE"

git log --reverse --no-merges --format='%h%x09%s' "$RANGE" "${PATHS[@]}" |
	awk -F '\t' -v all="$ALL" -v breaking="$BREAKING" -v total="$TOTAL" '
		BEGIN {
			n = split(breaking, b, " ")
			for (i = 1; i <= n; i++) isbreaking[b[i]] = 1
			split("feat fix perf refactor", listed, " ")
			for (i in listed) always[listed[i]] = 1
			split("build chore ci docs style test", optional, " ")
			for (i in optional) extra[optional[i]] = 1
		}
		{
			hash = $1
			subject = substr($0, length(hash) + 2)
			type = "other"
			scope = ""
			bang = 0
			if (match(subject, /^[a-z]+(\([^)]*\))?!?: /)) {
				head = substr(subject, 1, RLENGTH - 2)
				if (head ~ /!$/) {
					bang = 1
					head = substr(head, 1, length(head) - 1)
				}
				p = index(head, "(")
				if (p) {
					type = substr(head, 1, p - 1)
					scope = substr(head, p + 1, length(head) - p - 1)
				} else {
					type = head
				}
				if (!(type in always) && !(type in extra)) type = "other"
			}
			if (scope == "website") {
				website++
				next
			}
			if ((type in extra) && !all) {
				skipped[type]++
				nskipped++
				next
			}
			mark = (bang || (hash in isbreaking)) ? " [breaking]" : ""
			lines[type] = lines[type] "  " hash " " subject mark "\n"
			count[type]++
			nlisted++
		}
		END {
			website += total - NR
			printf "%d listed, %d website commits left out", nlisted, website
			if (nskipped) {
				printf ", %d left out by type (", nskipped
				sep = ""
				split("build chore ci docs style test", order, " ")
				for (i = 1; i <= 6; i++) {
					t = order[i]
					if (t in skipped) {
						printf "%s%s %d", sep, t, skipped[t]
						sep = ", "
					}
				}
				printf ") - --all lists them"
			}
			printf "\n"
			split("feat fix perf refactor build chore ci docs style test other", order, " ")
			for (i = 1; i <= 11; i++) {
				t = order[i]
				if (!(t in count)) continue
				printf "\n%s (%d)\n%s", t, count[t], lines[t]
			}
		}
	'
