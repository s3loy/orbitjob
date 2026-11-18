#!/usr/bin/env bash
# Check staged files against repository .gitignore rules, including force-adds.
set -euo pipefail

staged=$(git -c core.quotePath=true diff --cached --name-only --diff-filter=ACMR)
if [ -z "$staged" ]; then
	exit 0
fi

# Git quotes names containing line breaks. Use the same quoting on both lists.
ignored=$(git -c core.quotePath=true ls-files -ci --exclude-per-directory=.gitignore)
if [ -z "$ignored" ]; then
	exit 0
fi

export LC_ALL=C
hits=$(comm -12 <(printf '%s\n' "$staged" | sort -u) <(printf '%s\n' "$ignored" | sort -u))
if [ -z "$hits" ]; then
	exit 0
fi

echo "Refusing to commit files covered by repository .gitignore rules:"
printf '%s\n' "$hits"
echo "Unstage them: git rm --cached <file>"
exit 1
