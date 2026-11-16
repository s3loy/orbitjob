# Install OrbitJob

Install the current source on a Kubernetes cluster after completing the
[prerequisites](prerequisites.md). This procedure is for a new installation.
Use [upgrade OrbitJob](../operations/upgrading.md) when any OrbitJob database,
workload CR or release already exists. For a contributor's kind environment,
use [local development](../local-deployment.md).

## Before you begin

Obtain cluster administration access, a registry that every node can pull from,
a prepared PostgreSQL database and persistent archive storage. Select a source
revision and a unique image tag for that revision. Keep the chart, CRDs,
migrations and all seven component images together.

The commands use these example names:

```bash
export RELEASE_NAMESPACE=orbitjob-system
export RELEASE_NAME=orbitjob
export EXECUTION_NAMESPACE=orbitjob-tasks
export ORBITJOB_REGISTRY=registry.example.com/your-team
export ORBITJOB_IMAGE_TAG=source-REPLACE_WITH_REVISION
kubectl config current-context
```

Replace the registry and image tag before continuing. Configure registry
credentials using the cluster's existing image-pull mechanism. The chart has no
`imagePullSecrets` value; do not assume that credentials from a local Docker
login are available to nodes. Run the remaining commands from the repository
root at the selected revision.

## Build and publish the images

Build the seven component images with one tag:

```bash
make docker-build REGISTRY="$ORBITJOB_REGISTRY" TAG="$ORBITJOB_IMAGE_TAG"
for component in admin-api scheduler operator migrate bootstrap log-archive admission; do
  docker push "$ORBITJOB_REGISTRY/orbitjob-$component:$ORBITJOB_IMAGE_TAG"
done
```

Use images built for the cluster node architecture. The default build command
builds for the selected Docker platform; it does not publish a multi-platform
manifest. Record each pushed digest with the selected source revision. A tag
change without new image content is not an update, and rebuilding a mutable tag
does not restart existing Pods.

For an installation using published artifacts, use that release's own chart,
images and instructions instead of substituting the current source chart.
Verify artifact availability and Console support explicitly.

## Prepare namespaces and database credentials

Create the release and execution namespaces in the intended context:

```bash
kubectl create namespace "$RELEASE_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "$EXECUTION_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
```

Follow [database configuration](../database-setup.md#external-postgresql) to
create the installation's generated state and the `orbitjob-database` Secret in
`$RELEASE_NAMESPACE`. Do this before Helm: pre-install hooks require that Secret.
The DSNs must resolve from Pods, with any TLS files available in hooks as well
as long-running components. Keep the state and bootstrap credential private.

For this initial mapping, the execution namespace belongs to the bootstrap
Default workspace, whose ID is `00000000000000000000000001`. Additional tenant
mappings require existing workspace identities and namespaces, plus the chart's
corresponding namespaced grants. Do not map a namespace to an invented tenant ID.

## Configure the installation

Create a private `installation-values.yaml` outside the repository or in an
ignored configuration directory. Substitute the registry, image tag, namespace,
public origin, Ingress class and TLS Secret:

```yaml
global:
  imageRegistry: registry.example.com/your-team
  imageTag: source-REPLACE_WITH_REVISION
  imagePullPolicy: IfNotPresent
operator:
  namespaceTenants: orbitjob-tasks=00000000000000000000000001
taskNamespace:
  create: false
  name: orbitjob-tasks
adminapi:
  console:
    publicOrigin: https://jobs.example.com
  ingress:
    enabled: true
    className: nginx
    host: jobs.example.com
    tls:
      enabled: true
      secretName: orbitjob-public-tls
```

`taskNamespace.create: false` uses the execution namespace prepared above;
Helm must not try to import that existing namespace as a chart-owned object.
Provision `orbitjob-public-tls` and DNS before using this Ingress. To use a
separately managed HTTPS gateway, leave Ingress disabled and route Console and
API traffic to the ClusterIP Service. The public origin is an origin without a
path; forwarded headers do not determine it.

Review execution networking and collector storage against the actual cluster.
For a production declaration, add the verified settings:

```yaml
executionNetwork:
  profile: production
  enforcementVerified: true
  allowedEgress:
    - namespace: orbitjob-tasks
      cidrs: [192.0.2.10/32]
      ports:
        - {protocol: TCP, port: 443}
logs:
  collectorPersistence:
    mode: hostPath
    profile: production
    hostPathBase: /var/lib/orbitjob-collectors
    quotaVerified: true
    quotaBytes: 268435456
```

The destination is an example and must be replaced by the required application
destination. Set verification flags only after real traffic and filesystem
checks. These values are administrator declarations, not provisioners. Set
`logs.storageClass` if the default StorageClass cannot satisfy the archive.

For loopback-only development without an Ingress, set
`adminapi.console.allowInsecureLocalhost: true` and use a localhost
port-forward. That exception does not permit HTTP sessions on a public origin.

## Render and install

Set `VALUES_FILE` to the reviewed file. Rendered manifests contain generated
TLS private keys; keep them private:

```bash
export VALUES_FILE=/absolute/path/to/installation-values.yaml
umask 077
helm lint ./charts/orbitjob -f "$VALUES_FILE"
helm template "$RELEASE_NAME" ./charts/orbitjob \
  --namespace "$RELEASE_NAMESPACE" -f "$VALUES_FILE" > /tmp/orbitjob-rendered.yaml
```

Inspect the image references, namespace mapping, Secrets, PVC requirements,
network destinations and public routing. Stop on a render error or missing
prerequisite. Helm rendering does not establish database connectivity, network
enforcement or registry access.

Install after that review:

```bash
helm install "$RELEASE_NAME" ./charts/orbitjob \
  --namespace "$RELEASE_NAMESPACE" -f "$VALUES_FILE" \
  --wait --wait-for-jobs --timeout 10m
```

Hook order is owner/role initialization, ordered schema migration, then
application bootstrap after workloads are installed. Successful hook Jobs may
be removed. A failed hook can leave database changes committed by earlier
migrations; uninstalling the release does not undo them.

## Verify the installation

Check all long-running components:

```bash
for component in admin-api scheduler operator admission; do
  kubectl rollout status -n "$RELEASE_NAMESPACE" "deployment/orbitjob-$component" --timeout=5m
done
kubectl rollout status -n "$RELEASE_NAMESPACE" statefulset/orbitjob-log-archive --timeout=5m
kubectl rollout status -n "$RELEASE_NAMESPACE" daemonset/orbitjob-log-collector --timeout=5m
kubectl get pods,jobs,pvc -n "$RELEASE_NAMESPACE"
helm status "$RELEASE_NAME" -n "$RELEASE_NAMESPACE"
```

Use the database maintenance account to inspect
`SELECT version, name, checksum FROM schema_migrations ORDER BY version;`.
Verify the complete selected migration chain, rather than only its highest
version. Preserve the Helm-mounted SQL input channel for future upgrades.

Open `https://jobs.example.com/console/` at the configured public origin.
Complete [first administrator setup](../accounts.md#create-the-first-administrator)
using an operator-issued, single-use capability. A bootstrap API key is for API
clients; it is not a Console password. Installation administration and workspace
workload access have separate permissions.

Before admitting normal work, create one controlled workload, observe its
active revision, trigger it, and confirm the terminal run, attempt identity and
retained logs. Verify a forbidden operation with an appropriately restricted
identity. Readiness alone does not prove database, authorization, execution or
log collection behavior. Set up [monitoring](../operations/monitoring.md) and
verify a [recoverable backup](../operations/backup-and-recovery.md).

## If installation fails

Keep the database, installation state, Secrets, archive and failed-hook evidence.
Inspect [troubleshooting](../operations/troubleshooting.md) before retrying.
Correct missing prerequisites and retry with the same credentials and artifact
set. Do not regenerate the bootstrap key after it has been committed, rewrite
migration checksums or remove evidence finalizers to make installation succeed.

Remove the private rendered manifest when review is complete. Removing a failed
release does not authorize deleting database or archive data. For a disposable
cluster, remove the whole isolated environment only after preserving any needed
evidence; for a retained installation, follow backup, drain and recovery
procedures before uninstalling.
