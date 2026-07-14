#!/usr/bin/env bash
set -euo pipefail

cluster=${KIND_CLUSTER_NAME:-orbitjob-v020}
namespace=orbitjob
release=orbitjob
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tag=${ORBITJOB_IMAGE_TAG:-v020}
created_cluster=false
workdir=
dsn_file=
port_forward_pid=

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
config_dir=$(mktemp -d "${TMPDIR:-/tmp}/orbitjob-v020-config.XXXXXX")
dsn_file=$(mktemp "${TMPDIR:-/tmp}/orbitjob-v020-dsn.XXXXXX")
printf '%s\n' 'postgres://postgres:orbitjob-owner@orbitjob-postgres.orbitjob.svc:5432/orbitjob?sslmode=disable' >"$dsn_file"
# Port-forward lets the same configure command initialize an external database
# without teaching this script how to generate role-specific DSNs.
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
kill "$port_forward_pid"
wait "$port_forward_pid" 2>/dev/null || true
port_forward_pid=

for target in migrate admin scheduler dispatcher worker bootstrap; do
  case "$target" in
    admin) image=orbitjob-admin-api ;;
    *) image="orbitjob-$target" ;;
  esac
  docker build --target "$target" -t "$image:$tag" "$root"
  kind load docker-image --name "$cluster" "$image:$tag"
done

helm_args=(
  -n "$namespace" -f "$root/deploy/kind/values-v020.yaml"
  --set-string "images.admin.tag=$tag"
  --set-string "images.scheduler.tag=$tag"
  --set-string "images.dispatcher.tag=$tag"
  --set-string "images.bootstrap.tag=$tag"
  --set-string "images.migrate.tag=$tag"
  --set-string "worker.image.tag=$tag"
)
helm upgrade --install "$release" "$root/charts/orbitjob" "${helm_args[@]}" \
  --wait --wait-for-jobs --timeout 8m

psql_exec() {
  kubectl exec -n "$namespace" deployment/orbitjob-postgres -- env PGPASSWORD=orbitjob-owner \
    psql -U postgres -d orbitjob -Atqc "$1"
}

[[ $(psql_exec 'SELECT count(*) FROM schema_migrations') == 1 ]]
[[ $(psql_exec 'SELECT max(version) FROM schema_migrations') == 1 ]]
[[ $(psql_exec "SELECT name FROM schema_migrations WHERE version=1") == v020_baseline ]]
[[ $(psql_exec "SELECT count(*) FROM pg_roles WHERE rolname IN ('orbitjob_migrator','orbitjob_admin','orbitjob_runtime','orbitjob_operator') AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreaterole") == 4 ]]
[[ $(psql_exec "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('tenants','api_keys','jobs','job_instances','job_instance_attempts','workers','audit_events','job_change_audits','checks','check_runs','slis','slos','sli_snapshots','budgets','budget_alerts') AND c.relrowsecurity") == 15 ]]

before=$(psql_exec 'SELECT count(*) FROM tenants')
helm upgrade "$release" "$root/charts/orbitjob" "${helm_args[@]}" --wait --wait-for-jobs --timeout 8m
[[ $(psql_exec 'SELECT count(*) FROM tenants') == "$before" ]]

workdir=$(mktemp -d)
cp -R "$root/charts/orbitjob" "$workdir/chart"
cp "$root/deploy/kind/fixtures/0002_fail.up.sql" "$workdir/chart/migrations/"
if helm upgrade "$release" "$workdir/chart" "${helm_args[@]}" --wait --wait-for-jobs --timeout 3m; then
  printf 'failure migration unexpectedly succeeded\n' >&2
  exit 1
fi
[[ $(psql_exec 'SELECT count(*) FROM schema_migrations WHERE version=2') == 0 ]]
[[ $(psql_exec "SELECT to_regclass('public.v020_failure_probe') IS NULL") == t ]]

psql_exec "INSERT INTO tenants(id,slug,name,status) VALUES ('retain-marker','retain-marker','retain marker','active') ON CONFLICT DO NOTHING"
helm uninstall "$release" -n "$namespace" --wait
[[ $(psql_exec "SELECT count(*) FROM tenants WHERE id='retain-marker'") == 1 ]]
helm install "$release" "$root/charts/orbitjob" "${helm_args[@]}" --wait --wait-for-jobs --timeout 8m
[[ $(psql_exec "SELECT count(*) FROM tenants WHERE id='retain-marker'") == 1 ]]
printf 'v0.2.0 kind verification passed.\n'
