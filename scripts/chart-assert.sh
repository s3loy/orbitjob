#!/usr/bin/env bash
# Assert that the rendered OrbitJob chart matches the Kubernetes-only control
# plane contract:
#   - workload Deployments: admin-api, scheduler, operator; and no worker,
#     dispatcher, or worker-pdb objects of any kind;
#   - for each namespace in operator.namespaceTenants: exactly one Role and one
#     RoleBinding named orbitjob-admin-api, granting the verbs create, get,
#     patch on jobruns.workloads.orbitjob.io and on
#     workflowruns.workloads.orbitjob.io in that namespace -- no more, no
#     less, and no other namespaced Role grants jobruns anywhere;
#   - with operator.enabled=false: the operator Deployment and the
#     per-namespace admin RBAC do not render, while the admin ServiceAccount is
#     retained (it renders unconditionally by chart contract);
#   - the JobRun, WorkflowJob and WorkflowRun CustomResourceDefinitions are
#     part of the rendered output;
#   - the chart's migration files match db/migrations (same check as
#     scripts/helm-migrations.sh check, run against the chart under test).
#
# Comma-carrying values such as operator.namespaceTenants are passed through a
# generated values file, never --set.
#
# The manifest is parsed with awk: the repo has no yq dependency, and
# `helm template` has no JSON output mode.
#
# Usage: scripts/chart-assert.sh [CHART_DIR]
#   CHART_DIR defaults to charts/orbitjob next to the repository root.

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
chart_dir=${1:-"$repo_root/charts/orbitjob"}

failures=0

ok() { printf '[OK] %s\n' "$1"; }
fail() { printf '[FAIL] %s\n' "$1"; failures=$((failures + 1)); }
warn() { printf 'WARN %s\n' "$1"; }

if [[ ! -f $chart_dir/Chart.yaml ]]; then
  fail "chart-dir: $chart_dir does not contain a Chart.yaml"
  exit 1
fi
ok "chart-dir: $chart_dir"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

# The release namespace is fixed so assertions do not depend on a kubeconfig
# context. Tenant identifiers are CHAR(26) ULIDs (tenants.id); the namespaces
# are the operator watch keys. Two namespaces prove the per-namespace loop and
# let a red proof drop exactly one namespace's grant.
release_ns=orbitjob-assert
tenants=(
  "orbitjob-tasks=01ARZ3NDEKTSV4RRFFQ69G5FAV"
  "orbitjob-tasks-alt=01BBZ3NDEKTSV4RRFFQ69G5FAV"
)

values_enabled=$workdir/values-enabled.yaml
{
  echo "operator:"
  echo "  namespaceTenants: \"${tenants[0]%%=*} = ${tenants[0]#*=}, ${tenants[1]%%=*} = ${tenants[1]#*=}\""
} >"$values_enabled"

values_disabled=$workdir/values-disabled.yaml
{
  echo "operator:"
  echo "  enabled: false"
} >"$values_disabled"

values_duplicate=$workdir/values-duplicate.yaml
{
  echo "operator:"
  echo "  namespaceTenants: \"${tenants[0]}, ${tenants[0]}\""
} >"$values_duplicate"

manifest=$workdir/manifest.yaml
if helm template chart-assert "$chart_dir" \
  --namespace "$release_ns" --include-crds \
  -f "$values_enabled" >"$manifest" 2>"$workdir/render-enabled.err"; then
  ok "render-enabled: helm template rendered $(grep -c '^---$' "$manifest" || true) documents"
else
  sed 's/^/       /' "$workdir/render-enabled.err" >&2
  fail "render-enabled: helm template failed against $chart_dir"
fi

if helm template chart-assert "$chart_dir" \
  --namespace "$release_ns" -f "$values_duplicate" \
  >"$workdir/manifest-duplicate.yaml" 2>"$workdir/render-duplicate.err"; then
  fail "render-duplicate: whitespace-prefixed duplicate namespace was accepted"
else
  ok "render-duplicate: normalized duplicate namespace is rejected"
fi

manifest_disabled=$workdir/manifest-disabled.yaml
if helm template chart-assert "$chart_dir" \
  --namespace "$release_ns" --include-crds \
  -f "$values_disabled" >"$manifest_disabled" 2>"$workdir/render-disabled.err"; then
  ok "render-disabled: helm template rendered with operator.enabled=false"
else
  sed 's/^/       /' "$workdir/render-disabled.err" >&2
  fail "render-disabled: helm template failed against $chart_dir"
fi

# Normalize the rendered manifest into tab-separated fact records:
#   DOC<TAB>index<TAB>kind<TAB>namespace<TAB>name
#   RULE<TAB>index<TAB>apiGroups=[..];resources=[..];verbs=[..]   (Role rules,
#       whitespace stripped so rendering cosmetics cannot flip an assertion)
#   RB<TAB>index<TAB>roleKind/roleName                            (RoleBinding roleRef)
#   SUBJ<TAB>index<TAB>kind<TAB>name<TAB>namespace                (RoleBinding subjects)
extract_facts() {
  awk '
    function trimq(s) { gsub(/^[[:space:]]+|[[:space:]]+$/, "", s); return s }
    function norm(s) { gsub(/[[:space:]]/, "", s); return s }
    function reset() {
      kind = ""; mname = ""; mns = ""; topkey = ""
      nrules = 0; nsubj = 0; rrkind = ""; rrname = ""
      split("", rule); split("", subjkind); split("", subjname); split("", subjns)
    }
    function flush() {
      printf "DOC\t%d\t%s\t%s\t%s\n", docno, kind, mns, mname
      if (kind == "Role") {
        for (i = 1; i <= nrules; i++) printf "RULE\t%d\t%s\n", docno, rule[i]
      }
      if (kind == "RoleBinding" || kind == "ClusterRoleBinding") {
        printf "RB\t%d\t%s/%s\n", docno, rrkind, rrname
        for (i = 1; i <= nsubj; i++)
          printf "SUBJ\t%d\t%s\t%s\t%s\n", docno, subjkind[i], subjname[i], subjns[i]
      }
    }
    BEGIN { docno = 0; started = 0 }
    /^---[[:space:]]*$/ {
      if (started) flush()
      docno++; started = 1; reset(); next
    }
    {
      if (!started) { started = 1; reset() }
      line = $0
      sub(/\r$/, "", line)
      if (line == "" || line ~ /^#/) next
      if (line !~ /^[[:space:]]/) {
        colon = index(line, ":")
        if (colon > 1) {
          topkey = substr(line, 1, colon - 1)
          toplevel_val = substr(line, colon + 1)
          if (topkey == "kind") kind = trimq(toplevel_val)
          # A flow-style roleRef sits on the top-level line itself.
          if (kind == "RoleBinding" && topkey == "roleRef" && trimq(toplevel_val) != "") {
            flow = toplevel_val
            if (match(flow, /kind:[^,}]*/)) rrkind = trimq(substr(flow, RSTART + 5, RLENGTH - 5))
            if (match(flow, /name:[^,}]*/)) rrname = trimq(substr(flow, RSTART + 5, RLENGTH - 5))
          }
        }
        next
      }
      # Helm preserves comments from the templates, including inside rule
      # blocks; they are not fields.
      if (line ~ /^[[:space:]]+#/) next
      if (topkey == "metadata" && line ~ /^  (name|namespace):/) {
        colon = index(line, ":")
        key = trimq(substr(line, 1, colon - 1))
        val = trimq(substr(line, colon + 1))
        if (key == "name" && mname == "") mname = val
        if (key == "namespace" && mns == "") mns = val
        next
      }
      if (kind == "Role" && topkey == "rules") {
        if (line ~ /^  - /) { nrules++; part = substr(line, 4) }
        else part = trimq(line)
        colon = index(part, ":")
        if (colon > 1) {
          key = trimq(substr(part, 1, colon - 1))
          val = norm(substr(part, colon + 1))
          sep = (rule[nrules] == "") ? "" : ";"
          rule[nrules] = rule[nrules] sep key "=" val
        }
        next
      }
      if ((kind == "RoleBinding" || kind == "ClusterRoleBinding") && topkey == "roleRef") {
        if (line ~ /^roleRef: /) {
          flow = substr(line, 10)
          if (match(flow, /kind:[^,}]*/)) rrkind = trimq(substr(flow, RSTART + 5, RLENGTH - 5))
          if (match(flow, /name:[^,}]*/)) rrname = trimq(substr(flow, RSTART + 5, RLENGTH - 5))
        } else if (line ~ /^  (kind|name):/) {
          colon = index(line, ":")
          key = trimq(substr(line, 1, colon - 1))
          val = trimq(substr(line, colon + 1))
          if (key == "kind") rrkind = val
          if (key == "name") rrname = val
        }
        next
      }
      if ((kind == "RoleBinding" || kind == "ClusterRoleBinding") && topkey == "subjects") {
        if (line ~ /^  - /) { nsubj++; part = substr(line, 4) }
        else part = trimq(line)
        colon = index(part, ":")
        if (colon > 1) {
          key = trimq(substr(part, 1, colon - 1))
          val = trimq(substr(part, colon + 1))
          if (key == "kind") subjkind[nsubj] = val
          if (key == "name") subjname[nsubj] = val
          if (key == "namespace") subjns[nsubj] = val
        }
        next
      }
    }
    END { if (started) flush() }
  ' "$1" >"$2"
}

facts=$workdir/facts-enabled.tsv
[[ -s $manifest ]] && extract_facts "$manifest" "$facts"

# Prove the fact extractor cannot hide a renamed cluster-wide binding to the
# operator identity. The production manifest must not contain this object; the
# synthetic copy exists only to exercise the deny assertion's input.
scope_probe=$workdir/manifest-scope-probe.yaml
cp "$manifest" "$scope_probe"
cat >>"$scope_probe" <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: renamed-broad-grant
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: orbitjob-operator
    namespace: $release_ns
EOF
scope_probe_facts=$workdir/facts-scope-probe.tsv
extract_facts "$scope_probe" "$scope_probe_facts"
scope_probe_subject=$(awk -F'\t' -v release_ns="$release_ns" '
  $1 == "DOC" { kind[$2] = $3; next }
  $1 == "SUBJ" && kind[$2] == "ClusterRoleBinding" && $3 == "ServiceAccount" && $4 == "orbitjob-operator" && $5 == release_ns { print $4 "@" $5 }
' "$scope_probe_facts")
if [[ $scope_probe_subject == "orbitjob-operator@$release_ns" ]]; then
  ok "operator-rbac-scope-probe: renamed ClusterRoleBinding subject is visible to assertions"
else
  fail "operator-rbac-scope-probe: extractor hid a renamed ClusterRoleBinding subject"
fi

count_doc() { # <kind> <namespace-or-empty> <name> <facts-file>
  awk -F'\t' -v k="$1" -v ns="$2" -v n="$3" \
    '$1 == "DOC" && $3 == k && $4 == ns && $5 == n { c++ } END { print c + 0 }' "$4"
}

# --- rendered contract with the operator enabled ---------------------------

for name in orbitjob-admin-api orbitjob-scheduler orbitjob-operator; do
  c=$(awk -F'\t' -v n="$name" \
    '$1 == "DOC" && $3 == "Deployment" && $5 == n { c++ } END { print c + 0 }' "$facts")
  if [[ $c == 1 ]]; then
    ok "workloads-rendered: Deployment $name"
  else
    fail "workloads-rendered: expected exactly 1 Deployment $name, found $c"
  fi
done

forbidden=$(awk -F'\t' \
  '{ if ($1 == "DOC" && (tolower($5) ~ /worker|dispatcher/ || $3 == "PodDisruptionBudget")) print "  " $3 " ns=" $4 " name=" $5 }' \
  "$facts")
if [[ -z $forbidden ]]; then
  ok "forbidden-objects: no worker or dispatcher object of any kind, no PodDisruptionBudget"
else
  fail "forbidden-objects: removed workload objects are rendered:
$forbidden"
fi

for crd_name in jobruns.workloads.orbitjob.io workflowjobs.workloads.orbitjob.io workflowruns.workloads.orbitjob.io; do
  c=$(count_doc CustomResourceDefinition "" "$crd_name" "$facts")
  if [[ $c == 1 ]]; then
    ok "crd-rendered: CustomResourceDefinition $crd_name rendered"
  else
    fail "crd-rendered: expected exactly 1 CustomResourceDefinition $crd_name, found $c"
  fi
done

# Two rules, in document order: jobruns first, workflowruns second. Same verbs
# on both -- create publishes, get answers a replayed trigger, patch carries a
# cancel request (fan-out to step JobRuns in the workflow case).
expected_rule_jobruns='apiGroups=["workloads.orbitjob.io"];resources=["jobruns"];verbs=["create","get","patch"]'
expected_rule_workflowruns='apiGroups=["workloads.orbitjob.io"];resources=["workflowruns"];verbs=["create","get","patch"]'
for entry in "${tenants[@]}"; do
  ns=${entry%%=*}

  role_count=$(count_doc Role "$ns" orbitjob-admin-api "$facts")
  if [[ $role_count == 1 ]]; then
    ok "admin-rbac-role:$ns exactly one Role orbitjob-admin-api"
  else
    fail "admin-rbac-role:$ns expected exactly 1 Role orbitjob-admin-api, found $role_count"
  fi

  rule=$(awk -F'\t' -v ns="$ns" '
    $1 == "DOC" && $3 == "Role" && $4 == ns && $5 == "orbitjob-admin-api" { d = $2 }
    $1 == "RULE" && $2 == d { print $3 }' "$facts")
  expected_rule="$expected_rule_jobruns
$expected_rule_workflowruns"
  if [[ $rule == "$expected_rule" ]]; then
    ok "admin-rbac-rule:$ns grants only create,get,patch on jobruns and workflowruns"
  else
    fail "admin-rbac-rule:$ns Role rules are [$rule], want [$expected_rule]"
  fi

  rb_count=$(count_doc RoleBinding "$ns" orbitjob-admin-api "$facts")
  if [[ $rb_count == 1 ]]; then
    ok "admin-rbac-rolebinding:$ns exactly one RoleBinding orbitjob-admin-api"
  else
    fail "admin-rbac-rolebinding:$ns expected exactly 1 RoleBinding orbitjob-admin-api, found $rb_count"
    continue
  fi

  rbref=$(awk -F'\t' -v ns="$ns" '
    $1 == "DOC" && $3 == "RoleBinding" && $4 == ns && $5 == "orbitjob-admin-api" { d = $2 }
    $1 == "RB" && $2 == d { print $3 }' "$facts")
  if [[ $rbref == "Role/orbitjob-admin-api" ]]; then
    ok "admin-rbac-rolebinding-ref:$ns binds Role orbitjob-admin-api"
  else
    fail "admin-rbac-rolebinding-ref:$ns roleRef is [$rbref], want [Role/orbitjob-admin-api]"
  fi

  subj=$(awk -F'\t' -v ns="$ns" '
    $1 == "DOC" && $3 == "RoleBinding" && $4 == ns && $5 == "orbitjob-admin-api" { d = $2 }
    $1 == "SUBJ" && $2 == d { print $3 "/" $4 "@" $5 }' "$facts")
  if [[ $subj == "ServiceAccount/orbitjob-admin-api@$release_ns" ]]; then
    ok "admin-rbac-subject:$ns subject is ServiceAccount orbitjob-admin-api@$release_ns"
  else
    fail "admin-rbac-subject:$ns subjects are [$subj], want [ServiceAccount/orbitjob-admin-api@$release_ns]"
  fi
done

expected_total=${#tenants[@]}
for kind in Role RoleBinding; do
  total=$(awk -F'\t' -v k="$kind" \
    '$1 == "DOC" && $3 == k && $5 == "orbitjob-admin-api" { c++ } END { print c + 0 }' "$facts")
  if [[ $total == "$expected_total" ]]; then
    ok "admin-rbac-total: exactly $expected_total $kind named orbitjob-admin-api (no more, no less)"
  else
    fail "admin-rbac-total: expected exactly $expected_total $kind named orbitjob-admin-api, found $total"
  fi
done

for entry in "${tenants[@]}"; do
  ns=${entry%%=*}
  for kind in Role RoleBinding; do
    count=$(count_doc "$kind" "$ns" orbitjob-operator "$facts")
    if [[ $count == 1 ]]; then
      ok "operator-rbac:$ns exactly one $kind orbitjob-operator"
    else
      fail "operator-rbac:$ns expected exactly 1 $kind orbitjob-operator, found $count"
    fi
  done

  operator_rule=$(awk -F'\t' -v ns="$ns" '
    $1 == "DOC" && $3 == "Role" && $4 == ns && $5 == "orbitjob-operator" { d = $2 }
    $1 == "RULE" && $2 == d { print $3 }' "$facts")
  expected_operator_rule='apiGroups=["workloads.orbitjob.io"];resources=["scheduledjobs"];verbs=["get","list","watch","update","patch"]
apiGroups=["workloads.orbitjob.io"];resources=["jobruns"];verbs=["get","list","watch","create","update","patch","delete"]
apiGroups=["workloads.orbitjob.io"];resources=["workflowjobs","workflowruns"];verbs=["get","list","watch","patch"]
apiGroups=["workloads.orbitjob.io"];resources=["scheduledjobs/status","jobruns/status","workflowruns/status","workflowjobs/status"];verbs=["get","update","patch"]
apiGroups=["batch"];resources=["jobs"];verbs=["get","list","watch","create","update","patch","delete"]
apiGroups=[""];resources=["pods"];verbs=["get","list","watch"]'
  if [[ $operator_rule == "$expected_operator_rule" ]]; then
    ok "operator-rbac-rule:$ns grants the exact reconciliation surface"
  else
    fail "operator-rbac-rule:$ns Role rules are [$operator_rule], want [$expected_operator_rule]"
  fi

  operator_ref=$(awk -F'\t' -v ns="$ns" '
    $1 == "DOC" && $3 == "RoleBinding" && $4 == ns && $5 == "orbitjob-operator" { d = $2 }
    $1 == "RB" && $2 == d { print $3 }' "$facts")
  if [[ $operator_ref == "Role/orbitjob-operator" ]]; then
    ok "operator-rbac-rolebinding-ref:$ns binds Role orbitjob-operator"
  else
    fail "operator-rbac-rolebinding-ref:$ns roleRef is [$operator_ref], want [Role/orbitjob-operator]"
  fi

  operator_subj=$(awk -F'\t' -v ns="$ns" '
    $1 == "DOC" && $3 == "RoleBinding" && $4 == ns && $5 == "orbitjob-operator" { d = $2 }
    $1 == "SUBJ" && $2 == d { print $3 "/" $4 "@" $5 }' "$facts")
  if [[ $operator_subj == "ServiceAccount/orbitjob-operator@$release_ns" ]]; then
    ok "operator-rbac-subject:$ns subject is ServiceAccount orbitjob-operator@$release_ns"
  else
    fail "operator-rbac-subject:$ns subjects are [$operator_subj], want [ServiceAccount/orbitjob-operator@$release_ns]"
  fi
done

cluster_grants=$(awk -F'\t' '$1 == "DOC" && ($3 == "ClusterRole" || $3 == "ClusterRoleBinding") && $5 == "orbitjob-operator" { c++ } END { print c + 0 }' "$facts")
if [[ $cluster_grants == 0 ]]; then
  ok "operator-rbac-scope: no cluster-wide operator grant renders"
else
  fail "operator-rbac-scope: found $cluster_grants cluster-wide operator grants"
fi

cluster_operator_bindings=$(awk -F'\t' -v release_ns="$release_ns" '
  $1 == "DOC" { kind[$2] = $3; name[$2] = $5; next }
  $1 == "SUBJ" && kind[$2] == "ClusterRoleBinding" && $3 == "ServiceAccount" && $4 == "orbitjob-operator" && $5 == release_ns {
    print name[$2]
  }' "$facts")
if [[ -z $cluster_operator_bindings ]]; then
  ok "operator-rbac-scope: no ClusterRoleBinding grants the operator ServiceAccount"
else
  fail "operator-rbac-scope: ClusterRoleBindings grant the operator ServiceAccount: [$cluster_operator_bindings]"
fi

for kind in Role RoleBinding; do
  count=$(count_doc "$kind" "$release_ns" orbitjob-operator-leader-election "$facts")
  if [[ $count == 1 ]]; then
    ok "operator-leader-election: exactly one namespaced $kind"
  else
    fail "operator-leader-election: expected exactly 1 namespaced $kind, found $count"
  fi
done


leader_rule=$(awk -F'\t' -v ns="$release_ns" '
  $1 == "DOC" && $3 == "Role" && $4 == ns && $5 == "orbitjob-operator-leader-election" { d = $2 }
  $1 == "RULE" && $2 == d { print $3 }' "$facts")
expected_leader_rule='apiGroups=["coordination.k8s.io"];resources=["leases"];verbs=["get","create","update","delete"]'
if [[ $leader_rule == "$expected_leader_rule" ]]; then
  ok "operator-leader-election-rule: grants only the required lease verbs"
else
  fail "operator-leader-election-rule: Role rules are [$leader_rule], want [$expected_leader_rule]"
fi

leader_ref=$(awk -F'\t' -v ns="$release_ns" '
  $1 == "DOC" && $3 == "RoleBinding" && $4 == ns && $5 == "orbitjob-operator-leader-election" { d = $2 }
  $1 == "RB" && $2 == d { print $3 }' "$facts")
leader_subj=$(awk -F'\t' -v ns="$release_ns" '
  $1 == "DOC" && $3 == "RoleBinding" && $4 == ns && $5 == "orbitjob-operator-leader-election" { d = $2 }
  $1 == "SUBJ" && $2 == d { print $3 "/" $4 "@" $5 }' "$facts")
if [[ $leader_ref == "Role/orbitjob-operator-leader-election" && $leader_subj == "ServiceAccount/orbitjob-operator@$release_ns" ]]; then
  ok "operator-leader-election-binding: binds the operator ServiceAccount to the namespaced Role"
else
  fail "operator-leader-election-binding: roleRef [$leader_ref], subjects [$leader_subj]"
fi

rogue=$(awk -F'\t' '
  $1 == "DOC" { rkind[$2] = $3; rns[$2] = $4; rname[$2] = $5; next }
  $1 == "RULE" && index($3, "resources=[\"jobruns\"]") > 0 {
    if (rkind[$2] == "Role" && (rname[$2] == "orbitjob-admin-api" || rname[$2] == "orbitjob-operator")) next
    if (rkind[$2] == "Role") print "  Role " rname[$2] " ns=" rns[$2] ": " $3
  }' "$facts")
if [[ -z $rogue ]]; then
  ok "admin-rbac-rogue: no namespaced Role other than orbitjob-admin-api grants jobruns"
else
  fail "admin-rbac-rogue: unexpected namespaced jobruns grants:
$rogue"
fi

# --- rendered contract with the operator disabled ---------------------------

facts_disabled=$workdir/facts-disabled.tsv
[[ -s $manifest_disabled ]] && extract_facts "$manifest_disabled" "$facts_disabled"

operator_leftover=$(awk -F'\t' \
  '$1 == "DOC" && ($5 == "orbitjob-operator" || $5 == "orbitjob-operator-runtime") { print "  " $3 " ns=" $4 " name=" $5 }' \
  "$facts_disabled")
if [[ -z $operator_leftover ]]; then
  ok "operator-absent-disabled: no operator Deployment, RBAC, ServiceAccount, or ConfigMap renders"
else
  fail "operator-absent-disabled: operator objects render with operator.enabled=false:
$operator_leftover"
fi

admin_rbac_leftover=$(awk -F'\t' \
  '$1 == "DOC" && $5 == "orbitjob-admin-api" && ($3 == "Role" || $3 == "RoleBinding") { print "  " $3 " ns=" $4 }' \
  "$facts_disabled")
if [[ -z $admin_rbac_leftover ]]; then
  ok "admin-rbac-absent-disabled: no per-namespace admin Role or RoleBinding renders"
else
  fail "admin-rbac-absent-disabled: per-namespace admin RBAC renders with operator.enabled=false:
$admin_rbac_leftover"
fi

sa_count=$(count_doc ServiceAccount "$release_ns" orbitjob-admin-api "$facts_disabled")
if [[ $sa_count == 1 ]]; then
  ok "admin-sa-retained-disabled: ServiceAccount orbitjob-admin-api is retained"
else
  fail "admin-sa-retained-disabled: expected exactly 1 ServiceAccount orbitjob-admin-api in $release_ns, found $sa_count"
fi

# --- chart migrations vs db/migrations --------------------------------------

list_up() {
  find "$1" -maxdepth 1 -type f -name '*.up.sql' -exec basename {} \; | LC_ALL=C sort
}
src_migrations="$repo_root/db/migrations"
dst_migrations="$chart_dir/migrations"
if [[ ! -d $src_migrations || ! -d $dst_migrations ]]; then
  fail "migrations-sync: missing migration directory ($src_migrations or $dst_migrations)"
else
  list_up "$src_migrations" >"$workdir/migrations-src.lst"
  list_up "$dst_migrations" >"$workdir/migrations-dst.lst"
  if ! diff -u "$workdir/migrations-src.lst" "$workdir/migrations-dst.lst" >"$workdir/migrations.diff"; then
    fail "migrations-sync: migration file set differs from db/migrations:
$(sed 's/^/  /' "$workdir/migrations.diff")"
  else
    mismatched=""
    while IFS= read -r name; do
      if ! cmp -s "$src_migrations/$name" "$dst_migrations/$name"; then
        mismatched="$mismatched  $name
"
      fi
    done <"$workdir/migrations-src.lst"
    if [[ -z $mismatched ]]; then
      ok "migrations-sync: $(wc -l <"$workdir/migrations-src.lst" | tr -d ' ') migration files match db/migrations"
    else
      fail "migrations-sync: migration content mismatch:
$mismatched"
    fi
  fi
fi

# --- inert leftovers ---------------------------------------------------------

leftover=$(find "$chart_dir/templates" -type f \( -name '*worker*' -o -name '*dispatcher*' \) 2>/dev/null | LC_ALL=C sort | tr '\n' ' ')
if [[ -n $leftover ]]; then
  warn "leftover template files in $chart_dir/templates: $leftover"
fi

# --- summary -----------------------------------------------------------------

if [[ $failures -gt 0 ]]; then
  printf '[FAIL] chart-assert: %d assertion(s) failed\n' "$failures"
  exit 1
fi
printf '[OK] chart-assert: all assertions passed for %s\n' "$chart_dir"
