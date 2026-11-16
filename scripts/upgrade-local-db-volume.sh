#!/usr/bin/env bash
set -euo pipefail

# Offline copy to a new PVC; retain the original claim for rollback.
# This helper is deliberately restricted to the supported local installation.
context=kind-orbitjob-dev
namespace=orbitjob
k=(kubectl --context "$context")
old_claim=$("${k[@]}" -n "$namespace" get deployment orbitjob-postgres -o jsonpath='{.spec.template.spec.volumes[?(@.name=="data")].persistentVolumeClaim.claimName}')
database_replicas=$("${k[@]}" -n "$namespace" get deployment orbitjob-postgres -o jsonpath='{.spec.replicas}')
request=$("${k[@]}" -n "$namespace" get pvc "$old_claim" -o jsonpath='{.spec.resources.requests.storage}')
if [[ $request =~ ^([0-9]+)Gi$ ]] && (( BASH_REMATCH[1] >= 5 )); then
  echo '[OK] database claim already meets the 5-GiB floor'
  exit 0
fi
active=$("${k[@]}" -n default get jobruns -o json | python3 -c 'import sys,json; print(sum(x.get("status",{}).get("phase","") not in ("Succeeded","Failed","Canceled","Rejected") for x in json.load(sys.stdin)["items"]))')
(( active == 0 )) || { echo '[FAIL] wait for or cancel active local executions before offline volume migration' >&2; exit 1; }

declare -a workloads replicas
for target in deployment/orbitjob-admin-api deployment/orbitjob-scheduler deployment/orbitjob-operator statefulset/orbitjob-log-archive; do
  if count=$("${k[@]}" -n orbitjob-system get "$target" -o jsonpath='{.spec.replicas}' 2>/dev/null); then
    workloads+=("$target"); replicas+=("$count")
  fi
done
new_claim="orbitjob-postgres-logs-$(date +%s)"
helper="orbitjob-db-copy-$(date +%s)"
switched=false
copy_verified=false
restore_failed=false
restore_command() {
  local description=$1
  shift
  if "$@" >/dev/null; then
    return 0
  else
    local command_rc=$?
    echo "[FAIL] recovery: $description (exit $command_rc)" >&2
    restore_failed=true
    return "$command_rc"
  fi
}
restore() {
  local rc=$?
  trap - EXIT
  restore_command "remove copy Pod $helper" "${k[@]}" -n "$namespace" delete pod "$helper" --ignore-not-found --wait=true || :
  if [[ $copy_verified != true && $switched == true ]]; then
    if restore_command 'stop PostgreSQL before claim rollback' "${k[@]}" -n "$namespace" scale deployment/orbitjob-postgres --replicas=0 &&
      restore_command 'wait for PostgreSQL to stop before claim rollback' "${k[@]}" -n "$namespace" wait --for=delete pod -l app=orbitjob-postgres --timeout=90s; then
      restore_command "restore database claim $old_claim" "${k[@]}" -n "$namespace" patch deployment orbitjob-postgres --type=json \
        -p "[{\"op\":\"replace\",\"path\":\"/spec/template/spec/volumes/0/persistentVolumeClaim/claimName\",\"value\":\"$old_claim\"}]" || :
    fi
  fi
  restore_command "restore PostgreSQL replicas to $database_replicas" "${k[@]}" -n "$namespace" scale deployment/orbitjob-postgres --replicas="$database_replicas" || :
  restore_command 'wait for PostgreSQL recovery' "${k[@]}" -n "$namespace" rollout status deployment/orbitjob-postgres --timeout=180s || :
  for index in "${!workloads[@]}"; do
    restore_command "restore ${workloads[$index]} replicas to ${replicas[$index]}" "${k[@]}" -n orbitjob-system scale "${workloads[$index]}" --replicas="${replicas[$index]}" || :
    restore_command "wait for ${workloads[$index]} recovery" "${k[@]}" -n orbitjob-system rollout status "${workloads[$index]}" --timeout=180s || :
  done
  if [[ $copy_verified != true ]]; then
    echo "[FAIL] copy not accepted; original claim retained: $old_claim; new claim retained for inspection: $new_claim" >&2
  fi
  if [[ $restore_failed == true ]]; then
    echo "[FAIL] recovery incomplete; inspect the reported workloads and claims: original=$old_claim; copied=$new_claim" >&2
    if (( rc == 0 )); then rc=1; fi
  fi
  if (( rc == 0 )) && [[ $copy_verified == true ]]; then
    printf '[OK] active database claim: %s (5 GiB); original rollback claim retained: %s\n' "$new_claim" "$old_claim"
  fi
  exit "$rc"
}
trap restore EXIT
for target in "${workloads[@]}"; do "${k[@]}" -n orbitjob-system scale "$target" --replicas=0 >/dev/null; done
for target in "${workloads[@]}"; do
  name=${target#*/}
  "${k[@]}" -n orbitjob-system wait --for=delete pod -l "app.kubernetes.io/name=$name" --timeout=90s >/dev/null
done
"${k[@]}" -n "$namespace" scale deployment/orbitjob-postgres --replicas=0 >/dev/null
"${k[@]}" -n "$namespace" wait --for=delete pod -l app=orbitjob-postgres --timeout=90s >/dev/null

storage_class=$("${k[@]}" -n "$namespace" get pvc "$old_claim" -o jsonpath='{.spec.storageClassName}')
"${k[@]}" apply -f - >/dev/null <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: $new_claim
  namespace: $namespace
  labels: {orbitjob.io/local-volume-migration: "true"}
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: "$storage_class"
  resources: {requests: {storage: 5Gi}}
---
apiVersion: v1
kind: Pod
metadata: {name: $helper, namespace: $namespace}
spec:
  restartPolicy: Never
  automountServiceAccountToken: false
  containers:
    - name: copy
      image: postgres:17-alpine
      command: [sh, -ec, 'test -f /source/PG_VERSION; test ! -e /target/PG_VERSION; cp -a /source/. /target/; sync; cmp /source/PG_VERSION /target/PG_VERSION']
      securityContext: {runAsUser: 0, allowPrivilegeEscalation: false}
      resources: {requests: {cpu: 100m, memory: 64Mi}, limits: {cpu: "1", memory: 256Mi}}
      volumeMounts:
        - {name: source, mountPath: /source, readOnly: true}
        - {name: target, mountPath: /target}
  volumes:
    - name: source
      persistentVolumeClaim: {claimName: $old_claim}
    - name: target
      persistentVolumeClaim: {claimName: $new_claim}
EOF
for _ in $(seq 1 120); do
  phase=$("${k[@]}" -n "$namespace" get pod "$helper" -o jsonpath='{.status.phase}')
  [[ $phase != Failed ]] || { echo '[FAIL] database copy Pod failed; inspect its logs' >&2; exit 1; }
  [[ $phase != Succeeded ]] || break
  sleep 1
done
[[ $phase == Succeeded ]] || { echo '[FAIL] database copy deadline exceeded' >&2; exit 1; }
"${k[@]}" -n "$namespace" delete pod "$helper" --wait=true >/dev/null
"${k[@]}" -n "$namespace" patch deployment orbitjob-postgres --type=json \
  -p "[{\"op\":\"replace\",\"path\":\"/spec/template/spec/volumes/0/persistentVolumeClaim/claimName\",\"value\":\"$new_claim\"}]" >/dev/null
switched=true
"${k[@]}" -n "$namespace" scale deployment/orbitjob-postgres --replicas=1 >/dev/null
"${k[@]}" -n "$namespace" rollout status deployment/orbitjob-postgres --timeout=180s
"${k[@]}" -n "$namespace" exec deployment/orbitjob-postgres -- pg_isready -U postgres -d orbitjob >/dev/null
copy_verified=true
