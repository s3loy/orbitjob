#!/usr/bin/env bash
#
# Prepares the database side of a local kind installation.
#
# The chart needs two things that `make kind-up` does not provide: a running
# PostgreSQL, and an `orbitjob-database` Secret holding the eight keys every
# Pod reads. Without the Secret the Helm hooks fail with
# CreateContainerConfigError and the release lands in `failed`. This script
# fills both gaps for local development.
#
# It is idempotent. Re-running reuses the PostgreSQL Deployment and keeps the
# existing bootstrap API key, so an exported ORBITJOB_API_KEY stays valid.

set -euo pipefail

readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly RELEASE_NAMESPACE="${ORBITJOB_NAMESPACE:-orbitjob-system}"
readonly DB_NAMESPACE="${ORBITJOB_DB_NAMESPACE:-orbitjob}"
readonly DB_DEPLOYMENT="orbitjob-postgres"
readonly LOCAL_PORT="${ORBITJOB_DB_PORT:-15432}"
readonly SECRET_NAME="orbitjob-database"
# The kind installation is its own database, so it keeps its own state. Sharing
# `.runtime/database.json` collides with the bundled local setup: configure
# refuses to switch an existing installation from bundled to external.
readonly RUNTIME_DIR="${PROJECT_ROOT}/.runtime/kind"

log() { printf '[kind-db] %s\n' "$*" >&2; }
die() { printf '[kind-db] error: %s\n' "$*" >&2; exit 1; }

for command in kubectl go nc openssl shasum; do
    command -v "$command" >/dev/null || die "$command is required"
done

kubectl get deployment "$DB_DEPLOYMENT" -n "$DB_NAMESPACE" >/dev/null 2>&1 || {
    log "deploying PostgreSQL into $DB_NAMESPACE"
    kubectl apply -f "$PROJECT_ROOT/deploy/kind/postgres-17.yaml"
}

log "waiting for $DB_DEPLOYMENT"
kubectl rollout status "deployment/$DB_DEPLOYMENT" -n "$DB_NAMESPACE" --timeout=3m

port_forward_pid=
cleanup() {
    [[ -z ${port_forward_pid:-} ]] || {
        kill "$port_forward_pid" 2>/dev/null || true
        # Reap the job, otherwise the shell prints "Terminated: 15" over the
        # script's own output.
        wait "$port_forward_pid" 2>/dev/null || true
    }
}
trap cleanup EXIT

port_forward_log="$(mktemp)"
kubectl port-forward -n "$DB_NAMESPACE" "deployment/$DB_DEPLOYMENT" \
    "${LOCAL_PORT}:5432" >"$port_forward_log" 2>&1 &
port_forward_pid=$!

ready=false
for _ in $(seq 1 30); do
    kill -0 "$port_forward_pid" 2>/dev/null || { cat "$port_forward_log" >&2; die "port-forward exited"; }
    if nc -z 127.0.0.1 "$LOCAL_PORT"; then ready=true; break; fi
    sleep 1
done
rm -f "$port_forward_log"
[[ $ready == true ]] || die "PostgreSQL port-forward on $LOCAL_PORT did not become ready"

mkdir -p "$RUNTIME_DIR"
umask 077

# The image's POSTGRES_USER is always a superuser, and nothing OrbitJob runs
# needs one. Owner-init needs CREATEROLE; owning the database covers the schema
# grants, and pgcrypto is a trusted extension so the database owner can create
# it. Using the image account would put a superuser DSN into the installation
# Secret, where it would sit far longer than the one bootstrap that needs it.
#
# The password is generated once and kept: the Secret is derived from this DSN,
# so a new password on every run would invalidate the deployed release.
BOOTSTRAP_ROLE="orbitjob_bootstrap"
BOOTSTRAP_PASSWORD_FILE="$RUNTIME_DIR/bootstrap-password"
BOOTSTRAP_PASSWORD=""
[[ -f "$BOOTSTRAP_PASSWORD_FILE" ]] && BOOTSTRAP_PASSWORD="$(cat "$BOOTSTRAP_PASSWORD_FILE")"
if [[ -z $BOOTSTRAP_PASSWORD ]]; then
    BOOTSTRAP_PASSWORD="$(openssl rand -hex 24)"
    printf '%s' "$BOOTSTRAP_PASSWORD" >"$BOOTSTRAP_PASSWORD_FILE"
fi

psql_remote() {
    kubectl exec -n "$DB_NAMESPACE" "deployment/$DB_DEPLOYMENT" -- \
        env PGPASSWORD=orbitjob-owner psql -U postgres -d orbitjob -Atqc "$1"
}

log "provisioning the bootstrap role ($BOOTSTRAP_ROLE)"
if [[ "$(psql_remote "SELECT count(*) FROM pg_roles WHERE rolname='$BOOTSTRAP_ROLE'")" == "0" ]]; then
    psql_remote "CREATE ROLE $BOOTSTRAP_ROLE LOGIN NOSUPERUSER CREATEROLE NOBYPASSRLS PASSWORD '$BOOTSTRAP_PASSWORD'" >/dev/null
else
    # Re-assert the password: a rebuilt database would keep the state file but
    # lose the role's password.
    psql_remote "ALTER ROLE $BOOTSTRAP_ROLE PASSWORD '$BOOTSTRAP_PASSWORD'" >/dev/null
fi
psql_remote "ALTER DATABASE orbitjob OWNER TO $BOOTSTRAP_ROLE" >/dev/null

# CREATEROLE only lets a role manage roles it has ADMIN on. On a database
# where the OrbitJob roles were created by the image's superuser, the bootstrap
# role starts with no hold on them and cannot rotate their passwords. Granting
# ADMIN makes the switch from a superuser install a one-time step rather than a
# rebuild. On a fresh database the roles do not exist yet and the bootstrap role
# creates them itself, which confers ADMIN automatically.
psql_remote "
DO \$\$
DECLARE target text;
BEGIN
  FOREACH target IN ARRAY ARRAY[
    'orbitjob_owner', 'orbitjob_table_owner', 'orbitjob_migrator',
    'orbitjob_admin', 'orbitjob_runtime', 'orbitjob_operator'
  ] LOOP
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = target) THEN
      -- Already a member: re-granting only prints a NOTICE and changes
      -- nothing, so check first.
      IF NOT EXISTS (
        SELECT 1 FROM pg_auth_members am
        JOIN pg_roles m ON m.oid = am.member
        JOIN pg_roles p ON p.oid = am.roleid
        WHERE m.rolname = '$BOOTSTRAP_ROLE' AND p.rolname = target
      ) THEN
        EXECUTE format('GRANT %I TO $BOOTSTRAP_ROLE WITH ADMIN OPTION', target);
      END IF;
    END IF;
  END LOOP;
END
\$\$
" >/dev/null

dsn_file="$(mktemp)"
trap 'cleanup; rm -f "$dsn_file"' EXIT
printf 'postgres://%s:%s@127.0.0.1:%s/orbitjob?sslmode=disable\n' \
    "$BOOTSTRAP_ROLE" "$BOOTSTRAP_PASSWORD" "$LOCAL_PORT" >"$dsn_file"

log "generating installation credentials"
go -C "$PROJECT_ROOT" run ./cmd/configure setup \
    --mode external \
    --database-dsn-file "$dsn_file" \
    --state "$RUNTIME_DIR/database.json" \
    --runtime-env "$RUNTIME_DIR/database.env"

set -a
# shellcheck disable=SC1091
. "$RUNTIME_DIR/database.env"
set +a

for key in BOOTSTRAP_OWNER_DSN MIGRATOR_DSN ADMIN_DSN RUNTIME_DSN \
           MIGRATOR_PASSWORD ADMIN_PASSWORD RUNTIME_PASSWORD; do
    [[ -n ${!key:-} ]] || die "$key was not generated"
done

# Reuse the existing key when the Secret is already there. Regenerating would
# silently invalidate every client holding the old one.
api_key="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data.bootstrap-api-key}' 2>/dev/null | base64 -d || true)"
if [[ -z $api_key ]]; then
    api_key="otj_$(openssl rand -hex 24)"
    log "generated a new bootstrap API key"
else
    log "keeping the existing bootstrap API key"
fi

# The generated DSNs point at the port-forward. Pods reach PostgreSQL over the
# in-cluster Service instead, so the host is rewritten on the way in.
in_cluster="${DB_DEPLOYMENT}.${DB_NAMESPACE}.svc:5432"

# The Secret has to exist before `helm install`, which is also what would
# otherwise create this namespace.
kubectl create namespace "$RELEASE_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - >/dev/null

# A Secret change does not restart the Pods that read it, and the chart cannot
# hash a Secret it does not own, so a rotation would leave every workload
# authenticating with the old password until someone noticed. Capture the
# current contents to compare after the write.
secret_before="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data}' 2>/dev/null | shasum -a 256 | cut -d' ' -f1 || true)"

kubectl create secret generic "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    --from-literal=bootstrap-owner-dsn="${BOOTSTRAP_OWNER_DSN/127.0.0.1:${LOCAL_PORT}/${in_cluster}}" \
    --from-literal=migrator-dsn="${MIGRATOR_DSN/127.0.0.1:${LOCAL_PORT}/${in_cluster}}" \
    --from-literal=admin-dsn="${ADMIN_DSN/127.0.0.1:${LOCAL_PORT}/${in_cluster}}" \
    --from-literal=runtime-dsn="${RUNTIME_DSN/127.0.0.1:${LOCAL_PORT}/${in_cluster}}" \
    --from-literal=migrator-password="$MIGRATOR_PASSWORD" \
    --from-literal=admin-password="$ADMIN_PASSWORD" \
    --from-literal=runtime-password="$RUNTIME_PASSWORD" \
    --from-literal=bootstrap-api-key="$api_key" \
    --dry-run=client -o yaml | kubectl apply -f - >/dev/null

secret_after="$(kubectl get secret "$SECRET_NAME" -n "$RELEASE_NAMESPACE" \
    -o jsonpath='{.data}' | shasum -a 256 | cut -d' ' -f1)"
if [[ -n $secret_before && $secret_before != "$secret_after" ]] &&
    kubectl get deployment -n "$RELEASE_NAMESPACE" -l app.kubernetes.io/name >/dev/null 2>&1; then
    log "credentials changed; rolling the workloads that read them"
    kubectl rollout restart deployment -n "$RELEASE_NAMESPACE" -l app.kubernetes.io/name >/dev/null
    for deployment in $(kubectl get deployment -n "$RELEASE_NAMESPACE" -l app.kubernetes.io/name -o name); do
        kubectl rollout status -n "$RELEASE_NAMESPACE" "$deployment" --timeout=3m >/dev/null
    done
fi

log "Secret $SECRET_NAME is ready in $RELEASE_NAMESPACE"
log "PostgreSQL: $in_cluster (namespace $DB_NAMESPACE)"
log "API key: kubectl get secret $SECRET_NAME -n $RELEASE_NAMESPACE -o jsonpath='{.data.bootstrap-api-key}' | base64 -d"
