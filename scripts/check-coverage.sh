#!/usr/bin/env bash
# Coverage gate. The merge bar is defined in CONTRIBUTING.md.
#
#   Business logic and stores under core|admin           60%
#   Infrastructure and adapters                          no numeric gate
#
# The set of packages this gate checks comes from `go list ./...`, never from
# the coverage profile (ADR 0007). A profile-derived set can only report on the
# packages the profile happens to name, so a package that contributes no line
# would drop out of the check set without a word; a package declaring no
# statements contributes no line at all. Here the tier set comes from go list,
# an empty check set is a failure, and a tier package the profile never
# mentions fails instead of disappearing.
#
# scripts/coverage-baseline.txt lists declaration-only packages for which Go
# emits no measurable statements. Every entry must carry a rationale, name a
# current tier package, and pass its own coverage probe with zero statements.
set -euo pipefail

COVER_FILE="${1:-coverage.out}"
BASELINE_FILE="${BASELINE_FILE:-scripts/coverage-baseline.txt}"
CORE_MIN=60
IO_MIN=60

if [[ ! -f "$COVER_FILE" ]]; then
	printf 'coverage profile not found: %s\n' "$COVER_FILE" >&2
	printf 'run: go test -coverprofile=%s ./...\n' "$COVER_FILE" >&2
	exit 1
fi

if ! MODULE="$(go list -m)"; then
	printf 'go list -m failed; the check set cannot be derived\n' >&2
	exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# The independent source of the check set. Whether a package carries test files
# comes from go list too, so the report can tell "the profile is stale" apart
# from "the package has no tests".
if ! go list -f '{{.ImportPath}} {{if or .TestGoFiles .XTestGoFiles}}tests{{else}}notests{{end}}' ./... > "$WORK_DIR/packages" 2>&1; then
	printf 'go list ./... failed; the check set is unknown:\n' >&2
	cat "$WORK_DIR/packages" >&2
	exit 1
fi

# Tier matching runs on the module-relative path and is anchored at its start.
# The earlier form matched "/internal/(core|admin)/..." against the full import
# path, which required a slash before "internal" and matched nothing at all if
# the module path changed shape; every package then took the "not in a tier"
# branch and the gate went green over zero packages (ADR 0007). Anchoring at
# "^internal/" is exact for any module path, because the module prefix is
# stripped first rather than relied upon to supply a separator.
awk -v module="$MODULE" '
function tier_of(relative) {
	if (relative ~ /^internal\/(core|admin)\/(domain|app)(\/|$)/) return "core"
	if (relative ~ /^internal\/(core|admin)\/store(\/|$)/)       return "io"
	return ""
}
BEGIN { prefix = module "/" }
{
	import_path = $1
	if (index(import_path, prefix) != 1) {
		printf "package outside module %s: %s\n", module, import_path > "/dev/stderr"
		unexpected = 1
		next
	}
	tier = tier_of(substr(import_path, length(prefix) + 1))
	if (tier != "") print tier "\t" import_path "\t" $2
}
END { if (unexpected) exit 1 }
' "$WORK_DIR/packages" > "$WORK_DIR/expected"

: > "$WORK_DIR/baseline"
if [[ -f "$BASELINE_FILE" ]]; then
	if ! awk -v expected="$WORK_DIR/expected" '
	function trim(value) {
		sub(/^[[:space:]]+/, "", value)
		sub(/[[:space:]]+$/, "", value)
		return value
	}
	BEGIN {
		while ((getline line < expected) > 0) {
			split(line, field, "\t")
			in_tier[field[2]] = 1
		}
		close(expected)
	}
	/^[[:space:]]*($|#)/ { next }
	{
		marker = index($0, "#")
		if (marker == 0) {
			printf "coverage exception missing an inline rationale: %s\n", $0 > "/dev/stderr"
			invalid = 1
			next
		}
		package_path = trim(substr($0, 1, marker - 1))
		rationale = trim(substr($0, marker + 1))
		if (package_path == "" || package_path ~ /[[:space:]]/ || rationale == "") {
			printf "coverage exception missing an inline rationale: %s\n", $0 > "/dev/stderr"
			invalid = 1
			next
		}
		if (!(package_path in in_tier)) {
			printf "coverage exception is not a current coverage-tier package: %s\n", package_path > "/dev/stderr"
			invalid = 1
			next
		}
		if (seen[package_path]++) {
			printf "duplicate coverage exception: %s\n", package_path > "/dev/stderr"
			invalid = 1
			next
		}
		print package_path
	}
	END { if (invalid) exit 1 }
	' "$BASELINE_FILE" > "$WORK_DIR/baseline"; then
		exit 1
	fi
fi

# An exception self-expires as soon as its package gains executable code. A
# package-local probe also distinguishes a genuine declaration-only package
# from one merely omitted by a stale or partial aggregate profile.
exception_number=0
while IFS= read -r package_path; do
	[[ -z "$package_path" ]] && continue
	exception_number=$((exception_number + 1))
	exception_profile="$WORK_DIR/exception-$exception_number.out"
	exception_log="$WORK_DIR/exception-$exception_number.log"
	if ! go test -coverprofile="$exception_profile" "$package_path" > "$exception_log" 2>&1; then
		printf 'coverage exception probe failed for %s:\n' "$package_path" >&2
		cat "$exception_log" >&2
		exit 1
	fi
	if awk '$1 != "mode:" && NF { found = 1 } END { exit found ? 0 : 1 }' "$exception_profile"; then
		printf 'coverage exception has measurable statements: %s\n' "$package_path" >&2
		exit 1
	fi
done < "$WORK_DIR/baseline"

awk -v core_min="$CORE_MIN" -v io_min="$IO_MIN" -v cover_file="$COVER_FILE" \
	-v expected="$WORK_DIR/expected" -v baseline="$WORK_DIR/baseline" '
BEGIN {
	while ((getline line < baseline) > 0) if (line != "") exempt[line] = 1
	close(baseline)
	while ((getline line < expected) > 0) {
		if (line == "") continue
		split(line, field, "\t")
		tier_of_package[field[2]] = field[1]
		has_tests[field[2]] = (field[3] == "tests")
		total++
	}
	close(expected)
}
$1 == "mode:" { next }
{
	rows++
	split($1, location, ":")
	n = split(location[1], parts, "/")
	pkg = parts[1]
	for (i = 2; i < n; i++) pkg = pkg "/" parts[i]
	stmts[pkg]   += $2
	covered[pkg] += ($3 + 0 > 0) ? $2 : 0
}
END {
	if (total == 0) {
		# Nothing matched a tier, so nothing would be verified. An empty
		# check set is a fault signal, not a pass (ADR 0007).
		printf "coverage gate failed: go list found no package in a coverage tier\n"
		printf "checked 0 of 0 tier package(s)\n"
		exit 1
	}
	if (rows == 0) {
		# A profile with nothing but its "mode:" header carries no
		# measurement for any package, so there is nothing to pass.
		printf "coverage gate failed: %s holds no coverage data\n", cover_file
		printf "checked 0 of %d tier package(s)\n", total
		exit 1
	}
	for (pkg in tier_of_package) order[++count] = pkg
	# Insertion sort keeps the report stable across awk implementations.
	for (i = 2; i <= count; i++) {
		key = order[i]
		for (j = i - 1; j >= 1 && order[j] > key; j--) order[j + 1] = order[j]
		order[j + 1] = key
	}
	failed = 0; warned = 0
	for (i = 1; i <= count; i++) {
		pkg = order[i]
		min = (tier_of_package[pkg] == "core") ? core_min : io_min
		if (!(pkg in stmts)) {
			# No profile line for this package. go list says whether it has
			# test files, which separates the two causes: a package with
			# tests always contributes data lines, so silence there means a
			# stale or partial profile; a package without tests that also
			# declares no statements has nothing to instrument and can never
			# appear, no matter how many tests are added. Neither is a pass.
			if (pkg in exempt) {
				printf "WARN  %-62s %6s  (no measurable statements, documented exception)\n", pkg, "n/a"
				warned++
				continue
			}
			reason = has_tests[pkg] ? "no data for a package that has test files, profile stale?" \
			                        : "no coverage data and no test files"
			printf "FAIL  %-62s %6s  (min %d%%, %s)\n", pkg, "n/a", min, reason
			failed++
			continue
		}
		pct = covered[pkg] * 100 / stmts[pkg]
		if (pct + 0.049 >= min) continue
		printf "FAIL  %-62s %6.1f%%  (min %d%%)\n", pkg, pct, min
		failed++
	}
	printf "\nchecked %d of %d tier package(s)\n", count, total
	if (warned > 0) printf "%d package(s) with documented exceptions\n", warned
	if (failed > 0) {
		printf "%d package(s) failing without a documented exception\n", failed
		exit 1
	}
	printf "coverage gate passed\n"
}
' "$COVER_FILE"
