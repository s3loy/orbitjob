# Contribution Guide

Open an Issue before submitting major features or interface changes, describing the problem scope, edge cases and verification plan. Small fixes can go straight to a PR.

## Development environment

- Go 1.27.1
- PostgreSQL 17
- golangci-lint latest (version auto-updates with CI)
- Helm 3, kind, kubectl (for chart or Kubernetes changes)

```bash
git clone https://github.com/s3loy/orbitjob.git
cd orbitjob
go mod download
```

## Branch strategy

Create development branches from `dev` and merge PRs back into `dev`. Maintainers merge `dev` into `main` on the release cadence.

Branch naming:

```text
feat/<description>
fix/<description>
refactor/<description>
chore/<description>
```

Use kebab-case; one branch, one topic.

## Code conventions

Dependency direction:

```text
platform <- core
platform <- admin
core does not import admin
admin/http -> admin/app -> core/domain
```

Submitted code must:

- Use context-first APIs, with external dependencies injected through function variables
- Keep the domain layer free of HTTP types and admin DTOs
- Prefer the standard library for new dependencies, then reuse existing ones
- Use the unified error structure `{error:{code,message,field}}` in the Admin API
- Put writes in transactions; batch claims use `FOR UPDATE SKIP LOCKED`
- Use the `version` optimistic lock for concurrent updates
- Add metrics, structured logging or trace hooks to new code paths
- Ship authentication, input validation and permission checks together with the feature

## Database migrations

File names use a four-digit ordinal:

```text
NNNN_description.up.sql
NNNN_description.down.sql
```

Irreversible migrations add an `_irreversible` suffix to the down file name and explain the reason inside the file. Published migrations, or ones already recorded in `schema_migrations`, must never be modified; the runner detects changes via SHA-256 checksums.

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

Changes touching Helm or Kubernetes require:

```bash
make helm-check
make kind-verify
```

`kind-verify` creates a local kind cluster and takes longer than unit tests. State in the PR whether you actually ran it; never mark "not run" as "passed".

### Benchmarks

`make bench` measures the ledger's pure hot paths: occurrence-key derivation (the dedup cornerstone), run phase transitions, check/probe-config normalization, Kubernetes Job rendering, terminal-outcome-to-SLI derivation, and leader election/locking. These are in-process functions — no database, no cluster — so the target finishes in about a minute. It covers only packages that carry benchmarks and skips the test suites; `make test` owns verification.

The run writes `bench.txt`, which the Benchmark workflow tracks per commit; check the comparison it posts on your PR before merging a change to a benched path.

`make bench-etcd-memory` reruns the election benchmarks at higher count for benchstat-style comparisons. `make bench-etcd` does the same against a live etcd and needs `ETCD_ENDPOINTS` (default `localhost:2379`).

A change to a performance-sensitive path ships with a benchmark next to it: table-driven over representative inputs, with `b.ReportAllocs`, and honest — a trivially fast function says so in a comment rather than inflating the scenario. Benchmarks that need the ledger database are out of scope for now; once the store workstream settles they will land as a separate tagged target, not inside `make bench`.

## Local hooks

Commits are gated locally by [pre-commit](https://pre-commit.com) hooks that run CI's fast checks scoped to what you stage. One-time setup:

```bash
pip3 install pre-commit
make hooks
```

`make hooks` runs `pre-commit install`, which wires both the pre-commit and commit-msg hooks. No gate touches unstaged or unrelated files. The gates that rewrite files — trailing-whitespace, end-of-file-fixer, mixed-line-ending, go-fmt — exit non-zero after fixing: review the changes, `git add` them, and commit again.

| Gate | Triggers on | Fails when |
|---|---|---|
| trailing-whitespace, end-of-file-fixer, mixed-line-ending | every commit | fixable whitespace, EOF-newline or line-ending issues (fixed; re-stage) |
| check-yaml | every commit | a YAML file does not parse (multi-doc allowed; chart templates excluded) |
| check-added-large-files | every commit | a staged file exceeds 1 MB |
| check-merge-conflict | every commit | conflict markers are staged |
| go-fmt | staged `*.go` | `gofmt` rewrote a staged file (fixed; re-stage) |
| go-vet | staged `*.go` | `go vet` flags a package containing staged files |
| no-cjk | every commit | CJK characters appear outside `README.zh.md` |
| no-artifacts | every commit | root binaries (`operator`, `orbitjob`), `smoke-test-results.md` or `bench.txt`/`bench-*.txt` are staged |
| openapi-drift | `api/openapi.yaml`, `internal/admin/http/` or a domain `create_input.go` staged | `make openapi-check` reports spec drift |
| chart-gate | `charts/` staged | `make helm-check` fails |
| commit-msg | every commit | subject is empty, longer than 72 characters, or a `WIP` marker |

The heavyweight suites stay in CI — lint, unit/race/coverage, the integration suites, benchmark comparison, image builds; the pipeline map is `docs/ci.md`. Run `make check` before pushing rather than expecting the hooks to run the world.

Bypass with `git commit --no-verify`, but only in documented emergencies — a release cherry-pick out of a broken tree, or a failure caused by a hook itself — and say so in the commit or PR description. Never use it to skip a failure you have not read.

## CI

The pipeline map — which workflow runs when, what each one proves, and where
artifacts land — is `docs/ci.md`. On PRs into `dev`: unit/race/coverage, the
integration suites, lint, govulncheck, dependency review, and a build of all
five images for both release platforms (no push). Benchmark comparison runs on
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

Code and documentation change in the same PR:

- Project positioning and quick start: `README.md` (English), `README.zh.md` (Chinese variant)
- Operating steps and curl examples: `USAGE.md`
- Security boundaries: `SECURITY.md`
- API schema: `api/openapi.yaml`

The README does not record the version roadmap. Roadmaps and architecture plans live in Issues, PRs or maintainer-designated design documents.

## Commit format

Conventional Commits:

```text
type(scope): description
```

Descriptions are lowercase English, no trailing period, at most 72 characters. Common types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `style`, `perf`.

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
| `deploy` | Docker, Compose, Helm and kind |
| `platform` | migrations, coordination layer, infrastructure |
| `ci` | CI/CD |
| `docs` | documentation |

Do not fold unrelated features into one commit, and do not split one logical unit into per-file commits.

## PR content

PRs target `dev`. The description contains at least:

```markdown
## Summary
The problem solved and the user- or operator-visible change.

## Changes
Main code, database and deployment changes.

## Testing
How it was tested and the results.

## Related
Related Issues and design documents. Use `Fixes #issue` to auto-close.
```

Breaking changes, irreversible migrations, Secret key names and upgrade order belong in the PR body, not only in review comments.

## Code of conduct

Stay professional and friendly. Follow the [Contributor Covenant](https://www.contributor-covenant.org/).
