# CI pipeline map

Every GitHub Actions workflow lives in [`.github/workflows/`](../.github/workflows/).
Check run results and branch-rule enforcement on GitHub. Load tests are manually
dispatched advisory operations, outside the PR gate chain.

Go setup steps read the toolchain version from [go.mod](../go.mod) with
`go-version-file`; CI, benchmarks and load tests do not keep separate version
constants. Govulncheck checks both reachable module vulnerabilities and the
standard library in that selected toolchain. Updating module versions alone
cannot resolve a standard-library finding. A scan result belongs to its commit,
toolchain, scanner and vulnerability database at execution time.

## What runs when

| Workflow | Trigger | Checks | Outputs |
|---|---|---|---|
| CI (`ci.yml`) | push, PR → `main`/`dev` | unit + race tests, the 60% per-package coverage bar, OpenAPI freshness, Console verification, `make build-all`, configured image targets for `linux/amd64` **and** `linux/arm64` (no push); `CI / Gate` requires this chain to succeed | `binaries` artifact (7 days), Codecov upload |
| Documentation (`docs.yml`) | manual dispatch on `main` | verifies the selected commit and deploys to GitHub Pages | verified site artifact (1 day), Pages artifact, failed browser diagnostics (7 days) |
| Documentation verification (`docs-verification.yml`) | called for manual GitHub Pages deployment and formal release with one source commit | OpenAPI freshness, English/Chinese topic and API build, links, localized search and browser usability | failed browser diagnostics (7 days), verified site artifact when requested by the deployment workflow |
| Console (`console.yml`) | called by CI and release verification | locked dependencies, generated contracts, TypeScript, ESLint, unit tests, production build, formatting and fixture/mocked-HTTP browser tests | failure diagnostics (7 days) |
| Integration (`integration.yml`) | push, PR → `main`/`dev`, manual, release verification | eight packages: migrations, Postgres helpers, admin/core repositories, bootstrap, admin HTTP, load-tool isolation and installed-fixture credential retirement, using the workflow's PostgreSQL service image and provisioned deployment roles | — |
| Lint (`lint.yml`) | push, PR → `main`/`dev`, manual | golangci-lint | — |
| Govulncheck (`govulncheck.yml`) | push, PR → `main`/`dev`, manual, release verification | reachable vulnerabilities for `linux/amd64` and `linux/arm64`, using the configured scanner and database at run time | — |
| Dependency Review (`dependency-review.yml`) | PR → `main`/`dev` | dependency diff against the action's high-severity failure threshold | — |
| Benchmark (`benchmark.yml`) | push, PR → `main` | push stores `bench.txt` per commit; PR posts a comparison comment | benchmark history (committed by the workflow) |
| Load Test (`loadtest.yml`) | manual | installs kind and attempts advisory end-to-end evidence collection; workflow success is not a qualification verdict | `loadtest-<profile>-<run_id>` artifact (14 days) + run-page job summary |
| Release verification (`verification.yml`) | called by release and prerelease with one source commit | backend quality, coverage, Helm, PostgreSQL, vulnerabilities and Console checks. For formal releases, also checks the documentation. Installation lifecycle and complete installed Console tests follow | safe installed-verification receipt, failed documentation browser diagnostics (7 days) for formal releases |
| Pre-release Images (`prerelease.yml`) | push to `dev`, manual | shared release verification, then the configured component matrix pushed to GHCR as `rc-<run>-<sha>`; no repository tag is created | versioned prerelease images and mutable `rc` aliases |
| Release (`release.yml`) | tag `v*` | checks tag/chart version agreement, requires the tag to resolve to the configured default branch's current commit and version tags to be unused, completes shared release verification, publishes versioned images and packages the chart; no `latest` alias | `helm-chart` artifact (90 days), published images |

`DOCKER_COMPONENTS` in the [Makefile](../Makefile) defines the image targets for
the PR build and `make docker-build`, including the execution-log archive.
Release/prerelease workflows define their publication matrices; component
changes must keep those and the chart aligned. Image builds, installation
verification and registry publication are separate steps.

Release verification checks out the same commit in every required job.
For formal releases, Documentation verification uses read-only repository
permissions and confirms that its checkout matches the requested full commit
SHA. Its OpenAPI, build and browser checks must pass before installation starts.
A failure blocks formal release image publication. The Documentation workflow
runs the same checks for a manual GitHub Pages deployment from `main`.

The [Cloudflare Pages Git integration](project-site.md#cloudflare-pages)
automatically builds and deploys the site from `main` and publishes previews from
`dev`. Cloudflare runs the site build and artifact checks independently of GitHub
Actions. Both Pages hosts import the available benchmark archive at build time.

The installation job creates an isolated kind cluster and kubeconfig, runs the
installation lifecycle gate, then prepares and runs all installed Console
cases against that installation. The runner rejects skipped, retried or
incomplete results. Cleanup retires fixture credentials and removes the owned
cluster even after a failure. Credentials and raw request/response logs are not
uploaded. Controlled business recovery and old-version upgrade exercises remain
additional release evidence; the fresh-install lifecycle gate does not prove them.

The mutable `rc` aliases are updated one component at a time after all versioned
images publish. An interruption can leave mixed aliases. Use the explicit
`rc-<run>-<sha>` run tag for an installation and record each image digest.
Re-running that workflow can publish the same run tag again; use the recorded
digests when the installation must reproduce the exact verified artifacts.

PR check contexts are `CI / Gate`, `postgres-integration`, `golangci-lint`,
`govulncheck` and `dependency-review`. Which ones are required is a remote
repository/ruleset setting, not established by these workflow files. `CI / Gate`
summarizes only the dependency chain in `ci.yml`; it does not include the
independent Integration, Lint or security workflows. Benchmark comparison is
review evidence. Push events use distinct `*-post-merge` check names, including
`CI / Post-merge`, so a successful dev push cannot satisfy required
pull-request contexts for the same commit.

Pull requests are labeled from changed paths by `.github/labeler.yml`. Path
rules only assign objective `area/*` labels. Priority, security impact,
release-note eligibility, and breaking-change labels require maintainer
judgment.

The hosted load workflow offers only `smoke` and `standard`, with job budgets
of 90 and 350 minutes respectively. It does not offer the eight-hour `long`
profile; running that schedule requires a separately managed, isolated host.
See [the load guide](loadtest-guide.md) for execution and evidence limits.

The runner's settle loop calls `loadtest settle` with the current run's saved
tenant/revision mapping. Each read sets transaction-local tenant RLS context;
query failures and missing observations remain unknown, never a zero count.
Wait exhaustion warns and proceeds to independent verification; settling alone
does not establish that all CRs completed or produce a qualification verdict.

A successful Load Test workflow reports successful harness steps. The verifier
writes `result.json` and returns success even when its product verdict is
`FAIL` or `INCONCLUSIVE`; read that verdict and its individual checks. Independent recovery evidence checks require prepared canaries, source observations
and continuous fixture journals; missing or contradictory observations remain
`INCONCLUSIVE`. Ready replicas alone do not produce qualification `PASS`. See [verification](verification.md) for
executed checks and [known issues](known-issues.md) for qualification gaps.

## Manual triggers

```bash
gh workflow run loadtest.yml -f profile=standard   # ad-hoc load profile
gh workflow run integration.yml                    # re-run integration suites
gh workflow run docs.yml --ref main                # verify and deploy GitHub Pages
```

The Actions UI offers the same manual triggers. Formal releases are tag-push
only: a version is consumed once any release image exists, so a failed partial
publication must use a new version rather than overwrite an existing tag.

## Artifacts

All artifacts attach to the workflow run page (**Actions → run → Artifacts**). `loadtest` runs additionally get a job summary with profile, verdict, duration and the artifact link; the full report from the run is embedded there. Retention: binaries 7 days, load-test runs 14 days, packaged charts 90 days.

The `binaries` artifact contains the Linux/amd64 runtime commands, configuration
utility and CLI built by `make build-all`. It is a CI artifact, not a portable
multi-platform CLI release archive. The release workflow publishes container
images and uploads the packaged Helm chart; it does not create a GitHub Release.

## Local equivalents

| CI runs | Locally |
|---|---|
| `make test`, `make test-race`, `make test-cover-check` (60% bar) | same |
| Integration suites | `TEST_DATABASE_DSN='postgres://...' make integration` |
| `make openapi-check` | same; regenerate intentionally with `make openapi-gen` before committing schema changes |
| golangci-lint | `make lint` with the version pinned in `lint.yml` |
| Component image build (amd64 + arm64) | `make docker-build` (native arch only) |
| Benchmark store/compare | `make bench` |
| Console verification | `npm --prefix web ci`, `npm --prefix web run check`, `npm --prefix web run format:check`, `npm --prefix web run test:browser` |
| Installation and real Console | `make kind-verify`, then [installed Console verification](local-deployment.md#isolated-installation-and-console-checks) in the same disposable installation |
| Load profile | The isolated setup and commands in [load testing](loadtest-guide.md) |

`make check` combines lint, vet, race tests, OpenAPI freshness and
`go mod tidy -diff`. The tidy check does not modify module files or require a
clean Git tree. Coverage, tagged integration and Helm checks are separate
targets. The general PR CI chain does not invoke every `make check` prerequisite
as a standalone job; release and prerelease use the shared verification workflow
and require all its jobs before publishing images.

`make integration` runs the same eight packages serially with `-count=1`.
The workflow invokes that same target. An empty `TEST_DATABASE_DSN` fails before
Go starts, so skipped database suites cannot satisfy the target. Set the variable
to a disposable PostgreSQL server: some suites rebuild
`public`, and managed login roles are server-wide. See
[local deployment](local-deployment.md#change-check-and-redeploy). The local load
runner resets all tool-owned load tenants/namespaces on its target installation
before generating a new run; review that scope before invoking it.

`make bench` propagates the benchmark process's exit status and requires at least
one numeric measurement. It removes stale `bench.txt` first and publishes a fresh
file only after a successful measured run; empty output and partial failures
cannot feed benchmark history/comparison.

The load summary is bound to the attempted run directory emitted by the runner
before setup. An early setup or verifier failure cannot use a PASS result from an earlier run.
Manual trigger receipts are reconciled individually with projected ledger rows;
rejections, skips and missing projections remain `INCONCLUSIVE`. Execution config
and source identity are recorded when the workload starts, with details in
[the load guide](loadtest-guide.md).
