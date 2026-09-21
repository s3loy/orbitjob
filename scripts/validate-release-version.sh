#!/usr/bin/env bash

# Validate the tag before any release artifact is published. Helm requires a
# SemVer chart version; rejecting incompatible tags here prevents a workflow
# from pushing images and failing only when the chart is packaged.

set -euo pipefail

version=${1:-}
prerelease_id='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
semver="^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-${prerelease_id}(\\.${prerelease_id})*)?$"

if [[ ! $version =~ $semver ]]; then
  printf 'tag must be v-prefixed SemVer, got: %s\n' "$version" >&2
  exit 1
fi
