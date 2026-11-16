#!/usr/bin/env bash
# no-cjk gate: English is used outside README.zh.md and the bilingual
# documentation site (content, rendering and tests). Binary files (NUL byte) and
# files that are not valid UTF-8 are skipped.
set -euo pipefail

staged=$(git -c core.quotePath=false diff --cached --name-only --diff-filter=ACM)
if [ -z "$staged" ]; then
	exit 0
fi

if ! hits=$(printf '%s\n' "$staged" | python3 -c '
import re, sys

# CJK punctuation and symbols, kana, ideograph extension A, unified
# ideographs, hangul syllables, compatibility ideographs, half/fullwidth
# forms.
cjk = re.compile(
    "[\u3000-\u303f\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff"
    "\uac00-\ud7af\uf900-\ufaff\uff00-\uffef]"
)
offenders = []
for path in sys.stdin.read().splitlines():
    if not path or path == "README.zh.md" or path.startswith("site/"):
        continue
    try:
        data = open(path, "rb").read()
    except OSError:
        continue
    if b"\x00" in data:
        continue
    try:
        text = data.decode("utf-8")
    except UnicodeDecodeError:
        continue
    if cjk.search(text):
        offenders.append(path)
for path in offenders:
    print(path)
sys.exit(1 if offenders else 0)
'); then
	echo "Staged files contain CJK characters outside README.zh.md and site/:"
	printf '%s\n' "$hits"
	echo "Use English outside the maintained Chinese README and bilingual site; see CONTRIBUTING.md."
	exit 1
fi
