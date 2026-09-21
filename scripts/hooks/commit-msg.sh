#!/usr/bin/env bash
# commit-msg gate: the atomic-commit discipline — a real subject, at most 72
# characters, no WIP marker. Mirrors the "Commit format" section of
# CONTRIBUTING.md. pre-commit (commit-msg stage) and git both pass the path
# of the file holding the proposed message as the first argument.
set -euo pipefail

if [ $# -lt 1 ]; then
	echo "usage: $0 <commit-msg-file>" >&2
	exit 2
fi

# The subject is the first line that is neither a comment nor blank.
subject=$(sed -e '/^#/d' -e '/^[[:space:]]*$/d' -e q "$1")

problems=()
if [ -z "$subject" ]; then
	problems+=("subject is empty")
fi
if [ "${#subject}" -gt 72 ]; then
	problems+=("subject is ${#subject} characters (limit 72): $subject")
fi
wip_re='^[Ww][Ii][Pp]([[:space:]]|:|$)'
if [[ "$subject" =~ $wip_re ]]; then
	problems+=("subject is a WIP marker; finish or split the commit: $subject")
fi

if [ ${#problems[@]} -gt 0 ]; then
	for p in "${problems[@]}"; do
		echo "commit-msg: $p" >&2
	done
	exit 1
fi
