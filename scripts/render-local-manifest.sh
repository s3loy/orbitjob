#!/usr/bin/env bash
# Render the namespace overrides supported by local installation/load tools.
set -euo pipefail

readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
kind="${1:-}"
target_namespace="${2:-}"
if [[ ${#target_namespace} -gt 63 || ! $target_namespace =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]]; then
    printf 'invalid Kubernetes namespace: %s\n' "$target_namespace" >&2
    exit 1
fi

case "$kind" in
    kind-db)
        sed -e "s/^  name: orbitjob$/  name: $target_namespace/" \
            -e "s/^  namespace: orbitjob$/  namespace: $target_namespace/" \
            "$PROJECT_ROOT/deploy/kind/postgres-17.yaml"
        ;;
    load-operations)
        # The task ServiceAccount remains in orbitjob-tasks. Only its grants
        # follow the control plane's installation namespace.
        sed "s/^  namespace: orbitjob-system$/  namespace: $target_namespace/" \
            "$PROJECT_ROOT/deploy/load/operations-rbac.yaml"
        ;;
    *)
        printf 'unknown local manifest: %s\n' "$kind" >&2
        exit 1
        ;;
esac
