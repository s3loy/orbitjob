# CI pipeline map

Every workflow lives in `.github/workflows/`. PR checks gate merges into `main` and `dev`. Scheduled workflows fire only from the default branch, so the nightly entries below activate on `main`.

## What runs when

| Workflow | Trigger | Proves | Leaves behind |
|---|---|---|---|
| CI (`ci.yml`) | push, PR → `main`/`dev` | unit + race tests, coverage bar, OpenAPI freshness, all binaries, all five image targets build for `linux/amd64` **and** `linux/arm64` (no push) | `binaries` artifact (7 days), Codecov upload |
| Integration (`integration.yml`) | push, PR → `main`/`dev`, manual | six suites (`db/migrations`, `postgrestest`, admin + core repositories, bootstrap, admin HTTP) against real PostgreSQL 17, with deployment roles provisioned | — |
| Lint (`lint.yml`) | push, PR → `main`/`dev`, manual | golangci-lint | — |
| Govulncheck (`govulncheck.yml`) | push, PR → `main`/`dev`, manual | no known vulnerabilities reachable from the module | — |
| Dependency Review (`dependency-review.yml`) | PR → `main`/`dev` | dependency diff introduces no high-severity vulnerability | — |
| Benchmark (`benchmark.yml`) | push, PR → `main` | push stores `bench.txt` per commit; PR posts a comparison comment | benchmark history (committed by the workflow) |
| Load Test (`loadtest.yml`) | push to `refactor` (smoke); weekly Wednesday 02:30 UTC smoke; Saturday 22:00 UTC standard; manual | a load profile end-to-end against a real kind installation (`scripts/quickstart.sh` + `scripts/loadtest-ci.sh`) | `loadtest-<profile>-<run_id>` artifact (14 days) + run-page job summary |
| Pre-release Images (`prerelease.yml`) | push to `refactor`, manual | quality gate, then five images pushed to GHCR as `rc-<run>-<sha>` plus a full-success `rc` alias; no repository tag is created | published `rc` images |
| Release (`release.yml`) | tag `v*`, manual | quality gate, five images pushed to GHCR (Docker Hub when credentials exist), chart packaged | `helm-chart` artifact (90 days), published images |

The five image targets — `admin-api`, `scheduler`, `operator`, `migrate`, `bootstrap` — are defined once in the Makefile (`DOCKER_COMPONENTS`). CI parses that list, so the PR check and the dev loop (`make docker-build`) cannot drift.

## Schedules

- `30 2 * * 3` — weekly **smoke** (Wednesdays, ~40 min, non-qualifying). Off-peak UTC: evening Americas, small hours Europe.
- `0 22 * * 6` — weekly **standard** qualification (~350 min, the qualifying run), Saturday night UTC.
- Every push to `refactor` also runs a smoke profile until the workflows reach the default branch and the schedule/dispatch unlock fully applies there.

The two never share a night window: the workflow's concurrency group (`loadtest`, no cancellation) serializes profiles, so same-night sequencing would occupy one hosted runner for ~6.5 hours straight. Standard runs weekly rather than nightly because it exists to catch drift, and each run costs hours of hosted-runner time.

GitHub caps a hosted job at **360 minutes (6 hours)**. That is why the 8-hour `long` soak is not offered in CI at all — it cannot fit even before install, settle and verify — and runs on the always-on dev cluster instead (see `docs/loadtest-guide.md`). Scheduled runs may start late under GitHub load; the verdict carries its own timings, so a delayed start does not distort the result.

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
| `make test`, `make test-race`, `make test-cover-check` | same |
| Integration suites | `TEST_DATABASE_DSN='postgres://...' make integration` |
| `make openapi-gen` + `openapi-check` | same |
| Five-target image build (amd64 + arm64) | `make docker-build` (native arch only) |
| Benchmark store/compare | `make bench` |
| Load profile | `scripts/quickstart.sh` then `scripts/loadtest-ci.sh smoke` against the kind cluster |
