# Installation prerequisites

OrbitJob runs workloads as Kubernetes Jobs. A complete installation needs a
Kubernetes cluster, PostgreSQL, persistent execution-log storage and all control
plane images from the same source version. The chart does not provision a
PostgreSQL server, an Ingress controller, a certificate issuer or a monitoring
stack.

For a disposable contributor environment, follow [local development](../local-deployment.md).
For an administrator-managed cluster, complete the requirements below, then
follow [installation](installation.md).

## Choose the installation artifact

Use a chart, CustomResourceDefinitions (CRDs), migration files and component
images from the same version. The chart currently declares `version: 0.3.0` and
`appVersion: "0.3.0"`; this declaration does not establish that registry images
for that version have been published or include the current Console.

The current-source installation procedure builds seven images: Admin API,
Scheduler, Operator, migrator, bootstrap, log archive and admission webhook.
The Admin image includes the Console and the `orbitjob-identity` administration
tool. An older published Admin image may not provide those features. Select an
artifact with the required features before provisioning an installation.

## Cluster access

The installation administrator needs permission to:

- Install and update the `workloads.orbitjob.io` CRDs and validating webhook.
- Create the release namespace and execution namespaces, ServiceAccounts,
  namespaced roles, ClusterRoles and bindings required by the rendered chart.
- Create Secrets, a PersistentVolumeClaim, Deployments, a StatefulSet, a
  DaemonSet, Services and NetworkPolicies.
- Inspect Pod events, logs, Jobs, workload CRs, PVCs and rollout status.

Use a dedicated kubeconfig or explicit kubectl context. Check the context before
any write. The chart uses fixed component names, so use one OrbitJob release
per release namespace. Separate release names alone do not isolate those names.

The repository does not define a supported Kubernetes version range. The local
installation preflight requires kubectl's major and minor versions to match its
cluster. Use the matching kubectl minor for these procedures and qualify other
cluster versions in an isolated environment before adoption.

## Tools

Run repository commands from the root of the selected source checkout.

| Tool | Requirement | Used for |
|---|---|---|
| Go | 1.27.3+ | Database configuration and source tools |
| Docker | BuildKit and registry push support | Building current-source images |
| kubectl | Same major/minor as the target cluster for these procedures | Installation and diagnosis |
| Helm | Helm 4; repository CI uses 4.2.4 | Rendering and release hooks |
| Bash and GNU make | Compatible with the repository shell scripts and Makefile | Local helper commands |
| OpenSSL | `rand -hex` and certificate commands | Credentials and local fixture certificates |
| Python | Python 3 | Secret manifests and diagnostic tools |
| curl | Supports `--fail-with-body` | API checks and load tools |
| PostgreSQL client | `psql`, `pg_dump`, `pg_restore`, `pg_dumpall` compatible with the server | Database checks and backups |

Host Console development requires npm and a Node.js version supported by the
toolchain: 22.13.0 or later in 22.x, 24.x, or 26.0.0 and later. Docker builds
provide their own Node runtime. kind is needed only for the local cluster and
isolated installation checks; the hosted workflow pins kind 0.32.0. These CI
pins are reproducibility inputs, not tested compatibility ranges.

## PostgreSQL

Provide an existing PostgreSQL database reachable both by the configuration
machine and by cluster Pods. The repository's local manifest and integration
workflow use PostgreSQL 17; qualify any other server version and managed-service
policy before using it.

The bootstrap account needs `LOGIN`, `CREATEROLE`, database ownership and
permission to install `pgcrypto` in `public`. It also needs `ADMIN OPTION` on
pre-existing OrbitJob roles created by another account. Application components
receive separate restricted role credentials.

Managed role names are fixed and PostgreSQL roles are server-wide. Separate
databases do not isolate independently generated role passwords. Reserve a
server or arrange shared-role credentials under database administration before
creating multiple installations on that server.

Choose one database arrangement:

- For local kind development, `make kind-db` deploys the repository's
  PostgreSQL 17 manifest with a 5-GiB claim and prepares the installation Secret.
  Its fixed local credentials and single replica are development settings.
- For an externally managed server, provision the database, bootstrap account,
  connectivity, TLS and backups before running database setup.
- `configure setup --mode bundled` generates credentials for an already
  provisioned server. It does not create a PostgreSQL server or connect to it.

Follow [database configuration](../database-setup.md) for exact role, Secret,
TLS and migration requirements. The chart cannot currently add arbitrary
PostgreSQL CA/client certificate mounts to its Pods. DSNs that refer to such
files require a separately maintained deployment customization available to
every database consumer, including hooks.

## Storage and execution evidence

Provide a StorageClass or pre-provisioned storage that satisfies the archive's
ReadWriteOnce claim. Defaults request a separate 10-GiB PVC. The archive checks
its declared capacity and allocation budgets; a bound claim alone does not
prove usable capacity. Database storage is separate from this archive.

The Fluent Bit DaemonSet reads node CRI logs and persists offsets and queued
chunks in a node `hostPath`. The default development profile has a 128-MiB queue
and declares a 256-MiB directory capacity. That declaration does not create a
filesystem quota. Production collector settings require an administrator-verified
physical directory quota at least the configured queue plus 64 MiB on every
eligible node. Include offsets and chunks in backup and node-replacement plans.
An ephemeral collector profile loses pending collection state on restart.

Do not claim a complete backup from PostgreSQL alone. Retained execution output
is on the archive PVC; collector offsets and unshipped output are on each node.
See [backup and recovery](../operations/backup-and-recovery.md).

## Networking and certificates

The CNI must enforce the intended NetworkPolicies. The chart can require an
administrator declaration, but cannot test enforcement. Test allowed and denied
traffic before setting `executionNetwork.enforcementVerified: true`. Declare
each business destination's CIDRs and ports; DNS permission alone does not
permit application traffic. Existing historical workload policies may differ
from the current restricted profile.

The Admin Service defaults to `ClusterIP`. For browser use outside loopback,
provide an HTTPS origin. Ingress requires an existing controller and, when TLS
is enabled, a `kubernetes.io/tls` Secret in the release namespace. The configured
Ingress host and `adminapi.console.publicOrigin` must describe the same public
origin. The chart does not issue public certificates. NodePort and LoadBalancer
expose the complete HTTP listener, including operational routes.

The chart separately creates internal admission and execution-log TLS Secrets.
They do not secure public Console traffic. Keep the complete log certificate
cohort for recovery; coordinate any rotation with its consumers. Admission and
log serving certificates are generated with a one-year lifetime, and existing
Secrets are reused on upgrade. Plan certificate inspection and replacement
before expiry; an ordinary upgrade is not an automatic renewal procedure.

## Capacity and availability

The local combined profile requires at least 4 vCPU, 8 GiB memory and 40 GiB
initially free backing storage. Docker Desktop's guest filesystem and the host
filesystem containing its virtual disk must both meet the free-space check.
These are local prerequisites, not production sizing or throughput guarantees.
Load testing has separate CPU requirements in the [load testing guide](../loadtest-guide.md).

The chart runs one Admin API, Scheduler, Operator and archive replica by default;
admission defaults to two replicas. Shared database credentials and multiple
Pods do not establish high availability. Scheduler election and database,
storage and network failover need an independently qualified design. Review
[known limitations](../known-issues.md) before selecting an availability target.
