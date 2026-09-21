# OrbitJob

[![Go](https://img.shields.io/badge/Go-1.27.1-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/github/license/s3loy/orbitjob)](./LICENSE)
[![golangci-lint](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/lint.yml)
[![Build Status](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/ci.yml)
[![govulncheck](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/s3loy/orbitjob/actions/workflows/govulncheck.yml)
[![Coverage Status](https://codecov.io/gh/s3loy/orbitjob/graph/badge.svg)](https://codecov.io/gh/s3loy/orbitjob)

[Chinese](./README.zh.md)

Kubernetes-native run ledger. OrbitJob answers three questions about scheduled work: did it run, how many times, and who triggered it. Definitions are `ScheduledJob` custom resources; every run — scheduled, manual, or a check probe — is a `JobRun` custom resource that the operator renders into a Kubernetes Job and records in the PostgreSQL ledger. Three processes: `admin-api` serves the HTTP API and reads the ledger, `operator` fires due schedules and checks, reconciles the CRs into Kubernetes Jobs and writes the ledger, `scheduler` evaluates SLIs against error budgets.

Cron and manual triggers, retry policy with attempt accounting, concurrency and misfire policy, and cancel-via-CR-patch. Checks execute as Kubernetes Jobs too — a digest-pinned curl probe per occurrence. The admin API is SELECT-only against the database; a compromised component cannot forge the ledger.

## Quick Start

The quickest released installation uses Kind + Helm. It requires `kind`,
`kubectl`, `helm`, and `make`.

```bash
# 1. Create the kind cluster (cluster only, no database)
make kind-up

# 2. Install PostgreSQL and generate the orbitjob-database Secret
make kind-db

# 3. Install the v0.2.1 images; the values file supplies the local tenant mapping
helm upgrade --install orbitjob ./charts/orbitjob \
  --namespace orbitjob-system \
  -f deploy/kind/values-dev.yaml \
  --set-string global.imageTag=v0.2.1 \
  --set-string global.imagePullPolicy=IfNotPresent \
  --wait --timeout=10m

# 4. Export the API key and verify
source <(make kind-env)
kubectl -n orbitjob-system port-forward svc/orbitjob-admin-api 18080:8080 &
curl -H "Authorization: Bearer $ORBITJOB_API_KEY" http://localhost:18080/api/v1/tenants
```

For local source builds, run `bash scripts/quickstart.sh`; it builds and loads
`:dev` images. See [`docs/local-deployment.md`](docs/local-deployment.md).

The admin API also serves `/metrics` and `/openapi.json`. Observability is a separate kube-prometheus-stack release:

```bash
make monitoring-up
kubectl -n monitoring port-forward svc/kube-prometheus-stack-prometheus 9090:9090 &
kubectl -n monitoring port-forward svc/kube-prometheus-stack-grafana 3000:80 &
```

Images are pulled from `ghcr.io/s3loy`, built and pushed by CI on tag. To verify code changes on a local cluster, see the local build section in [`docs/local-deployment.md`](docs/local-deployment.md).

Full kind guide: [`docs/local-deployment.md`](docs/local-deployment.md).

## Helm

```bash
helm upgrade --install orbitjob charts/orbitjob \
  --namespace orbitjob-system --create-namespace \
  -f my-values.yaml \
  --set-string global.imageTag=v0.2.1
```

`operator.namespaceTenants` is mandatory: the namespace-to-tenant mapping. Installation fails when it is missing. The chart does not install PostgreSQL. Database configuration: [`docs/database-setup.md`](docs/database-setup.md).

## Development

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob

make test          # unit tests
make test-cover    # coverage
make check         # lint + vet + race + openapi-check + tidy-check
make integration   # integration tests (requires TEST_DATABASE_DSN)
```

Branch strategy, testing, commit format: [CONTRIBUTING.md](./CONTRIBUTING.md).

## Features

An incomplete list of features OrbitJob provides:

* Scheduled (cron) and manual triggers
* Retry (with attempt accounting), concurrency control, misfire and history-retention policies
* Run cancellation
* DAG workflows with task dependencies and conditional gating
* Serverless functions invoked over HTTP
* Multi-tenant isolation: API key grants, PostgreSQL roles + RLS, same-transaction audit records
* Checks producing SLIs, bound to SLOs for error budgets and burn-rate alerts
* Prometheus metrics, Grafana dashboard, structured logging, trace IDs
* Leader election (Kubernetes Lease for the operator, optional etcd for the scheduler)

## Documentation

- [USAGE.md](./USAGE.md) - deployment, API examples, operations, troubleshooting
- [CONTRIBUTING.md](./CONTRIBUTING.md) - branch, dependency, testing, migration, commit, PR
- [SECURITY.md](./SECURITY.md) - security model, vulnerability reporting, PostgreSQL roles and RLS, container security
- [docs/database-setup.md](docs/database-setup.md) - PostgreSQL configuration, TLS, Kubernetes Secret contract
- [docs/local-deployment.md](docs/local-deployment.md) - full kind local development guide
- [docs/architecture.md](docs/architecture.md) - components, processes and data flow
- [api/openapi.yaml](./api/openapi.yaml) - API schema

## License

[BSD 3-Clause](./LICENSE)
