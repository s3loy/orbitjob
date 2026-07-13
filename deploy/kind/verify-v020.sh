#!/usr/bin/env bash
set -euo pipefail

cluster=${KIND_CLUSTER_NAME:-orbitjob-v020}
namespace=orbitjob
release=orbitjob
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
tag=${ORBITJOB_IMAGE_TAG:-v020}
created_cluster=false
workdir=
password_file=

cleanup() {
  status=$?
  if ((status != 0)); then
    kubectl get pods,jobs -A 2>/dev/null || true
    helm status "$release" -n "$namespace" 2>/dev/null || true
    kubectl logs -n "$namespace" -l job-name --all-containers --tail=100 2>/dev/null || true
  fi
  [[ -z ${workdir:-} ]] || rm -rf "$workdir"
  [[ -z ${password_file:-} ]] || rm -f "$password_file"
  if [[ $created_cluster == true && $status == 0 ]]; then
    kind delete cluster --name "$cluster" >/dev/null 2>&1 || true
  elif [[ $created_cluster == true ]]; then
    printf 'kind cluster %s retained for failure diagnosis\n' "$cluster" >&2
  fi
  exit "$status"
}
trap cleanup EXIT

require() { command -v "$1" >/dev/null || { printf '%s is required\n' "$1" >&2; exit 1; }; }
for command in kind kubectl helm docker openssl; do require "$command"; done

docker info >/dev/null
if ! kind get clusters | grep -qx "$cluster"; then
  kind create cluster --name "$cluster" --wait 5m
  created_cluster=true
fi
kubectl config use-context "kind-$cluster" >/dev/null
kubectl apply -f "$root/deploy/kind/postgres-17.yaml"
kubectl rollout status deployment/orbitjob-postgres -n "$namespace" --timeout=3m

umask 077
password_file=$(mktemp "${TMPDIR:-/tmp}/orbitjob-v020-passwords.XXXXXX")
migrator_password=$(openssl rand -hex 24)
admin_password=$(openssl rand -hex 24)
runtime_password=$(openssl rand -hex 24)
operator_password=$(openssl rand -hex 24)
owner_dsn='postgres://postgres:orbitjob-owner@orbitjob-postgres.orbitjob.svc:5432/orbitjob?sslmode=disable'
kubectl create secret generic orbitjob-database -n "$namespace" \
  --from-literal=bootstrap-owner-dsn="$owner_dsn" \
  --from-literal=migrator-dsn="postgres://orbitjob_migrator:${migrator_password}@orbitjob-postgres.orbitjob.svc:5432/orbitjob?sslmode=disable" \
  --from-literal=admin-dsn="postgres://orbitjob_admin:${admin_password}@orbitjob-postgres.orbitjob.svc:5432/orbitjob?sslmode=disable" \
  --from-literal=runtime-dsn="postgres://orbitjob_runtime:${runtime_password}@orbitjob-postgres.orbitjob.svc:5432/orbitjob?sslmode=disable" \
  --from-literal=migrator-password="$migrator_password" \
  --from-literal=admin-password="$admin_password" \
  --from-literal=runtime-password="$runtime_password" \
  --from-literal=operator-password="$operator_password" \
  --from-literal=bootstrap-api-key="otj_$(openssl rand -hex 24)" \
  --dry-run=client -o yaml | kubectl apply -f - >/dev/null

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
[[ $(psql_exec "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname IN ('tenants','api_keys','jobs','job_instances','job_instance_attempts','workers','audit_events','job_change_audits','checks','check_runs','slis','slos','sli_snapshots','budgets','budget_alerts') AND c.relrowsecurity AND c.relforcerowsecurity") == 15 ]]

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
