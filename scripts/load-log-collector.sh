#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cluster=${1:-orbitjob-dev}
image=$(sed -n 's/^  collectorImage: //p' "$root/charts/orbitjob/values.yaml")
[[ $image =~ ^cr\.fluentbit\.io/fluent/fluent-bit:[a-zA-Z0-9.-]+@sha256:[a-f0-9]{64}$ ]] || {
  echo '[FAIL] collector image must be the explicit digest-pinned official image' >&2; exit 1;
}
docker pull "$image"
architecture=$(docker info --format '{{.Architecture}}')
case "$architecture" in aarch64|arm64) architecture=arm64 ;; x86_64|amd64) architecture=amd64 ;; *) echo '[FAIL] unsupported local node architecture' >&2; exit 1 ;; esac
# Docker may hold only this platform of a multi-platform index. kind's generic
# --all-platforms import then asks for layers that were never pulled.
while IFS= read -r node; do
  [[ -n $node ]] || continue
  docker image save "$image" | docker exec --privileged -i "$node" \
    ctr --namespace k8s.io images import --platform "linux/$architecture" --digests -
  digest=${image##*@}
  canonical="${image%%:*}@$digest"
  source=$(docker exec "$node" ctr --namespace k8s.io images ls -q | awk -v suffix="@$digest" 'index($0,suffix) {print; exit}')
  [[ -n $source ]] || { echo '[FAIL] imported collector index is missing' >&2; exit 1; }
  docker exec "$node" ctr --namespace k8s.io images tag --force "$source" "$canonical" "$image"
  docker exec "$node" crictl inspecti "$canonical" >/dev/null
done < <(kind get nodes --name "$cluster")
