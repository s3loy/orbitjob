# Contribution guide

Discuss major features or interface changes with the maintainer first, describing
the problem scope, edge cases and verification plan. An Issue or PR can carry
that discussion when an external contribution needs review.

Issues and pull requests use a small multi-axis taxonomy: `kind/*` describes
the work, `area/*` identifies the affected subsystem, and `priority/*` records
maintainer scheduling. Pull requests receive objective `area/*` labels from
their changed paths. Priority, security impact, release-note eligibility, and
breaking changes are always classified by a maintainer.

## Development environment

- Go: use the version in [`go.mod`](go.mod); image build stages are in [`Dockerfile`](Dockerfile).
- PostgreSQL for integration tests: use the service image in [the Integration workflow](.github/workflows/integration.yml), on a disposable instance.
- golangci-lint: install the version pinned in [the Lint workflow](.github/workflows/lint.yml), rather than assuming the latest release matches CI.
- Helm, kind and kubectl for chart or Kubernetes changes. The [Load Test workflow](.github/workflows/loadtest.yml) records its Helm/kind pins; [the local guide](docs/local-deployment.md) explains prerequisites and side effects.

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
go mod download
```

## Branch strategy

Use `dev` as the shared development branch. Routine maintainer work does not
require a separate topic branch or self-authored PR.
`main` remains the release branch. Publication and promotion to `main` are
separate maintainer actions after the applicable quality gates pass.

External contributions may still use a topic branch and PR targeting `dev`.
Use a short kebab-case branch name when isolation is explicitly needed.
Pushes, merges and releases require separate maintainer authorization. Preserve
existing uncommitted changes when switching branches or integrating work.

## Code conventions

Dependency direction:

```text
platform <- core
platform <- admin
core does not import admin
admin/http -> admin/app -> core/domain
```

Check production imports with:

```bash
go list -f '{{.ImportPath}} -> {{join .Imports " "}}' ./internal/core/... \
  | grep -E 'orbitjob/internal/(admin|operator)'
```

No matches are expected; grep exits 1 when the output is empty.


Submitted code must:

- Use context-first APIs, with external dependencies injected through function variables
- Keep the domain layer free of HTTP types and admin DTOs
- Prefer the standard library for new dependencies, then reuse existing ones
- Use the unified error structure `{error:{code,message,field}}` in the Admin API
- Put writes in transactions; batch claims use `FOR UPDATE SKIP LOCKED`
- Use the `version` optimistic lock for concurrent updates
- Add metrics, structured logging or trace hooks to new code paths
- Implement authentication, input validation and permission checks with the feature

## Database migrations

File names use a four-digit ordinal:

```text
NNNN_description.up.sql
```

Versions must be contiguous from `0001`. The runner applies forward migrations
only and ignores `.down.sql` files; a down file is not a rollback mechanism.
Published migrations, or ones already recorded in `schema_migrations`, must
never be modified; the runner detects changes via SHA-256 checksums. Add a new
forward migration and document any backup/restore or compatibility boundary.

After editing `db/migrations/*.up.sql`, sync the Helm chart copy:

```bash
make helm-migrations-sync
make helm-migrations-check
```

Changes touching roles, RLS, ownership or `SECURITY DEFINER` functions must state:

- which PG role gains the privilege
- whether RLS is bypassed or enforced
- why `SECURITY DEFINER` instead of a tenant-scoped query
- whether rollback is reversible

The [migration and compatibility requirements](docs/database-setup.md#migration-execution-and-compatibility)
require stopping older writers before migration/deployment. A Helm
rolling upgrade alone does not implement that boundary. See
[database setup](docs/database-setup.md#writer-cutover).

## Testing

Test tiers, inside out:

1. Domain unit tests: pure logic
2. Use-case tests: mock external dependencies
3. Handler tests: `httptest`
4. Repository tests: `go-sqlmock`
5. Integration tests: real PostgreSQL, `//go:build integration`
6. Deployment verification: Helm or kind

Business logic and repository packages under
`internal/{core,admin}/{domain,app,store}` must meet a 60%
statement-coverage merge bar. Infrastructure, adapters, commands and operator
packages have no numeric gate. Declaration-only packages with no executable
statements are documented and validated in `scripts/coverage-baseline.txt`;
they are reported but have no meaningful percentage. Before submitting, run:

```bash
make check
make test-cover-check
```

Changes touching the database, repositories, migrations, bootstrap or RLS also require:

```bash
TEST_DATABASE_DSN='postgres://...' make integration
```

The integration target refuses an empty DSN and runs the shared-database packages
serially without cached results. Calling tagged Go tests directly without a DSN
can still skip individual tests; that is not integration evidence.

Changes touching Helm or Kubernetes require:

```bash
make helm-check
make kind-verify
```

`kind-verify` creates or reuses `orbitjob-verify` (overridable with
`KIND_CLUSTER_NAME`), switches kubeconfig context, and modifies its database
and Helm release. It exercises upgrade, failing migration, uninstall and
reinstall paths, so use a disposable cluster. A cluster created by the script
is removed on success and retained on failure; an existing cluster is retained.
State in the PR whether it actually ran and against which checkout. Helm
rendering and local tests do not prove a real installation or upgrade passed.

### Frontend checks

For `web/` changes, run `npm ci`, `npm run check`, `npm run format:check` and
`npm run test:browser` in that directory. Package scripts and the lockfile own
the frontend toolchain. See [frontend development](docs/local-deployment.md#change-check-and-redeploy)
for the browser install and API proxy. Generated declarations follow
`api/openapi.yaml`; `npm run generate` updates them and `types:check` detects
drift. Demo/mocked HTTP browser evidence does not replace installed-cluster
execution checks. Keep Go gates as well for API, routing or repository changes.
The nested `web/go.mod` excludes npm dependencies from root Go package discovery.
The real-cluster suite is prepared and run by `scripts/console-installed.py`, as documented in
[installed Console verification](docs/local-deployment.md#isolated-installation-and-console-checks).
It uses an explicit disposable kind kubeconfig and private credential file;
traces/video are disabled to keep Authorization out of test recordings.

### Benchmarks

`make bench` measures the ledger's pure hot paths: occurrence-key derivation, run phase transitions, check/probe-config normalization, Kubernetes Job rendering, terminal-outcome-to-SLI derivation, and leader election/locking. These are in-process functions with no database or cluster dependency. The target covers only packages listed in `BENCH_PACKAGES` in the Makefile and skips test suites; `make test` owns verification. Runtime depends on the host and benchmark inputs.

The run publishes `bench.txt` only after the benchmark process succeeds and
emits a numeric measurement. Producer failures, empty runs and stale artifacts
cannot count as a baseline. The Benchmark workflow stores a baseline on pushes
to `main` and compares PRs targeting `main` when a baseline exists; a PR to
`dev` does not receive that comparison automatically.

`make bench-etcd-memory` reruns the election benchmarks at higher count for benchstat-style comparisons. `make bench-etcd` does the same against a live etcd and needs `ETCD_ENDPOINTS` (default `localhost:2379`).

A change to a performance-sensitive path must include a benchmark alongside it:
table-driven over representative inputs, with `b.ReportAllocs`. State which
dependency costs it excludes. The current `make bench` target does not measure
database queries, a deployed cluster or end-to-end throughput.

## Local hooks

Commits are gated locally by [pre-commit](https://pre-commit.com) hooks that run CI's fast checks scoped to what you stage. One-time setup:

```bash
pip3 install pre-commit
brew install ggshield
ggshield auth login
make hooks
```

On macOS, Homebrew is the supported `ggshield` installation path; other
platforms can use an installation method from the GitGuardian documentation.
`ggshield auth login` stores a personal access token in the developer's local
GitGuardian configuration. Never put that token in this repository or an env
file under version control.

`make hooks` runs `pre-commit install`, which wires both the pre-commit and
commit-msg hooks. Hooks select staged paths, but package vet, OpenAPI and chart
checks may read their dependencies elsewhere in the checkout. The gates that
rewrite files — trailing-whitespace, end-of-file-fixer, mixed-line-ending,
go-fmt — exit non-zero after fixing: review the changes, `git add` them, and
commit again.

| Gate | Triggers on | Fails when |
|---|---|---|
| trailing-whitespace, end-of-file-fixer, mixed-line-ending | every commit | fixable whitespace, EOF-newline or line-ending issues (fixed; re-stage) |
| check-yaml | every commit | a YAML file does not parse (multi-doc allowed; chart templates excluded) |
| check-added-large-files | every commit | a staged file exceeds 1 MB |
| check-merge-conflict | every commit | conflict markers are staged |
| ggshield | staged changes | GitGuardian detects a possible secret or the scan cannot complete |
| go-fmt | staged `*.go` | `gofmt` rewrote a staged file (fixed; re-stage) |
| go-vet | staged `*.go` | `go vet` flags a package containing staged files |
| no-cjk | every commit | CJK characters appear outside `README.zh.md` and `site/` |
| no-artifacts | every commit | a staged file matches a repository `.gitignore` rule, including files added with `git add -f` |
| openapi-drift | `api/openapi.yaml`, `internal/admin/http/` or a domain `create_input.go` staged | `make openapi-check` reports spec drift |
| chart-gate | `charts/` staged | `make helm-check` fails |
| commit-msg | every commit | subject is empty, longer than 72 characters, or a `WIP` marker |

The heavyweight suites stay in CI — lint, unit/race/coverage, the integration suites, benchmark comparison, image builds; the pipeline map is `docs/ci.md`. Run `make check` before pushing rather than expecting the hooks to run every check.

A maintainer may authorize a hook bypass for a documented emergency, such as a
release cherry-pick out of a broken tree or a failure in the hook itself. Agents
must report the failure and must not infer that exception from permission to
commit. Preserve Git signing and hooks unless the maintainer explicitly directs
the specific exception; document any authorized bypass in the commit or PR.

## CI

The pipeline map — which workflow runs when, what each one proves, and where
artifacts land — is `docs/ci.md`. On PRs into `dev`: unit/race/coverage, the
integration suites, lint, govulncheck, dependency review, and a build of the
configured component images for both release platforms (no push), plus the
Console type, lint, format, unit, browser and build checks. Release and
pre-release publication additionally require the shared verification workflow
on the exact source revision, including disposable installation and complete
real Console execution. The Makefile
and release workflows define the component list. Benchmark comparison runs on
PRs into `main`. Load tests are manually dispatched advisory operator evidence,
never a required merge check.

## OpenAPI

After changing HTTP routes, request fields, enums or error responses, update `api/openapi.yaml`:

```bash
make openapi-gen
make openapi-check
```

Runtime documentation is at `GET /openapi.json`. If handler behavior temporarily cannot be expressed in OpenAPI, flag the deviation explicitly in the PR description and `USAGE.md`.

## Documentation

Maintain the complete English topic affected by a behavior change. Check prose,
commands, prerequisites, permissions and failure handling against the owning
implementation. README introduces the project; [USAGE](USAGE.md) indexes tasks.
Topic sources live under `docs/`, with security policy in `SECURITY.md`.

The site maps explicit topic sources to structured routes. Follow the
[documentation workflow](docs/project-site.md) for topic registration, source
links, generated HTTP reference and browser checks. Run:

```bash
make openapi-check
npm --prefix site run build
npm --prefix site run test:browser
```

Use the production artifact for reading and navigation checks. Execute affected
installation, API or recovery procedures separately in the appropriate isolated
environment; a successful site build does not establish operational correctness.
Update the distributed workload-assistant package when its references change.

Public procedures must work from the distributed source tree without internal
plans, decision records or raw logs. Write documentation and comments in English.
Maintain reviewed Chinese counterparts under `site/i18n/zh-CN/topics/` and the
current API dictionary alongside English changes. Check semantic equivalence
before updating the translation review baseline; hashes do not perform review.

The README does not record the version roadmap. Requirements and current
implementation/verification records retain their separate purposes.

## Commit format

Agents create local commits only when the maintainer explicitly requests them;
ordinary edits remain uncommitted. A commit request includes the necessary
staging within that scope, but does not authorize pushing or publication.

Conventional Commits:

```text
type(scope): description
```

Descriptions are lowercase English with no trailing period; the entire subject
is at most 72 characters. Common types: `feat`, `fix`, `refactor`, `test`, `docs`,
`chore`, `style`, `perf`.

| Scope | Purpose |
|---|---|
| `core/domain` | domain rules |
| `core/store` | core write store |
| `core/app` | core use cases |
| `admin/http` | HTTP handlers and middleware |
| `admin/store` | admin read store |
| `admin/app` | admin use cases |
| `schedule` | scheduler process |
| `operator` | operator process and CRD reconciler |
| `check` | Checks and CheckRuns |
| `slo` | SLIs, SLOs and error budgets |
| `bootstrap` | first-time initialization |
| `deploy` | Docker images, Helm and kind |
| `platform` | migrations, coordination layer, infrastructure |
| `ci` | CI/CD |
| `docs` | documentation |

Do not fold unrelated features into one commit, and do not split one logical unit into per-file commits.

## PR descriptions

When a PR is used, target `dev`. Write for the reviewer, not as release notes: start with why
the change exists, then explain the behavior or design that changed. A small
change may need only one sentence. A larger change should explain causality,
tradeoffs, migration impact, known limitations and any issue, ADR or prior PR
that provides necessary context.

Do not force a fixed section structure. Avoid enumerating files, commits or
every feature in the diff, and do not repeat CI checks visible on the PR.
Mention a test, benchmark, manual check, skipped test or failure only when it
materially helps review. Use headings or bullets only when they make a long
description easier to review.

Breaking changes, irreversible migrations, Secret key names and upgrade order
belong in the PR body, not only in review comments. Use `Fixes #issue` when the
PR resolves an issue. The author owns every claim; agent-assisted drafts must
be verified and rewritten in the author's own voice before submission.

## Code of conduct

Stay professional and friendly. Follow the [Contributor Covenant](https://www.contributor-covenant.org/).
