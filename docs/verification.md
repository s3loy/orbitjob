# Verification

This page records verification scope separately from product behavior.
[Project status](status.md) summarizes the implementation;
[known issues](known-issues.md) lists unresolved limitations.
[CONTRIBUTING](../CONTRIBUTING.md#testing) defines the required quality gates.

## Evidence levels

| Level | What the result establishes |
|---|---|
| Unit and race tests | Domain, application, adapter and concurrency behavior under the supplied test boundaries |
| PostgreSQL integration | Actual migration, transaction, RLS, role, authorization and repository behavior on the recorded database version |
| Helm and schema checks | Migration-copy consistency, rendered configuration, RBAC and generated API/CRD contracts |
| Installed scenarios | Behavior observed with the tested images, Kubernetes topology and controlled failure cases |

A passing result applies to its source inputs and test environment. Installation
readiness alone does not establish business recovery. A controlled leader-loss
test does not establish arbitrary network-partition resilience or production HA.

## Backend design checks

| Boundary | Verification approach |
|---|---|
| Management and ledger ownership | Check Admin/Runtime database grants, transaction ownership and real-role denial cases |
| Authorization and tenant isolation | Exercise explicit Deny, delegation boundaries, derived parent scope, RLS and cross-tenant references |
| Source admission | Race distinct requests and exact replays with source lifecycle changes; verify pending admission and pinned execution payloads |
| Execution recovery | Remove owned Job evidence, verify original identity and nonterminal Unknown behavior, and recover retained terminal facts |
| Leadership | Observe actual Lease renewal, lost-term cancellation/drain and successful scheduling after restored authority |
| Cancellation | Confirm owned termination; retain uncertainty when evidence is absent; preserve unrelated Kubernetes objects |
| Outcome projections | Replay terminal facts without duplicate SLI increments; preserve pending projections during retention |
| Execution archive | Verify retained output, ownership checks, separate read/delete permissions, capacity limits and restart persistence |
| Installation and migration | Exercise fresh installation, repeated upgrade, failed migration handling and data retention through uninstall/reinstall |

Tests that rebuild schemas use an independent PostgreSQL instance. Installation
and fault scenarios use a dedicated kind cluster and kubeconfig. Neither suite
may reuse an operator's working database as a disposable fixture.

## Verification record

The dated results below apply to their recorded source inputs and environments,
including earlier candidate builds and the uncommitted working tree tested on
2026-11-15. That working tree included packaging, routing and frontend changes
outside the backend commit series. Those results are preserved historical
evidence; they do not qualify the staged backend snapshots, later commits or
their artifacts. Pending installation, recovery and upgrade work remains separate
from local implementation checks.

Backend commit qualification on 2026-11-15 uses isolated source snapshots.
The runtime snapshot passed `make check`, Helm checks and all seven PostgreSQL
integration packages. The Admin and migration-0026 snapshots passed their scoped
PostgreSQL checks; the identity-0027 snapshot passed all seven PostgreSQL
packages. The final cumulative snapshot passed `make check`, the coverage gate
for all 51 tier packages with two documented no-statement exceptions, Helm and
OpenAPI checks, and `make build-all` for Linux amd64. The changed load-test
package also passed its PostgreSQL integration suite. Native Linux arm64 Admin
and Admission images were built from the cumulative snapshot; the identity CLI
also started successfully from the account-stage image. These local results do
not establish installed acceptance. The opt-in collector container suite and a
new kind installation were not run during commit preparation. Host free space
remains below the 40-GiB prerequisite, and migration 0027 has not been deployed
to the working installation.

| Date | Scope | Result |
|---|---|---|
| 2026-10-30 | Backend lint, vet, race tests, OpenAPI and module consistency | Passed |
| 2026-10-30 | Coverage gate | Passed all 48 tier packages, with three documented exceptions |
| 2026-10-30 | Helm rendering, migration copies, RBAC assertions and Linux binaries | Passed |
| 2026-10-30 | Controller-acknowledged cancellation and retained Pod evidence | Focused unit/race tests and independent implementation review passed |
| 2026-10-30 | Independent PostgreSQL 17.11 integration | All seven packages passed, with 1,312 passing test events and no failed, skipped or incomplete events |
| 2026-11-06 | Linux amd64 and arm64 vulnerability scans | No reachable vulnerabilities; three findings in required modules were outside the code's detected call paths |
| 2026-11-06 | Six Linux arm64 candidate images and a three-node Kubernetes v1.34.3 kind installation | Passed fresh installation, repeated upgrade, expected migration-failure handling, uninstall and reinstall; running image identities matched the recorded builds |
| 2026-11-06 | Native execution and retained-evidence recovery | All nine lifecycle scenarios and both supplemental probes passed; original Deployment replicas, node scheduling state and controller manifest were restored |
| 2026-11-06 | Distributed business recovery and upgrade from released v0.2.1 | Pending |
| 2026-11-15 | Personal-account service, HTTP authentication journey and account database boundaries on disposable PostgreSQL | Passed the scoped cases described below; no account-system rollout to the working kind installation |
| 2026-11-15 | Earlier working-tree backend lint, vet, full race suite, OpenAPI/module consistency and Helm checks | Passed for the recorded working-tree inputs; coverage passed all 51 tier packages with two documented no-statement exceptions |

The 1,194 recorded build inputs were unchanged when rechecked on 2026-11-06.
The PostgreSQL test driver initially reported a cleanup-check failure after
the database tests passed. A separate inspection confirmed removal of the exact
disposable container and volume; the original failed driver result was retained.

### Personal accounts

The account service and HTTP journey were exercised with a disposable PostgreSQL
instance and the `orbitjob_admin` role. The HTTP journey covers setup, session
restoration, exact Origin and CSRF rejection, recent password confirmation,
workspace creation with creator membership, invitations, cross-workspace denial,
password reset and change, account and membership disablement, logout and legacy
Bearer authentication. Its Function invocation uses a Kubernetes dynamic fake;
it does not establish installed workload execution.

Focused database cases also verify single-use setup and invitation races,
session expiry and revocation, last active installation-administrator protection,
database-role privileges, policy Deny and boundary enforcement, and isolation of
global password resets from workspace administration. Final review regressions
verify member reactivation against retained grants, inherited or narrower member
boundaries, and invalidation of earlier reset links and sessions after recovery.

The full PostgreSQL gate exceeded its ten-minute package deadline in
`internal/core/store/postgres` while running
`TestDeletedSnapshotStepsRemainSubjectToExplicitExecutionPause`. Earlier migration,
bootstrap, Admin HTTP and Admin store packages passed. The remaining load-test
package was stopped to reduce workstation load. A later run against an independent
PostgreSQL instance passed all seven packages, as recorded in the reliability and
account maintenance checks below. An installed account-system rollout remains
outstanding.

### Installation origins and current-source local upgrade

On 2026-11-15, the existing single-node Kubernetes v1.37.0 `orbitjob-dev`
installation was upgraded from its Helm-initialized migration ledger 0001–0014
to 0001–0026. Seven current-source Linux arm64 component images, the current
CRDs and matching RBAC were installed. Coordinated database and archive backups
preceded migration. The original bootstrap credential, resource UIDs and all
historical migration checksums were preserved.

Real API checks completed definition validation, save, admission lookup, manual
trigger and successful Kubernetes execution. Both retained container output and
the execution archive returned the expected output. These checks used the real
Admin API, Operator, Admission service, PostgreSQL and log components.

The earlier working-tree Service/Ingress schema, rendered routing and installation
notes passed their focused Go tests and `make helm-check`. Those chart changes
are outside the backend commit series. The checks do not establish an external
Ingress controller or public address.

A separate loopback reverse-proxy probe passed 21 checks across an HTTP hostname,
an HTTPS hostname and an HTTPS IP address with nondefault ports. It preserved
same-origin authentication and rejected public metrics and health paths. TLS
checks verified the probe certificate against its explicit local trust root.
This does not verify an external Ingress controller, public DNS, certificate
issuance or renewal, a fresh-machine install, or a production deployment.

## Reliability and account maintenance checks (2026-11-15)

An independent PostgreSQL 17 instance passed all seven integration packages for
the earlier reliability and personal-account working-tree candidate. Tests
recreated isolated schemas from migrations 0001–0027. They covered migration/schema authority,
Admin/Runtime role restrictions, tenant isolation, source admission and pause,
SLI recording and retirement concurrency, execution-isolation inventory and
recovery-canary verification. Earlier migration bytes and the working
installation's migration ledger were not rewritten.

Account regressions exercised authenticated invitation binding, installation-only
global password reset, recovery invalidation of older reset capabilities and
sessions, and audit emission only for a session actually revoked. Member
reactivation checked the complete saved grant without silently changing it.
Bounded administrators could not restore missing or wider boundaries. An
independent database session verified transaction locking; injected audit failure
verified rollback of membership state and audit history together.

A disposable single-node Kubernetes v1.37.0 kind cluster passed 16 real network
checks with its native `kindest/kindnetd:v20260820-69b56db7`. The checks used the
chart-rendered policy, verified target health before and after denials, and covered
DNS, default ingress/egress denial, one allowed CIDR/port and a denied second port.
Omitting the policy produced the expected failed egress-denial check. Both runs
removed their probe resources; no extra policy provider was needed.

The same disposable node exercised a dedicated 256-MiB ext4 collector filesystem:
physical exhaustion returned `ENOSPC`, preserved state survived node restart, and
a missing image prevented kubelet startup through the required mount dependency.
Restoring the image recovered the mount and preserved sentinel. These checks used
owned rehearsal paths, not the working collector directory. The temporary cluster
was removed after the experiments.

The [working-cluster maintenance procedure](local-maintenance.md) remains a plan.
Its network and quota flags were not enabled, and the candidate was not deployed
there. The working node was Ready with no pressure conditions at the final
inspection, but several components had restarted during concurrent verification;
events recorded probe timeouts and some exits were 137. The records do not prove
an OOM cause. Host free space remained below the 40-GiB installation
prerequisite. Installed account cutover, full fault scenarios and standard/long
load qualification remain pending.

## Recovery and upgrade scope

Cancellation acceptance requires actual container exits, retained evidence,
PostgreSQL persistence, finalizer release and removal of the original Job.
Controller acknowledgement precedes collection of a complete inventory. Tests
must distinguish a slow confirmed stop from an irreversible evidence gap:
release of an unrelated deletion hold can complete the former, while missing
termination facts remain unknown.

The native acceptance suite covers ordinary success and failure, platform and
Kubernetes retries, configured deadlines, cancellation, Operator and database
outages, controller delay and interrupted finalizer release. An unscheduled
terminal Pod without trustworthy container exits must retain its evidence hold
and unknown state. Preparation of a fault runner is not a passing result.

A 2026-10-29 backup/restore exercise compared 22 database tables, 14 sequences
and 807 archive files, then checked retained records through both restored and
original APIs. It completed after recovery from an initial cleanup-wait failure.
This result applies to the preceding installed candidate; it does not establish
the later cancellation protocol, reconstruction of shared database roles on
another server or production disaster recovery.

Upgrade acceptance must start with the actual released v0.2.1 images and its
original migration input channel. It must verify existing credentials and
history, the migration cutover, repeated upgrade and restore of the pre-upgrade
backup. A fresh installation or a second upgrade of the same candidate cannot
substitute for this compatibility check.

## Earlier observations

Earlier checks retain their original source and environment scope:

| Date | Observation |
|---|---|
| 2026-10-29 | The preceding candidate passed seven PostgreSQL integration packages, a dedicated three-node kind installation and eight controlled business-recovery scenarios |
| 2026-10-26 | Execution archives retained accepted output after workload deletion and archive restart; bounded-output, rotation, filesystem-pressure and retention-cutoff scenarios passed |
| 2026-10-26 | A disposable 4-vCPU / 8-GiB installation with 5-GiB database and 10-GiB archive claims passed the documented installation smoke profile |

Raw logs, source manifests, screenshots and recovery artifacts remain outside
published documentation. Credentials and raw workload output are excluded from
public reports. Prior results do not replace acceptance of later behavior
changes.

## Qualification boundaries

Current checks do not establish production rollout, published-image availability,
quantified throughput, prolonged fault tolerance, arbitrary network-partition
recovery or exactly-once workload effects. Standard/long load qualification and
installed execution of the implemented independent business-recovery canaries
remain separate work. The tools require prepared canaries, tenant-scoped ledger
facts, Kubernetes ownership, a continuous fixture journal and Scheduler budget
evaluation; missing evidence remains `INCONCLUSIVE`. Compatibility tests
must identify the actual old/new images and migration input channel before they
can support an upgrade or rollback claim.
