# Known issues

Things that are wrong, misleading, or unverified today, with what was actually
checked and how. Nothing here is a plan; it is a record so the same ground does
not have to be walked twice.

"Verified" means a command was run or a file was read and the answer is quoted
below it; "unverified" means exactly that. An entry leaves the open list when
it is fixed, or when a test would now fail without it; fixed entries move to
the resolved list at the bottom with a one-line receipt pointing at the fix.

## Open

### The long and standard profiles have no recorded completed result

`standard` is four hours of schedule and `long` is eight. Neither has a
completed result recorded in this repository. `long` cannot run on a hosted
GitHub Actions job, which is capped at six hours.

**In progress.** `.github/workflows/loadtest.yml` runs `smoke` and `standard`
only by manual dispatch — `timeout-minutes: 350` for `standard`, 90 otherwise —
with `scripts/loadtest-ci.sh` doing the sequence the guide walks a human
through. These profiles are advisory operator evidence, not merge gates; an
operator decides when a fresh run is useful and records its artifact outside
the repository.

### Rotating the database password silently breaks every workload

**Verified the hard way.** `configure setup` generates the migrator, admin and
runtime passwords and writes them into the installation Secret; the `owner-init`
hook sets them on the roles. Regenerate the Secret without re-running the hook
and the two diverge. Nothing notices until a pod restarts: running pods keep the
environment they started with, so the install looks healthy while every restart
is a crash-loop — `password authentication failed for user "orbitjob_runtime"`.
`helm upgrade` re-runs `owner-init` and repairs it.

Two things made this harder to see than it should have been, both worth
remembering:

- The bundled PostgreSQL runs the stock `postgres:17-alpine` image
  (`deploy/kind/postgres-17.yaml:44`) and mounts no custom pg_hba, so its
  built-in `pg_hba.conf` keeps `host all all 127.0.0.1/32 trust` ahead of the
  catch-all `host all all all scram-sha-256` line. `kubectl port-forward`
  arrives on the pod's loopback, so a connection from the host matches the
  `trust` rule and **verifies no password at all**. A
  from-the-host check reports the credentials as fine while every in-cluster
  connection fails. Test credentials from inside the cluster, or expect the
  answer to be meaningless.
- The failure only shows on restart, so it can sit for hours.

### The integration suite ran against the cluster database and rotated its passwords

**Verified the hard way (2026-09-18 ~13:07Z).** A test run pointed at the
cluster's PostgreSQL instead of the dedicated `ojtest-pg` instance. The
cluster database gained two scratch databases (`baseline_check`,
`orbitjob_test`), and the run's role provisioning flipped `orbitjob_runtime`
— and the `postgres` monitor user the exporter uses — to test values. Every
database touch from the operator and the scheduler failed with
`password authentication failed` (28P01) from 13:07:44Z until the roles were
realigned from the `orbitjob-database` secret and the scratch databases were
dropped. The health gate worked as designed: the operator pod went 0/1 within
one readiness interval, which is what surfaced it.

The red line is unchanged: integration suites run ONLY against `ojtest-pg`
at `127.0.0.1:55432`; the cluster database is install state. The same loopback
`trust` trap as the entry above applies when verifying credentials from the
host — a port-forwarded check proves nothing.

### Schedule adherence has its input but no derivation

**Verified.** `job_run_control_plane.scheduled_for` exists so that "did the
03:00 job run" is answerable without re-deriving occurrence keys
(`db/migrations/0002_checks_to_kubernetes_jobs.up.sql:39`). Nothing derives an
SLI from it: the SLI type vocabulary is `availability`, `latency`, `quality`,
`custom` (`internal/core/domain/sli/create_input.go:5-8`) and the only source
type is `job_run` (`:18`). No owner.

## Resolved

**Resolved 2026-09-21:** `generate` accepts the same explicit profile as the
other load-test stages and rejects a config/profile mismatch before writing a
manifest (`scripts/loadtest/main.go`, `Makefile`).

- **Operator cadence units are explicit.** Resolved 2026-09-18 by distinct
  `OPERATOR_SCHEDULE_INTERVAL_SEC` and `OPERATOR_RETENTION_INTERVAL_MIN` names.
- **The integration harness replays the baseline seed.** Resolved by deriving
  the preset statement from `0001_baseline.up.sql`; no parallel schema remains.
- **Orphan labeled Jobs no longer retry forever.** `ReconcileJob` treats
  `ErrNoAttempt` as a safe skip while preserving the store's anti-adoption gate.

- **The hosted loadtest harness could not complete a profile.** Resolved on
  2026-09-19 in run 35387532919: prepare cleared, manual triggers were admitted
  500/500, and evidence reads used the SELECT-only admin DSN. Load testing is
  intentionally advisory. A green workflow means the harness completed and
  uploaded evidence; the product result remains the independent verdict in
  `result.json`, which an operator reviews rather than using as a merge gate.

- **The operator went deaf 15–20 minutes after start, and its workers hung
  behind the dead watches.** Three stacked defects produced one symptom (runs
  materializing but never reaching a terminal phase, 893 in flight on the
  2026-09-18 smoke). First, a watch riding a connection the network had gone
  silent on hung forever: TCP idle, no HTTP/2 pings, no error, no delivery —
  only the startup LIST and the 10-minute resync still arrived. Second, every
  reconcile paid an apiserver GET per dequeued key, throttled by client-go's
  silent 5 QPS default, so a burst drained far slower than it filled. Third,
  those GETs carried no deadline, so when the connection died the GETs hung
  and the two workers stopped taking work at all — not even resync
  re-deliveries were processed. Fixed in `internal/operator/factory.go` (the
  shared apiserver client now carries TCP keepalive plus HTTP/2
  send-ping/ping-timeout health checks, and an explicit 50 QPS / 150 burst),
  `internal/operator/controller.go` (the reconcile reads its object from the
  informer store, falls back to one read when the key is gone, and every pass
  runs under a 60-second budget that turns a wedge into a retried failure),
  and `cmd/operator/main.go` (four workers). Live receipts 2026-09-18:
  `prepare` declared 1200 definitions without the revision-wait timeout that
  used to fire, the smoke test passed 65/65/0, and the loadtest smoke profile
  ended with the verdict below.

- **The "jobs informer blackout" reported by the 2026-09-18 upgrade smoke did
  not reproduce.** On the same build that had reported zero job reconciles, a
  labeled probe Job was delivered by both the startup LIST and the running
  watch in under a second, and a 33-Job burst was fully processed (reconcile
  counter 4 → 198). The 893 non-terminal runs that smoke reported are better
  attributed to its 60-second restart watchdog starving the workers behind a
  ~1300-key startup LIST than to a broken informer; no informer defect is
  known to exist.

- **Kubernetes mode had no load-test path.** The tool now drives the CR path end
  to end: `prepare` declares one `ScheduledJob` per generated definition
  (`scripts/loadtest/prepare.go:110`) and `verify` checks the ledger and the
  JobRun CRs (`scripts/loadtest/verify.go:452`). The dispatcher gate and the
  `job_instances` route the entry warned about were deleted with the legacy path.
- **The load tool sent `X-OrbitJob-Tenant`, which the server ignored.** The
  header is gone; `scripts/loadtest/api.go:289-294` sets only `Authorization`,
  `X-OrbitJob-Idempotency-Key` and `Content-Type`.
- **`reset` left run state behind.** Rewritten: the control-plane tables
  truncate first, the four RESTRICT-family tables delete before their tenants,
  and namespace cleanup is label-scoped to what the tool created
  (`scripts/loadtest/reset.go:43` and the steps that follow it). The legacy
  tables it used to miss no longer exist.
- **The tool's correctness checks could not see the runs that matter.** verify's
  checks read the run ledger, its attempts and CR reads
  (`scripts/loadtest/verify.go:181-238`); the `job_instances` reads they used to
  pass without looking through are gone.
- **Scenario family names overstated what the workloads do.** One family per
  body, enforced at build time by `TestCurlFamiliesReachTheirOwnEndpoint` and
  `TestNonCurlFamiliesMatchTheirBody` (`scripts/loadtest/generate_test.go:438`,
  `:503`).
- **What `preflight` does about images was unestablished.** Established:
  preflight validates the image lock — allowed repositories, index and
  per-platform digests, exactly six entries (`scripts/loadtest/images.go:41-72`)
  — and does not pull or probe the registry; the guide now describes exactly
  that.
- **Documentation drift in the load-test guide.** The `long`-as-qualification
  listing, the `expected-terminal-state` false-failure note and the stale family
  table are gone. The guide no longer hard-codes run history; workflow artifacts
  and local run directories are the evidence source for individual results.
