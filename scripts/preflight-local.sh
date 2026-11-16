#!/usr/bin/env bash
set -euo pipefail

# Support floor for the combined local profile (ADR0029). Only read checks.
for command in docker kind kubectl helm go nc; do
  command -v "$command" >/dev/null || { echo "[FAIL] missing $command" >&2; exit 1; }
done
read -r cpus memory < <(docker info --format '{{.NCPU}} {{.MemTotal}}')
(( cpus >= 4 )) || { echo '[FAIL] assign at least 4 vCPU to Docker' >&2; exit 1; }
(( memory >= 8589934592 )) || { echo '[FAIL] assign at least 8 GiB memory to Docker' >&2; exit 1; }

node=$(kind get nodes --name orbitjob-dev 2>/dev/null | head -1 || true)
if [[ -n $node ]]; then
  disk=$(docker exec "$node" df -Pk /var/lib | awk 'NR==2 {print $4}')
else
  disk=$(docker run --rm --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges --memory 64m --pids-limit 32 \
    postgres:17-alpine df -Pk / | awk 'NR==2 {print $4}')
fi
[[ $disk =~ ^[0-9]+$ ]] && (( disk >= 41943040 )) || {
  echo '[FAIL] Docker filesystem needs at least 40 GiB free; inspect Docker storage settings' >&2
  exit 1
}

# A Desktop VM can report free blocks beyond the physical host's capacity.
# Inspect the filesystem containing its sparse disk as well. Remote daemons
# need this check on their own host, not on the CLI client's filesystem.
if [[ $(docker info --format '{{.OperatingSystem}}') == *'Docker Desktop'* ]]; then
  endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}')
  if [[ $endpoint == unix://* ]]; then
    storage_path=${ORBITJOB_DOCKER_STORAGE_PATH:-}
    if [[ -z $storage_path && $(uname -s) == Darwin ]]; then
      storage_path="$HOME/Library/Containers/com.docker.docker/Data/vms/0/data/Docker.raw"
    fi
    [[ -n $storage_path && -e $storage_path ]] || {
      echo '[FAIL] set ORBITJOB_DOCKER_STORAGE_PATH to the local Docker Desktop disk image' >&2
      exit 1
    }
    host_disk=$(df -Pk "$storage_path" | awk 'NR==2 {print $4}')
    [[ $host_disk =~ ^[0-9]+$ ]] && (( host_disk >= 41943040 )) || {
      echo '[FAIL] Docker Desktop host storage needs at least 40 GiB free; guest free space is insufficient evidence' >&2
      exit 1
    }
    printf '[OK] Docker Desktop host storage: %s KiB free\n' "$host_disk"
  else
    echo '[FAIL] run Desktop storage preflight on the remote Docker host' >&2
    exit 1
  fi
fi
printf '[OK] Docker profile: %s vCPU, %s bytes memory, %s KiB free Docker filesystem\n' "$cpus" "$memory" "$disk"

if [[ ${1:-} == --cluster ]]; then
  context=kind-orbitjob-dev
  versions=$(kubectl --context "$context" --request-timeout=10s version -o yaml) || {
    echo '[FAIL] cannot establish kubectl and Kubernetes server versions' >&2
    exit 1
  }
  read -r client_major client_minor server_major server_minor < <(
    printf '%s\n' "$versions" | awk '
      /^clientVersion:/ { scope="client" }
      /^serverVersion:/ { scope="server" }
      $1 == "major:" || $1 == "minor:" {
        value=$2; gsub(/"/, "", value); sub(/[^0-9].*$/, "", value)
        versions[scope,$1]=value
      }
      END { print versions["client","major:"], versions["client","minor:"], versions["server","major:"], versions["server","minor:"] }
    '
  )
  [[ $client_major =~ ^[0-9]+$ && $client_minor =~ ^[0-9]+$ &&
     $server_major =~ ^[0-9]+$ && $server_minor =~ ^[0-9]+$ ]] || {
    echo '[FAIL] incomplete kubectl or Kubernetes server version response' >&2
    exit 1
  }
  [[ $client_major == "$server_major" && $client_minor == "$server_minor" ]] || {
    printf '[FAIL] local profile requires kubectl %s.%s.x to match the cluster; client is %s.%s\n' \
      "$server_major" "$server_minor" "$client_major" "$client_minor" >&2
    exit 1
  }
  printf '[OK] kubectl and cluster minor version match: %s.%s\n' "$server_major" "$server_minor"
  request=$(kubectl --context "$context" -n orbitjob get pvc orbitjob-postgres -o jsonpath='{.spec.resources.requests.storage}' 2>/dev/null || true)
  # A migrated installation may point to a different, larger claim.
  claim=$(kubectl --context "$context" -n orbitjob get deploy orbitjob-postgres -o jsonpath='{.spec.template.spec.volumes[?(@.name=="data")].persistentVolumeClaim.claimName}')
  if [[ -n $claim ]]; then
    request=$(kubectl --context "$context" -n orbitjob get pvc "$claim" -o jsonpath='{.spec.resources.requests.storage}')
  fi
  case "$request" in
    *Gi) quantity=${request%Gi}; [[ $quantity =~ ^[0-9]+$ ]] && (( quantity >= 5 )) ;;
    *Ti) quantity=${request%Ti}; [[ $quantity =~ ^[0-9]+$ ]] && (( quantity >= 1 )) ;;
    *) false ;;
  esac || { echo '[FAIL] existing database claim is below 5 GiB; stop installation and prepare a separate backup and storage-migration plan as described in docs/local-deployment.md' >&2; exit 1; }
  echo '[OK] database volume meets the 5-GiB allocation floor'
fi
