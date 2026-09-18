#!/usr/bin/env bash
# Block until every named verdict file exists in a verdict directory, then
# print each one under its name. Exits 1 on timeout and names the missing ones.
#
# Usage: wait-verdicts.sh <verdict-dir> <name>... [--timeout <seconds>]
# Example: wait-verdicts.sh "$VERDICTS" behavior
set -euo pipefail

timeout=540 # 9 minutes, inside the Bash tool's 10 minute cap
dir=""
names=()
while [ $# -gt 0 ]; do
  case "$1" in
    --timeout) timeout="$2"; shift 2 ;;
    *) if [ -z "$dir" ]; then dir="$1"; else names+=("$1"); fi; shift ;;
  esac
done
if [ -z "$dir" ] || [ ${#names[@]} -eq 0 ]; then
  echo "usage: $0 <verdict-dir> <name>... [--timeout <seconds>]" >&2
  exit 2
fi

deadline=$(( $(date +%s) + timeout ))
while :; do
  missing=()
  for n in "${names[@]}"; do [ -f "$dir/$n.md" ] || missing+=("$n"); done
  [ ${#missing[@]} -eq 0 ] && break
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "TIMEOUT - no verdict from:"
    printf '  %s\n' "${missing[@]}"
    exit 1
  fi
  sleep 10
done

for n in "${names[@]}"; do echo "===== $n ====="; cat "$dir/$n.md"; done
