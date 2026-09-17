#!/bin/bash
#
# Copies the GitHub issues that carry ready-for-agent into the Coding Owl
# queue, one Job per issue. The prompt each Job gets names the issue by number
# and carries the gh commands that read it, ready to paste, so the Agent never
# has to go looking for the ticket - and it tells the Agent to check the issue
# for blockers before it does anything else.
#
#   ./scripts/queue-ready-for-agent.sh              queue what is ready
#   ./scripts/queue-ready-for-agent.sh --dry-run    print what it would queue
#
# Running it again is safe. An issue whose Job is pending, running, waiting for
# review or already accepted is left alone. An issue whose Job ended blocked or
# exhausted is queued again: that is how an issue an Agent found blocked comes
# back to the queue for a later attempt, once the blocker has cleared.
#
# Flags:
#   -n, --dry-run          print what would be queued, queue nothing
#   -v, --verbose          with --dry-run, print the prompt each Job would get
#   -p, --project NAME     Owl Project to queue against (default: the Project
#                          registered at this repository)
#   -R, --repo OWNER/NAME  GitHub repository to read issues from (default: the
#                          repository this clone points at)
#   -l, --label LABEL      the label that marks an issue ready (default:
#                          ready-for-agent)
#       --limit N          how many issues to read at most (default: 50)
#       --ttl N            how many Runs each Job may take (default: Owl's own)
#       --no-plan          queue Jobs that are carried out without planning
#                          first - not advised, see docs/agents/queue-from-github.md
#       --no-requeue       leave blocked and exhausted Jobs alone
#       --max-attempts N   stop queueing an issue again once this many of its
#                          Jobs have ended without the work landing (default: 3)
#   -r, --highest-first    queue from the highest issue number down (default:
#                          lowest first, so the oldest work is queued first)
#   -f, --force            queue an issue even when it already has a Job
#   -h, --help             this message
#
# The Agent needs the gh CLI to read its issue. That is not in Owl's default
# allowlist (ADR-0035), so grant it in the Project's own configuration:
#
#   allowedTools:
#     - Bash(gh:*)
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

LABEL="ready-for-agent"
LIMIT=50
PROJECT=""
REPO=""
TTL=""
DRY_RUN=0
VERBOSE=0
FORCE=0
HIGHEST_FIRST=0
NO_PLAN=0
REQUEUE=1
MAX_ATTEMPTS=3

# REQUEUE_STATES are the states a Job may be in for its issue to be queued
# again. Both mean the Job left the queue having delivered nothing.
REQUEUE_STATES="blocked exhausted"

# HOLD_STATES are the states that mean an issue is already being dealt with:
# waiting its turn, running, or waiting for a person to decide about the work.
HOLD_STATES="pending active review done"

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

warn() {
	printf 'warning: %s\n' "$*" >&2
}

# One line per issue, so that a pass over a full queue reads as a column of
# decisions rather than as prose.
report() {
	printf '%-7s #%-5s %s\n' "$1" "$2" "$3"
}

usage() {
	awk 'NR == 1 { next } /^[^#]/ { exit } { sub(/^#[ ]?/, ""); print }' "$0"
}

while (($#)); do
	case "$1" in
	-n | --dry-run) DRY_RUN=1 ;;
	-v | --verbose) VERBOSE=1 ;;
	-f | --force) FORCE=1 ;;
	-r | --highest-first) HIGHEST_FIRST=1 ;;
	--no-plan) NO_PLAN=1 ;;
	--no-requeue) REQUEUE=0 ;;
	--max-attempts)
		MAX_ATTEMPTS="${2:-}"
		[[ "$MAX_ATTEMPTS" =~ ^[0-9]+$ ]] || die "--max-attempts takes a whole number"
		shift
		;;
	-p | --project)
		PROJECT="${2:-}"
		[[ -n "$PROJECT" ]] || die "--project needs a name"
		shift
		;;
	-R | --repo)
		REPO="${2:-}"
		[[ -n "$REPO" ]] || die "--repo needs an owner/name"
		shift
		;;
	-l | --label)
		LABEL="${2:-}"
		[[ -n "$LABEL" ]] || die "--label needs a label"
		shift
		;;
	--limit)
		LIMIT="${2:-}"
		[[ "$LIMIT" =~ ^[0-9]+$ ]] || die "--limit takes a whole number"
		shift
		;;
	--ttl)
		TTL="${2:-}"
		[[ "$TTL" =~ ^[0-9]+$ ]] || die "--ttl takes a whole number"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) die "unknown argument: $1" ;;
	esac
	shift
done

command -v gh >/dev/null || die "gh is not installed; see https://cli.github.com"
command -v owl >/dev/null || die "owl is not installed; see the README"
gh auth status >/dev/null 2>&1 || die "gh is not authenticated; run: gh auth login"

# The daemon holds the queue, so an unreachable one is the first thing to say
# rather than the last thing to find out.
owl queue list >/dev/null 2>&1 || die "the Owl daemon is not answering; run: owl daemon status"

if [[ -z "$REPO" ]]; then
	REPO="$(gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null || true)"
	[[ -n "$REPO" ]] || die "cannot tell which GitHub repository this is; pass --repo owner/name"
fi

# The Project is the one registered at this working tree, matched on its path:
# names are the Project's identity (ADR-0031) and need not match the directory.
if [[ -z "$PROJECT" ]]; then
	TOPLEVEL="$(git rev-parse --show-toplevel 2>/dev/null || true)"
	[[ -n "$TOPLEVEL" ]] || die "not inside a git repository; pass --project NAME"
	PROJECT="$(owl project list | awk -v path="$TOPLEVEL" '
		NR == 1 { next }
		{
			# NAME PATH BASE-BRANCH, padded with spaces. The path is taken as
			# a whole field so that one Project is not read as the prefix of
			# another.
			if ($2 == path) { print $1; exit }
		}
	')"
	[[ -n "$PROJECT" ]] || die "no Project is registered at $TOPLEVEL; run: owl project add $TOPLEVEL"
fi

# What Owl knows about the Project, so that a Job queued here is a Job that can
# actually run. Neither of these is fatal: the queue is still the right place
# for the work, and both are fixed in the Project's configuration.
PROJECT_SHOW="$(owl project show "$PROJECT" 2>/dev/null || true)"
[[ -n "$PROJECT_SHOW" ]] || die "Owl has no Project called $PROJECT"

CONFIG_SOURCE="$(printf '%s\n' "$PROJECT_SHOW" | awk -F': ' '$1 == "config" { print $2 }')"
ACCOUNT="$(printf '%s\n' "$PROJECT_SHOW" | awk -F': ' '$1 == "account" { print $2 }')"
if [[ "$ACCOUNT" == "(none)" || -z "$ACCOUNT" ]]; then
	warn "Project $PROJECT names no Account, so its Runs will fail before an Agent starts."
	warn "  put \`account: <name>\` in its configuration; owl project show $PROJECT says which file is in force"
fi

# The prompt tells the Agent to read its issue with gh, which the default
# allowlist does not grant (ADR-0035). The grant may be in the Project's file
# or in the Account's settings.json, so both are looked at, and silence is
# only a warning: neither file is Owl's to be sure about.
grants_gh() {
	local config="$1"
	case "$config" in
	"" | "(none)") return 1 ;;
	*:*)
		# An in-repo form, read from the base branch, as Owl reads it.
		local branch="${config%%:*}" path="${config#*:}"
		git show "$branch:$path" 2>/dev/null | grep -q 'Bash(gh'
		;;
	*) grep -q 'Bash(gh' "$config" 2>/dev/null ;;
	esac
}

account_grants_gh() {
	local settings="${XDG_DATA_HOME:-$HOME/.local/share}/coding-owl/accounts/$1/settings.json"
	[[ -f "$settings" ]] && grep -q 'Bash(gh' "$settings"
}

if ! grants_gh "$CONFIG_SOURCE" && ! account_grants_gh "$ACCOUNT"; then
	warn "nothing seen grants an Agent the gh CLI, and the prompt asks it to read its issue with gh."
	warn "  add to the Project's configuration:  allowedTools: [\"Bash(gh:*)\"]"
fi

# The states of every Job this Project already has for an issue, one per line,
# as "<issue> <state>". Parsed by finding the marker rather than by counting
# columns from the right, so that a Project name with a space in it is read
# correctly.
job_states() {
	owl queue list --all | awk -v project="$PROJECT" '
		match($0, /\[gh#[0-9]+\]/) {
			head = substr($0, 1, RSTART - 1)
			sub(/[ \t]+$/, "", head)
			n = split(head, f, /[ \t]+/)
			if (n < 4) next
			# POSITION ID PROJECT... STATE, with the Project name possibly
			# holding a space of its own.
			proj = f[3]
			for (i = 4; i <= n - 1; i++) proj = proj " " f[i]
			if (proj != project) next
			print substr($0, RSTART + 4, RLENGTH - 5) " " f[n]
		}
	'
}

JOBS="$(job_states)"

states_for() {
	printf '%s\n' "$JOBS" | awk -v issue="$1" '$1 == issue { print $2 }'
}

holds() {
	local state
	for state in $1; do
		case " $HOLD_STATES " in
		*" $state "*) return 0 ;;
		esac
	done
	return 1
}

requeueable() {
	local state
	for state in $1; do
		case " $REQUEUE_STATES " in
		*" $state "*) return 0 ;;
		esac
	done
	return 1
}

# The prompt a Job is queued with. Written with placeholders and substituted
# afterwards rather than interpolated by the shell, so that nothing in an issue
# title is ever expanded as shell.
prompt_template() {
	cat <<'TEMPLATE'
[gh#__ISSUE__] __TITLE__

Carry out GitHub issue #__ISSUE__ of __SLUG__, end to end.

The issue is the specification, not this prompt, and it may have changed since
this Job was queued. Read it in full, with its comments, before anything else:

    gh issue view __ISSUE__ --repo __SLUG__ --comments

    Issue:  __SLUG__#__ISSUE__ - __TITLE__
    URL:    __URL__
    Labels when queued: __LABELS__

If gh is not available to you, stop and say so plainly. Do not guess at what
the issue asks for, and do not work from this prompt alone.

## 1. Check the issue for blockers, before anything else

Run these, exactly as they are:

    gh issue view __ISSUE__ --repo __SLUG__ --json state,labels --jq '{state: .state, labels: [.labels[].name]}'
    gh api repos/__SLUG__/issues/__ISSUE__/dependencies/blocked_by --jq '.[] | select(.state == "open") | "blocked by #\(.number) \(.title)"'
    gh api repos/__SLUG__/issues/__ISSUE__/sub_issues --jq '.[] | select(.state == "open") | "open sub-issue #\(.number) \(.title)"'
    gh api repos/__SLUG__/issues/__ISSUE__/timeline --paginate --jq '.[] | select(.event == "cross-referenced") | .source.issue | select(.pull_request != null) | select(.state == "open") | "open pull request #\(.number) \(.title)"'

Then read the body and every comment for a blocker written in prose - "blocked
by #12", "depends on #12", "once #12 lands", "waiting on" - and check the state
of every issue named that way:

    gh issue view <n> --repo __SLUG__ --json number,state,title

The issue is blocked when any of these is true:

- it is no longer open, or no longer carries the __LABEL__ label: somebody
  has taken it out of the queue since this Job was made;
- it carries needs-info, needs-triage, ready-for-human or wontfix;
- an issue it is blocked by, or a sub-issue of it, is still open;
- an issue or pull request it says must land first is still open;
- it needs credentials, access or an irreversible external action you do not
  have.

Ambiguity is not a blocker. Where the issue leaves a choice open, take the
conventional default, write down what you chose and why, and carry on.

## 2. If it is blocked, stop - do not work around it

- Say so on the issue, once:

      gh issue comment __ISSUE__ --repo __SLUG__ --body "Coding Owl picked this up and stopped: blocked by <what>, found with <the command above>. Returned to the Owl queue; it will be tried again once that clears."

  Read the comments already there first and say nothing if one of yours
  already says the same thing. This issue comes back for another attempt, and
  it should not collect the same comment every time.

- Change nothing else: no plan, no .coding-owl/HANDOFF.md, no commits, no
  branch, no pull request. Leave the worktree as you found it.
- Say `BLOCKED: <one line>` and finish.

A Run that plans nothing leaves its Job blocked rather than waiting for review,
which is what should happen: there is nothing to review. The issue keeps its
__LABEL__ label, so scripts/queue-ready-for-agent.sh returns it to the Owl
queue on its next pass and a later Run tries again. Do not remove the label and
do not relabel the issue - that is a person's decision, never yours.

## 3. If it is not blocked, do the work

- Read AGENTS.md, CONTEXT.md for the domain language, and the ADRs in
  docs/adr/ that touch what you are changing. Use the glossary's terms; say so
  rather than silently overriding when something contradicts an ADR.
- Cover the behaviour the issue describes with tests, and write them before the
  code that makes them pass.
- Stay inside the issue's scope. Anything else you notice belongs in a new
  issue, not in this diff.
- Commit as you go, Conventional Commits, always with --signoff.
- Keep .coding-owl/HANDOFF.md current as you go: the next Run of this Job
  starts with no memory of this one.

## 4. Finish

- Run the Project's own checks before you stop - `make lint && make test` in
  this repository. Owl runs its Verification after you exit, and a Run that
  fails it has spent an attempt for nothing.
- Push the branch and open a pull request. Its body starts with
  `Closes #__ISSUE__`, so that merging it takes the issue out of the queue:

      git push -u origin HEAD
      gh pr create --repo __SLUG__ --base main --title "<type>(<scope>): <summary>" --body "Closes #__ISSUE__

      ## What changed
      <summary>

      ## Decisions made on my own
      <the choices the issue left open, with reasons - or none>"

- Do not merge it. Owl's Verification runs after you exit, and a person
  accepts or drops the work.
TEMPLATE
}

build_prompt() {
	local issue="$1" title="$2" url="$3" labels="$4" text
	text="$(prompt_template)"
	text="${text//__ISSUE__/$issue}"
	text="${text//__SLUG__/$REPO}"
	text="${text//__LABEL__/$LABEL}"
	text="${text//__URL__/$url}"
	text="${text//__LABELS__/$labels}"
	# The title comes from a GitHub issue, so it is substituted last: a title
	# that happens to hold a placeholder is then only ever text.
	text="${text//__TITLE__/$title}"
	printf '%s\n' "$text"
}

# What is blocking an issue right now, for the report. The Agent checks again
# when it runs, which is the check that counts - this one only says what the
# queue is being given. An API that does not answer is not worth stopping for.
blocked_now() {
	gh api "repos/$REPO/issues/$1/dependencies/blocked_by" \
		--jq '.[] | select(.state == "open") | "#\(.number)"' 2>/dev/null |
		tr '\n' ' ' | sed 's/ $//'
}

# Lowest issue number first, so that the oldest work is queued first and comes
# out of the queue first - the queue is first in, first out (ADR-0025).
# --highest-first turns that around, for a repository where the newest issue is
# the one that matters. The sort is asked of the search as well as of the
# result, because --limit takes its issues in the order the search returns
# them: sorting only here would take the wrong end of a long queue.
SEARCH_SORT="sort:created-asc"
NUMBER_SORT="sort_by(.number)"
if ((HIGHEST_FIRST)); then
	SEARCH_SORT="sort:created-desc"
	NUMBER_SORT="sort_by(-.number)"
fi

ISSUES="$(gh issue list --repo "$REPO" --state open --label "$LABEL" --limit "$LIMIT" \
	--search "$SEARCH_SORT" \
	--json number,title,url,labels \
	--jq "$NUMBER_SORT"' | .[] | [.number, .title, .url, ([.labels[].name] | join(","))] | @tsv')"

if [[ -z "$ISSUES" ]]; then
	printf 'no open issue in %s carries %s\n' "$REPO" "$LABEL"
	exit 0
fi

if ((DRY_RUN)); then
	printf 'dry run: nothing is queued\n'
fi

QUEUED=0
REQUEUED=0
SKIPPED=0

while IFS=$'\t' read -r NUMBER TITLE URL LABELS; do
	[[ -n "$NUMBER" ]] || continue
	STATES="$(states_for "$NUMBER" | tr '\n' ' ')"

	if ((!FORCE)) && [[ -n "$STATES" ]]; then
		if holds "$STATES"; then
			report skip "$NUMBER" "already has a job (${STATES% })"
			SKIPPED=$((SKIPPED + 1))
			continue
		fi
		if ! requeueable "$STATES"; then
			report skip "$NUMBER" "its job is ${STATES% }; --force queues it again"
			SKIPPED=$((SKIPPED + 1))
			continue
		fi
		if ((!REQUEUE)); then
			report skip "$NUMBER" "job ended ${STATES% }, and --no-requeue was given"
			SKIPPED=$((SKIPPED + 1))
			continue
		fi
		# An issue that comes back every pass and ends the same way every time
		# is not waiting on a blocker that will clear - something about it, or
		# about the Project, needs a person. Queueing it for ever would spend
		# an attempt a night on it.
		ATTEMPTS="$(states_for "$NUMBER" | wc -l | tr -d ' ')"
		if ((ATTEMPTS >= MAX_ATTEMPTS)); then
			report skip "$NUMBER" "$ATTEMPTS jobs for it have ended without the work landing; read the issue, or pass --force"
			SKIPPED=$((SKIPPED + 1))
			continue
		fi
	fi

	AGAIN=0
	if [[ -n "$STATES" ]]; then
		AGAIN=1
	fi

	NOTE=""
	BLOCKERS="$(blocked_now "$NUMBER" || true)"
	if [[ -n "$BLOCKERS" ]]; then
		NOTE=" (blocked by $BLOCKERS right now; the Agent checks again when it runs)"
	fi

	PROMPT="$(build_prompt "$NUMBER" "$TITLE" "$URL" "$LABELS")"

	if ((DRY_RUN)); then
		if ((AGAIN)); then
			report requeue "$NUMBER" "$TITLE$NOTE"
			REQUEUED=$((REQUEUED + 1))
		else
			report queue "$NUMBER" "$TITLE$NOTE"
			QUEUED=$((QUEUED + 1))
		fi
		if ((VERBOSE)); then
			printf -- '----- prompt for #%s -----\n%s\n\n' "$NUMBER" "$PROMPT"
		fi
		continue
	fi

	ADD=(add "$PROMPT" --project "$PROJECT")
	if [[ -n "$TTL" ]]; then
		ADD+=(--ttl "$TTL")
	fi
	if ((NO_PLAN)); then
		ADD+=(--no-plan)
	fi

	if ! OUT="$(owl "${ADD[@]}" 2>&1)"; then
		warn "#$NUMBER was not queued: $OUT"
		SKIPPED=$((SKIPPED + 1))
		continue
	fi
	if ((AGAIN)); then
		report requeue "$NUMBER" "$OUT$NOTE"
		REQUEUED=$((REQUEUED + 1))
	else
		report queue "$NUMBER" "$OUT$NOTE"
		QUEUED=$((QUEUED + 1))
	fi
done <<<"$ISSUES"

printf '\n%s queued, %s requeued, %s skipped\n' "$QUEUED" "$REQUEUED" "$SKIPPED"
