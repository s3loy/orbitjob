#!/usr/bin/env bash
set -euo pipefail

cluster=${KIND_CLUSTER_NAME:-orbitjob-verify}
namespace=orbitjob
release=orbitjob
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tag=${ORBITJOB_IMAGE_TAG:-dev}
created_cluster=false
workdir=
dsn_file=
port_forward_pid=

assert_eq() {
  # An empty actual is "the check could not run", not "the check passed": a
  # failed psql or an unset value must not compare equal to anything.
  if [[ -z $1 ]]; then
    printf 'assertion failed: %s produced no value; absence is not success\n' "$3" >&2
    exit 1
  fi
  if [[ $1 != "$2" ]]; then
    printf 'assertion failed: %s\n  expected: %s\n  actual:   %s\n' "$3" "$2" "$1" >&2
    exit 1
  fi
}

# Every expectation below is derived from the repository, never copied into the
# script: a hardcoded count, version, or name goes stale the moment a migration
# is added, and the gate then fails for a reason it does not name (ADR 0007).
# db/migrations is the single source; the loader
# (internal/platform/migrate/load.go) parses the same filenames.
migration_dir="$root/db/migrations"
migration_versions=()
migration_names=()
while IFS= read -r migration_path; do
  migration_file=$(basename "$migration_path")
  # Same shape the loader accepts (internal/platform/migrate/load.go): a
  # filename it would reject must stop the script, not become a failed
  # expectation later.
  if [[ ! $migration_file =~ ^[0-9]{4}_[a-z0-9_]+\.up\.sql$ ]]; then
    printf 'migration filename the loader would reject: %s\n' "$migration_file" >&2
    exit 1
  fi
  migration_version=${migration_file%%_*}
  migration_name=${migration_file#"${migration_version}"_}
  migration_name=${migration_name%.up.sql}
  # Base 10: a leading zero would otherwise make bash read 0008 as octal.
  migration_versions+=("$((10#$migration_version))")
  migration_names+=("$migration_name")
done < <(find "$migration_dir" -maxdepth 1 -type f -name '*.up.sql' | LC_ALL=C sort)

migration_count=${#migration_versions[@]}
# Guard the guard. An expectation the script cannot derive is a failure signal,
# not a reason to skip the assertions that depend on it (ADR 0007 rule 2): a
# zero count or a version gap would make every downstream comparison wrong.
if ((migration_count == 0)); then
  printf 'derived no migrations from %s; cannot verify schema_migrations\n' "$migration_dir" >&2
  exit 1
fi
for i in "${!migration_versions[@]}"; do
  if ((migration_versions[i] != i + 1)); then
    printf 'migration versions must be contiguous from 0001: expected %04d, got %04d (%s)\n' \
      "$((i + 1))" "${migration_versions[i]}" "${migration_names[i]}" >&2
    exit 1
  fi
done
last_index=$((migration_count - 1))
expected_max=${migration_versions[$last_index]}

# The roles that must be hardened are named in one place: the CREATE ROLE
# statements in internal/platform/migrate/roles.go. Deriving them here keeps the
# assertion from drifting with a role rename, and keeps it off roles the
# repository does not manage.
roles_source="$root/internal/platform/migrate/roles.go"
managed_roles=()
while IFS= read -r role_name; do
  if [[ -n $role_name ]]; then
    managed_roles+=("$role_name")
  fi
done < <(grep -oE 'CREATE ROLE orbitjob_[a-z_]+' "$roles_source" | awk '{print $3}' | LC_ALL=C sort -u)
if ((${#managed_roles[@]} == 0)); then
  printf 'derived no role names from %s; cannot verify role hardening\n' "$roles_source" >&2
  exit 1
fi
managed_roles_sql=$(printf "'%s'," "${managed_roles[@]}")
managed_roles_sql=${managed_roles_sql%,}

cleanup() {
  status=$?
  if ((status != 0)); then
    kubectl get pods,jobs -A 2>/dev/null || true
    helm status "$release" -n "$namespace" 2>/dev/null || true
    kubectl logs -n "$namespace" -l job-name --all-containers --tail=100 2>/dev/null || true
  fi
  [[ -z ${workdir:-} ]] || rm -rf "$workdir"
  [[ -z ${config_dir:-} ]] || rm -rf "$config_dir"
  [[ -z ${dsn_file:-} ]] || rm -f "$dsn_file"
  [[ -z ${port_forward_pid:-} ]] || kill "$port_forward_pid" 2>/dev/null || true
  if [[ $created_cluster == true && $status == 0 ]]; then
    kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
  elif [[ $created_cluster == true ]]; then
    printf 'kind cluster %s retained for failure diagnosis\n' "$cluster" >&2
  fi
  exit "$status"
}
trap cleanup EXIT

require() { command -v "$1" >/dev/null || { printf '%s is required\n' "$1" >&2; exit 1; }; }
for command in kind kubectl helm docker openssl go nc; do require "$command"; done

docker info >/dev/null
if ! kind get clusters | grep -qx "$cluster"; then
  kind create cluster --name "$cluster" --wait 5m
  created_cluster=true
fi
kubectl config use-context "kind-$cluster" >/dev/null
kubectl apply -f "$root/deploy/kind/postgres-17.yaml"
kubectl rollout status deployment/orbitjob-postgres -n "$namespace" --timeout=3m

umask 077
config_dir=$(mktemp -d "${TMPDIR:-/tmp}/orbitjob-verify-config.XXXXXX")
dsn_file=$(mktemp "${TMPDIR:-/tmp}/orbitjob-verify-dsn.XXXXXX")
printf '%s\n' 'postgres://postgres:orbitjob-owner@orbitjob-postgres.orbitjob.svc:5432/orbitjob?sslmode=disable' >"$dsn_file"
# Port-forward lets the same configure command initialize an external database
# without teaching this script how to generate role-specific DSNs. A foreign
# listener on the port would satisfy the readiness probe while this port-forward
# never bound, so the check below refuses to start and the liveness check after
# the loop refuses to continue unless the process we started is the one serving.
if nc -z 127.0.0.1 15432; then
  printf 'port 15432 is already in use; refusing to configure a foreign PostgreSQL\n' >&2
  exit 1
fi
kubectl port-forward -n "$namespace" deployment/orbitjob-postgres 15432:5432 >"$config_dir/port-forward.log" 2>&1 &
port_forward_pid=$!
ready=false
for _ in $(seq 1 30); do
  if ! kill -0 "$port_forward_pid" 2>/dev/null; then
    cat "$config_dir/port-forward.log" >&2
    exit 1
  fi
  if nc -z 127.0.0.1 15432; then ready=true; break; fi
  sleep 1
done
[[ $ready == true ]] || { printf 'PostgreSQL port-forward did not become ready\n' >&2; exit 1; }
if ! kill -0 "$port_forward_pid" 2>/dev/null; then
  cat "$config_dir/port-forward.log" >&2
  printf 'port-forward exited before the database was configured\n' >&2
  exit 1
fi
printf '%s\n' 'postgres://postgres:orbitjob-owner@127.0.0.1:15432/orbitjob?sslmode=disable' >"$dsn_file"
go -C "$root" run ./cmd/configure setup --mode external --database-dsn-file "$dsn_file" --state "$config_dir/database.json" --runtime-env "$config_dir/database.env"
set -a
# shellcheck disable=SC1090
. "$config_dir/database.env"
set +a
kubectl create secret generic orbitjob-database -n "$namespace" \
  --from-literal=bootstrap-owner-dsn="${BOOTSTRAP_OWNER_DSN/127.0.0.1:15432/orbitjob-postgres.orbitjob.svc:5432}" \
  --from-literal=migrator-dsn="${MIGRATOR_DSN/127.0.0.1:15432/orbitjob-postgres.orbitjob.svc:5432}" \
  --from-literal=admin-dsn="${ADMIN_DSN/127.0.0.1:15432/orbitjob-postgres.orbitjob.svc:5432}" \
  --from-literal=runtime-dsn="${RUNTIME_DSN/127.0.0.1:15432/orbitjob-postgres.orbitjob.svc:5432}" \
  --from-literal=migrator-password="$MIGRATOR_PASSWORD" \
  --from-literal=admin-password="$ADMIN_PASSWORD" \
  --from-literal=runtime-password="$RUNTIME_PASSWORD" \
  --from-literal=bootstrap-api-key="otj_$(openssl rand -hex 24)" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kill "$port_forward_pid" 2>/dev/null || true
wait "$port_forward_pid" 2>/dev/null || true
port_forward_pid=

helm_args=(
  -n "$namespace" -f "$root/deploy/kind/values-local.yaml"
  # One tag and one pull policy for every component: the script loads each
  # image locally, so a pull would only mask a missing build.
  --set-string global.imageTag="$tag"
  --set-string global.imagePullPolicy=Never
  # The operator reads tenancy from configuration. Everything in this script
  # lives in one namespace, so the bootstrap tenant owns it.
  --set-string "operator.namespaceTenants=orbitjob=00000000000000000000000001"
)

# Build exactly the images the chart deploys. The rendered manifests are the
# single source, so a component added to or removed from the chart is built or
# skipped here without a second list to keep in step. The repository-to-Docker-
# target mapping is the Dockerfile's interface.
rendered_images=$(helm template "$release" "$root/charts/orbitjob" "${helm_args[@]}" |
  awk '/^[[:space:]]*image:[[:space:]]/ {print $2}' | tr -d '"' | sed 's/:[^:]*$//' | LC_ALL=C sort -u)
if [[ -z $rendered_images ]]; then
  printf 'the chart rendered no images; nothing to build\n' >&2
  exit 1
fi
while IFS= read -r image; do
  [[ -n $image ]] || continue
  case "$image" in
    orbitjob-admin-api) target="admin" ;;
    orbitjob-*) target=${image#orbitjob-} ;;
    *)
      printf 'cannot map image %s to a Docker build target\n' "$image" >&2
      exit 1
      ;;
  esac
  docker build --target "$target" -t "$image:$tag" "$root"
  kind load docker-image --name "$cluster" "$image:$tag"
done <<<"$rendered_images"

helm upgrade --install "$release" "$root/charts/orbitjob" "${helm_args[@]}" \
  --wait --wait-for-jobs --timeout 8m

psql_exec() {
  kubectl exec -n "$namespace" deployment/orbitjob-postgres -- env PGPASSWORD=orbitjob-owner \
    psql -U postgres -d orbitjob -Atqc "$1"
}

# schema_migrations must describe exactly the migrations in db/migrations. The
# row count plus a name check per derived version pins the set both ways: a
# missing row fails the count, an unexpected row fails the per-version lookup.
assert_eq "$(psql_exec 'SELECT count(*) FROM schema_migrations')" "$migration_count" \
  'schema_migrations holds one row per migration in db/migrations'
assert_eq "$(psql_exec 'SELECT max(version) FROM schema_migrations')" "$expected_max" \
  'schema_migrations highest version'
for i in "${!migration_versions[@]}"; do
  assert_eq "$(psql_exec "SELECT name FROM schema_migrations WHERE version=${migration_versions[i]}")" \
    "${migration_names[i]}" "migration ${migration_versions[i]} carries its file's name"
done

# Every role the migrator hardens must exist and carry no elevated attribute.
assert_eq "$(psql_exec "SELECT count(*) FROM pg_roles WHERE rolname IN ($managed_roles_sql)")" "${#managed_roles[@]}" \
  'every role the migrator creates exists'
assert_eq "$(psql_exec "SELECT count(*) FROM pg_roles WHERE rolname IN ($managed_roles_sql) AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolinherit)")" 0 \
  'every role the migrator creates is hardened'

# Tenant isolation is derived from the catalog, not from a table-name list that
# a new tenant-owned table would leave behind. These are the two rules
# db/migrations/0001_baseline.up.sql states and enforces on itself: rule 1 is
# partition closure, rule 2 is "a tenant-owned relation has RLS", where a
# relation is tenant-owned because it has a tenant_id column (tenants is keyed
# by id, so it is named). Re-deriving them here proves a live database matches
# the contract the migrations claim, which the migration's own assertion cannot
# do once the schema is already applied.
tenant_owned=$(psql_exec "
  SELECT count(*)
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public'
    AND c.relkind IN ('r', 'p')
    AND (
      c.relname = 'tenants'
      OR EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE a.attrelid = c.oid AND a.attname = 'tenant_id'
          AND a.attnum > 0 AND NOT a.attisdropped
      )
    )")
if ((tenant_owned == 0)); then
  printf 'found no tenant-owned relation; the RLS assertions would pass vacuously\n' >&2
  exit 1
fi
unsecured=$(psql_exec "
  SELECT count(*)
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = 'public'
    AND c.relkind IN ('r', 'p')
    AND NOT c.relrowsecurity
    AND (
      c.relname = 'tenants'
      OR EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE a.attrelid = c.oid AND a.attname = 'tenant_id'
          AND a.attnum > 0 AND NOT a.attisdropped
      )
    )")
assert_eq "$unsecured" 0 'every tenant-owned relation has row level security'

# Rule 1: PostgreSQL does not apply a parent's policy to a partition queried
# directly, so a partition of an RLS-protected table must carry its own.
open_partitions=$(psql_exec "
  SELECT count(*)
  FROM pg_inherits i
  JOIN pg_class child ON child.oid = i.inhrelid
  JOIN pg_class parent ON parent.oid = i.inhparent
  JOIN pg_namespace n ON n.oid = child.relnamespace
  WHERE n.nspname = 'public'
    AND parent.relrowsecurity
    AND NOT child.relrowsecurity")
assert_eq "$open_partitions" 0 'every partition of an RLS-protected table has row level security'

before=$(psql_exec 'SELECT count(*) FROM tenants')
[[ $before =~ ^[0-9]+$ ]] || { printf 'tenant count read returned %q; absence is not success\n' "$before" >&2; exit 1; }
helm upgrade "$release" "$root/charts/orbitjob" "${helm_args[@]}" --wait --wait-for-jobs --timeout 8m
assert_eq "$(psql_exec 'SELECT count(*) FROM tenants')" "$before" 'a repeated upgrade leaves the tenant count unchanged'

# The failing migration must sort after every real migration. A version that
# already exists is skipped by the runner as applied, and a colliding name makes
# the upgrade fail for the wrong reason, so the destination version is derived
# as the one after the highest real migration.
fail_version=$((expected_max + 1))
fail_dest=$(printf '%04d_fail.up.sql' "$fail_version")
fixture="$root/deploy/kind/fixtures/fail.up.sql"
[[ -f $fixture ]] || { printf 'missing failure fixture: %s\n' "$fixture" >&2; exit 1; }

workdir=$(mktemp -d)
cp -R "$root/charts/orbitjob" "$workdir/chart"
cp "$fixture" "$workdir/chart/migrations/$fail_dest"
if helm upgrade "$release" "$workdir/chart" "${helm_args[@]}" --wait --wait-for-jobs --timeout 3m; then
  printf 'failure migration unexpectedly succeeded\n' >&2
  exit 1
fi
assert_eq "$(psql_exec "SELECT count(*) FROM schema_migrations WHERE version=$fail_version")" 0 \
  "failed migration $fail_version is not recorded"
assert_eq "$(psql_exec "SELECT to_regclass('public.failure_probe') IS NULL")" t \
  'failed migration rolled back its CREATE TABLE'

psql_exec "INSERT INTO tenants(id,slug,name,status) VALUES ('retain-marker','retain-marker','retain marker','active') ON CONFLICT DO NOTHING"
helm uninstall "$release" -n "$namespace" --wait
assert_eq "$(psql_exec "SELECT count(*) FROM tenants WHERE id='retain-marker'")" 1 'the marker row survives helm uninstall'
helm install "$release" "$root/charts/orbitjob" "${helm_args[@]}" --wait --wait-for-jobs --timeout 8m
assert_eq "$(psql_exec "SELECT count(*) FROM tenants WHERE id='retain-marker'")" 1 'the marker row survives a reinstall'
printf 'kind verification passed.\n'
