#!/usr/bin/env bash

# Validate the tag before any release artifact is published. Helm requires a
# SemVer chart version; rejecting incompatible tags here prevents a workflow
# from pushing images and failing only when the chart is packaged.

set -euo pipefail

version=${1:-}
chart=${2:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/charts/orbitjob/Chart.yaml}
prerelease_id='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
semver="^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-${prerelease_id}(\\.${prerelease_id})*)?$"

if [[ ! $version =~ $semver ]]; then
  printf 'tag must be v-prefixed SemVer, got: %s\n' "$version" >&2
  exit 1
fi

# These fields are top-level scalar values in the source chart. Require exactly
# one of each and reject aliases, folded values and malformed quoting. Helm
# separately validates the complete chart before any image can be published.
chart_value() {
  awk -v key="$1" '
    $0 ~ "^" key ":[[:space:]]*" {
      count++
      value = $0
      sub("^[^:]*:[[:space:]]*", "", value)
      sub(/[[:space:]]+#.*$/, "", value)
      sub(/[[:space:]]*$/, "", value)
      quote = substr(value, 1, 1)
      if (quote == "\"" || quote == "\047") {
        if (length(value) < 2 || substr(value, length(value), 1) != quote) exit 1
        value = substr(value, 2, length(value) - 2)
      }
      if (value !~ /^[0-9A-Za-z.+-]+$/) exit 1
      result = value
    }
    END { if (count != 1) exit 1; print result }
  ' "$chart"
}

for field in version appVersion; do
  if ! actual=$(chart_value "$field"); then
    printf 'chart must define one scalar %s: %s\n' "$field" "$chart" >&2
    exit 1
  fi
  if [[ $actual != "${version#v}" ]]; then
    printf 'release tag %s does not match chart %s %s: %s\n' "$version" "$field" "$actual" "$chart" >&2
    exit 1
  fi
done
