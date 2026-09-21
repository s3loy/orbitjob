# CI pipeline map

Every workflow lives in `.github/workflows/`. PR checks gate merges into `main`
and `dev`. Load tests are separate, manually dispatched advisory operations:
they never participate in the required merge-check set.

## What runs when

| Workflow | Trigger | Proves | Leaves behind |
|---|---|---|---|
| CI (`ci.yml`) | push, PR → `main`/`dev` | unit + race tests, the 60% per-package coverage bar, OpenAPI freshness, all binaries, all five image targets build for `linux/amd64` **and** `linux/arm64` (no push) | `binaries` artifact (7 days), Codecov upload |
| Integration (`integration.yml`) | push, PR → `main`/`dev`, manual | six suites (`db/migrations`, `postgrestest`, admin + core repositories, bootstrap, admin HTTP) against real PostgreSQL 17, with deployment roles provisioned | — |
| Lint (`lint.yml`) | push, PR → `main`/`dev`, manual | golangci-lint | — |
| Govulncheck (`govulncheck.yml`) | push, PR → `main`/`dev`, manual | no known vulnerabilities reachable from the module | — |
| Dependency Review (`dependency-review.yml`) | PR → `main`/`dev` | dependency diff introduces no high-severity vulnerability | — |
| Benchmark (`benchmark.yml`) | push, PR → `main` | push stores `bench.txt` per commit; PR posts a comparison comment | benchmark history (committed by the workflow) |
| Load Test (`loadtest.yml`) | manual | advisory end-to-end evidence against a real kind installation; never a merge gate | `loadtest-<profile>-<run_id>` artifact (14 days) + run-page job summary |
| Pre-release Images (`prerelease.yml`) | push to `refactor`, manual | quality gate, then five images pushed to GHCR as `rc-<run>-<sha>` plus a full-success `rc` alias; no repository tag is created | published `rc` images |
| Release (`release.yml`) | tag `v*`, manual | quality gate, five images pushed to GHCR (Docker Hub when credentials exist), chart packaged | `helm-chart` artifact (90 days), published images |

The five image targets — `admin-api`, `scheduler`, `operator`, `migrate`, `bootstrap` — are defined once in the Makefile (`DOCKER_COMPONENTS`). CI parses that list, so the PR check and the dev loop (`make docker-build`) cannot drift.

GitHub caps a hosted job at **360 minutes (6 hours)**. That is why the 8-hour
`long` soak is not offered in GitHub Actions; an operator runs it on the
always-on dev cluster when useful (see `docs/loadtest-guide.md`).

A green Load Test workflow means setup, execution, evidence collection and
artifact upload completed. It does **not** turn the product verdict into a merge
decision: read `result.json` or the job summary for `PASS`, `FAIL` or
`INCONCLUSIVE`. This separation is intentional because load tests are reviewed
by an operator and are not required CI.

## Manual triggers

```bash
gh workflow run loadtest.yml -f profile=standard   # ad-hoc load profile
gh workflow run integration.yml                    # re-run integration suites
gh workflow run release.yml -f tag=v0.2.2          # re-release an existing tag
```

The Actions UI offers the same under **Run workflow**.

## Artifacts

All artifacts attach to the workflow run page (**Actions → run → Artifacts**). `loadtest` runs additionally get a job summary with profile, verdict, duration and the artifact link; the full report from the run is embedded there. Retention: binaries 7 days, load-test runs 14 days, packaged charts 90 days.

## Local equivalents

| CI runs | Locally |
|---|---|
| `make test`, `make test-race`, `make test-cover-check` (60% bar) | same |
| Integration suites | `TEST_DATABASE_DSN='postgres://...' make integration` |
| `make openapi-gen` + `openapi-check` | same |
| Five-target image build (amd64 + arm64) | `make docker-build` (native arch only) |
| Benchmark store/compare | `make bench` |
| Load profile | `scripts/quickstart.sh` then `scripts/loadtest-ci.sh smoke` against the kind cluster |
