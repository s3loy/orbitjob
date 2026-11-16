# Known issues and verification gaps

This page records implementation gaps and operating constraints.
[Project status](status.md) describes implemented capabilities;
[verification](verification.md) records tested behavior and qualification limits.

## Current implementation gaps

### Fault qualification requires complete installed recovery evidence

The load harness prepares separate recovery canaries for Operator, Admin API,
PostgreSQL and Scheduler faults. Verification cross-checks tenant-scoped ledger
facts, Kubernetes ownership, a continuous fixture journal and Scheduler budget
evaluation. Ready replicas or a manually supplied recovery claim cannot establish
recovery. Missing preparation, source observations or journal continuity remain
`INCONCLUSIVE`. The fixture keeps its journal in memory; a restart invalidates the
current run's continuity even if later effects are available.

These checks require installed fault scenarios for the actual candidate before
they support qualification. They do not establish arbitrary network-partition or
Lease-loss recovery. See [load testing](loadtest-guide.md) and the scoped
[verification record](verification.md).

### Clean-machine onboarding remains unverified

The quick start has no measured clean-machine completion-time guarantee.
Installation duration depends on downloads, image builds and host resources.
See [verification](verification.md) for installation checks and
[product requirements](prd.md) for onboarding scope.

### Execution evidence can remain irrecoverable

Missing bound Jobs become nonterminal ExecutionUnknown. Empty Pod inventories,
node loss and force deletion do not establish physical process termination.
Unknown and CancelUnknown retain their Forbid slot and are excluded from normal
history pruning. There is no force-release API; prolonged uncertainty
may require manual isolation and a separately reviewed recovery procedure.

New workload Pods retain container exit evidence until the Operator commits it.
An Operator or database outage can therefore leave exited Pods and their Jobs
present. Restore the compatible Operator and database before removing the
installation; follow [the upgrade checks](operations/upgrading.md).
Historical workloads without this protection do not gain missing evidence
during upgrade.

A terminal Pod without trustworthy container exit status remains unknown even
when Kubernetes reports a Job failure. This includes an unscheduled Pod marked
Failed during cleanup. Its evidence hold remains until a separately reviewed
recovery establishes what can safely be removed.

### Schedule adherence has no dedicated SLI derivation

The ledger records `scheduled_for`, but the supported SLI types and run-outcome
criteria do not derive a dedicated schedule-adherence
indicator. Existing status/duration SLIs do not fully measure schedule adherence.
The vocabulary is defined in `internal/core/domain/sli/create_input.go` and
`criteria.go`; `scheduled_for` is not itself an adherence evaluation.

### SLI retirement preserves unresolved history

The Operator retires replay details in bounded pages after the original Run is
gone, all plan members have matching facts, and the plan and latest fact have
passed the minimum 30-day replay horizon. Queryable `unavailable` facts remain
for the longest supported SLO window, currently 8760 hours. A missing event time,
incomplete projection or retained Run prevents automatic retirement. Aggregate
snapshots and observation state remain.

Source and event-run locks serialize plan creation, fact recording and retirement;
retired plans cannot authorize another counter increment. Maintenance includes
inactive sources and suspended tenants still in the Operator namespace mapping.
Removing a tenant from that mapping also removes it from automatic maintenance.
Preserve its data for explicit review. See
[outcome recovery](database-setup.md#writer-cutover).

## Operational and compatibility constraints

- **Long definition identities require migration 0028.** Older schemas restrict
  the audit `resource_id` to 64 characters and can reject an otherwise valid
  `namespace/name` during checked saves. The current migration widens that
  column across existing partitions without truncating audit identities.
  Follow the [database migration procedure](database-setup.md#migration-execution-and-compatibility)
  before deploying the matching application.

- **Account rollout requires installed acceptance.** The personal-account
  candidate includes migration 0027, session-origin configuration and the
  out-of-band setup/recovery tool. PostgreSQL and browser-fixture checks do not
  establish an upgraded installation. Preserve the original API-key and migration
  input channels, stop old writers and verify human sessions, existing automation
  and retained history after the coordinated cutover.
- **Existing installations require their own maintenance verification.** Native
  kind network-policy enforcement and a bounded collector filesystem were
  exercised in a disposable cluster. An installation's verified flags
  must remain unset until its own traffic, mount, capacity and restart checks
  pass. Host backing-filesystem space must meet the 40-GiB prerequisite. Follow
  [local maintenance](local-maintenance.md) before scheduling the change.

- **Settling observes the projected JobRun ledger.** The hosted runner now uses
  `loadtest settle`, with transaction-local tenant context and this run's recorded
  revision IDs. Missing observations or query errors remain unknown. A zero count
  does not prove that unprojected CRs have completed, stop active cron definitions,
  or establish fault recovery; the independent verifier remains authoritative.
- **Historical projection cutover.** Migration 0012 preserves old aggregates
  and acknowledges already-terminal rows. It cannot distinguish a previously
  counted historical event from a missed projection. Automatic recovery covers
  later terminal transitions; missing older projections need offline review.
  Stop old operators before applying this migration and start the new version
  afterwards; overlapping old and new writers can invalidate deduplication.
- **Migration checksum channels differ.** The existing Helm ConfigMap `|-`
  projection removes trailing LF bytes; the migrator hashes its actual input.
  Raw-directory and Helm-mounted SQL therefore are not interchangeable checksum
  channels for an existing ledger. Keep its original input channel and preserve
  both source/projected manifests; never rewrite recorded checksums. See
  [database setup](database-setup.md#writer-cutover).
- **Credential rotation is coordinated work.** Changing an installation Secret
  alone does not change PostgreSQL role passwords or the environment of running
  Pods. Follow the owner-init/upgrade workflow in [database setup](database-setup.md)
  and verify both new connections and restarted workloads. A loopback/port-forward
  connection using a trust rule does not prove that the supplied password works.
- **Integration tests require a disposable database.** They can rebuild schema,
  provision roles and change data. A dedicated `TEST_DATABASE_DSN` must never
  point at installation state. The supported command and serialization are in
  [CONTRIBUTING.md](../CONTRIBUTING.md) and the Makefile; there is no mandatory
  machine-specific test port or container name.
- **Namespace onboarding is explicit.** Workload namespaces must be in
  `operator.namespaceTenants` and receive the matching RBAC. A new tenant row
  alone does not provision a namespace or expand operator authority.
- **External work can repeat.** Job backoff and platform retries can execute
  workload code again and repeat external effects. Workloads must make repeated
  effects safe through idempotency where needed. Work can also fail before
  application code executes, so retries do not guarantee that a business effect
  occurs. A successful trigger response acknowledges a run resource, not a
  completed workload or a ledger row already visible to queries.

## Live evidence still required

Controlled recovery scenarios do not establish arbitrary network-partition
recovery, production resilience or general Job/Pod garbage-collection timing.
Upgrade, rollback and backup recovery must be verified for the actual source,
database history and deployment configuration involved. See
[verification](verification.md) for completed checks and outstanding scenarios.

Load profile durations and hosted choices are defined in
[test/load/config/](../test/load/config/) and the
[load workflow](../.github/workflows/loadtest.yml). Read the product verdict in
`result.json`; successful harness completion alone is not qualification.
