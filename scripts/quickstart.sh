#!/usr/bin/env bash
#
# Brings up OrbitJob on a local kind cluster.
#
# Every step below is one that has been run end to end; the docs in
# docs/local-deployment.md follow the same order. If a step starts failing,
# fix it here first and then update the doc.
#
# Prerequisites: docker, kind, kubectl, helm, make, go, nc (netcat).

set -euo pipefail

readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly CLUSTER_NAME="orbitjob-dev"
readonly NAMESPACE="orbitjob-system"
readonly RELEASE="orbitjob"
readonly TAG="dev"
# The single source for everything the chart reads: the dev tag, the Never
# pull policy, and the operator.namespaceTenants mapping. Comma-separated
# values must be changed there or with -f, never with --set.
readonly VALUES_FILE="deploy/kind/values-dev.yaml"
readonly ADMIN_PORT="18080"
# Budget for each per-deployment rollout status wait in restart_deployments.
readonly ROLLOUT_TIMEOUT="5m"

log() { printf '[quickstart] %s\n' "$*" >&2; }
ok() { printf '[quickstart] [OK] %s\n' "$*" >&2; }
warn() { printf '[quickstart] WARN %s\n' "$*" >&2; }
die() { printf '[quickstart] [FAIL] %s\n' "$*" >&2; exit 1; }

check_prerequisites() {
    log "Step 1/7: checking prerequisites"
    local missing=()
    for cmd in docker kind kubectl helm make go nc; do
        command -v "$cmd" >/dev/null || missing+=("$cmd")
    done
    [[ ${#missing[@]} -eq 0 ]] || die "missing commands: ${missing[*]}"
    docker info >/dev/null 2>&1 || die "docker is not running"
    ok "prerequisites present"
}

create_cluster() {
    log "Step 2/7: ensuring kind cluster $CLUSTER_NAME"
    make kind-up
    ok "kind cluster $CLUSTER_NAME ready"
}

prepare_database() {
    log "Step 3/7: preparing PostgreSQL and the installation Secret"
    make kind-db
    ok "database and installation Secret ready"
}

build_images() {
    log "Step 4/7: building images and loading them into kind"
    make docker-build TAG="$TAG"
    make kind-load TAG="$TAG"
    ok "images built and loaded"
}

install_chart() {
    log "Step 5/7: installing the chart"
    # The chart references images by the tag in the values file, and the
    # Never pull policy turns a mismatch with the TAG built above into pods
    # that can never start, so the two must be pinned together.
    grep -q "^[[:space:]]*imageTag:[[:space:]]*$TAG\$" "$PROJECT_ROOT/$VALUES_FILE" \
        || die "$VALUES_FILE does not set global.imageTag: $TAG (must match the tag the images were built with)"
    helm upgrade --install "$RELEASE" ./charts/orbitjob \
        --namespace "$NAMESPACE" \
        -f "$VALUES_FILE" \
        --wait --timeout=10m || die "helm upgrade failed"
    ok "release $RELEASE deployed from $VALUES_FILE"
}

# The dev images are rebuilt in place under the same :dev tag, so a helm
# upgrade with otherwise unchanged values leaves the pod templates untouched
# and the pods keep running whatever image content was loaded into kind under
# that tag before the rebuild. Restarting the deployments recreates the pods,
# which then resolve the freshly loaded images; without this, quickstart can
# report success while the cluster still serves the previous build.
restart_deployments() {
    log "Step 6/7: restarting deployments to pick up the freshly built $TAG image"
    local deploy
    for deploy in orbitjob-admin-api orbitjob-scheduler orbitjob-operator; do
        kubectl -n "$NAMESPACE" rollout restart "deployment/$deploy" \
            || die "rollout restart failed for $deploy"
        kubectl -n "$NAMESPACE" rollout status "deployment/$deploy" \
            --timeout="$ROLLOUT_TIMEOUT" || die "rollout of $deploy did not complete"
        ok "$deploy restarted and ready"
    done
}

report() {
    log "Step 7/7: exporting credentials"
    mkdir -p .kind
    # The redirect creates the file with the default umask, so chmod after the
    # fact leaves the API key world-readable in between. umask 077 makes it 0600
    # from the moment it exists.
    (
        umask 077
        make kind-env >.kind/env.sh
    )

    cat <<EOF

OrbitJob is running.

  source .kind/env.sh
  kubectl -n $NAMESPACE port-forward svc/orbitjob-admin-api $ADMIN_PORT:8080 &
  curl -H "Authorization: Bearer \$ORBITJOB_API_KEY" http://localhost:$ADMIN_PORT/api/v1/tenants

Credentials: .kind/env.sh
Guide:       docs/local-deployment.md

EOF
    ok "quickstart complete"
}

main() {
    cd "$PROJECT_ROOT"
    check_prerequisites
    create_cluster
    prepare_database
    build_images
    install_chart
    restart_deployments
    report
}

main "$@"
