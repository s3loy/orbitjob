#!/usr/bin/env bash
# Check staged files against indexed .gitignore rules, including force-adds.
set -euo pipefail

index=$(git rev-parse --path-format=absolute --git-path index)
export GIT_INDEX_FILE="$index"
cd -- "$(git rev-parse --show-toplevel)"
staged=$(git -c core.quotePath=true diff --cached --name-only --diff-filter=ACMR)
if [ -z "$staged" ]; then
	exit 0
fi

rules=$(mktemp -d)
trap 'rm -rf -- "$rules"' EXIT
git ls-files --stage -z -- ':(glob)**/.gitignore' |
	while IFS= read -r -d '' entry; do
		read -r mode blob stage <<< "${entry%%$'\t'*}"
		if [[ $stage == 0 && ( $mode == 100644 || $mode == 100755 ) ]]; then
			rule="$rules/${entry#*$'\t'}"
			mkdir -p -- "${rule%/*}"
			git cat-file blob "$blob" > "$rule"
		fi
	done

# Git quotes names containing line breaks. Use the same quoting on both lists.
ignored=$(git -C "$rules" --git-dir="$(git rev-parse --absolute-git-dir)" \
	--work-tree="$rules" -c core.quotePath=true ls-files -ci --exclude-per-directory=.gitignore)
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
