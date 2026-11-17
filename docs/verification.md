# Verification

This page records verification scope separately from product behavior.
[Project status](status.md) summarizes the implementation;
[known issues](known-issues.md) lists unresolved limitations.
[CONTRIBUTING](../CONTRIBUTING.md#testing) defines the required quality gates.

## Legacy Check upgrade and release preparation (2026-11-17)

A separate installed pass closes the Check coverage gap recorded in the delivery
candidate section below. It used the five actual published Linux arm64 v0.2.1
payloads at `d12cca01a2e16eba808043dc98358f35ace4709e` and the same candidate
application images as that section. The eight implementation and guide commits
ending at `8f77723b957c` match the accepted source capsule's application,
deployment, workflow, site and bilingual-guide inputs. Release prose and this
verification record are recorded separately.

The dedicated single-node kind cluster used Kubernetes and kubectl 1.37.0,
PostgreSQL 17, a 5-GiB database claim and a new 10-GiB archive with the chart's
default budgets. It used a separate kubeconfig and a namespace-local HTTP probe
target. Old and current executions used the same pinned official probe image.

| Check | Result and scope |
|---|---|
| Old Check interfaces | The actual old API created an active Check and an originally paused control. Create/read/pause/resume and definition-version CAS passed. A stale version returned 409 without changing the definition. The main Check reached an acknowledged paused version 4; the control remained at version 2. |
| Old execution and drain | The old Check produced Run ID 1 / Attempt ID 1 with a successful physical Job and Pod. The run ledger, terminal Check readmodel, Attempt, Job UID, Pod UID and exit code were bound. The old JobRun CR initially lagged those facts and converged through the normal ten-minute informer resynchronization. No manual status, finalizer, cursor or run-ledger write was used to satisfy the drain. |
| Pre-upgrade recovery | After the complete drain and old-writer stop, the backup was restored in an isolated namespace before forward migrations. All 21 table summaries, 14 sequences and six migration rows matched. The old Admin read both Checks and their actual run and attempt history using the original API key; no restored Operator or Scheduler ran. |
| Forward upgrade | Normal Helm upgrade advanced the migration ledger from six to 28 entries. The original six ledger rows and mounted migration ConfigMap bytes remained unchanged. Both Check identities, creation times, versions, cursors and configuration, the old execution history, API-key identity and grants were retained. The candidate reported legacy execution policy version 0. |
| Same-source execution | The candidate API resumed the same Check from version 4 to 5. It automatically produced Run ID 2 / Attempt ID 2 (attempt number 1), which succeeded with a bound new Job and Pod and exit code 0. Its readmodel reported success, one associated good SLI event existed, and the created/completed/good/snapshot metric increments were each 1. |
| Guarded pause and repeat | The Check was paused again at version 6. No new automatic run was accepted during the following 42 seconds. The originally paused control remained unchanged. A normal repeated Helm upgrade reached revision 3 / deployed with 28 unchanged migration rows, both Checks still paused and the original key still valid. |
| Retirement | Components were ready before retirement. Nonterminal runs and Workflows, active workload Pods, accepted-but-unpublished work and termination-evidence obligations were zero before and after stopping writers. The collector stopped before the archive; the old recovery reader stopped. The owned kind cluster was deleted, the default kubeconfig was unchanged and all three original shared-service identities and running states were preserved. The 12 private backup-file checksums were reverified after cleanup. |

Two failed preparation observations were retained: the temporary driver's empty
Job-list assumption did not account for Check projections, and its first drain
window was shorter than the old Operator's normal resynchronization period.
The corrected inventory allowed only the recorded Check projections; the longer
wait preserved the drain assertions and product inputs. No product correction
was needed for this supplementary pass.

Local release preparation also reran the Function, Admin HTTP and script package
tests without cached results, all 12 archive-export unit cases and the complete
site build. The build passed its 18 script cases and checked 577 current pages,
34 reviewed topic pairs and 106 operations; the two retained benchmark files
kept their bytes. The preceding dual-base browser results apply to unchanged
site inputs. Independent review checked each of the eight intermediate commit
trees, all translation-input hashes, logical boundaries and SSH signatures.
Normal local commit hooks remained enabled.

This pass qualifies the recorded legacy Check path on local Linux arm64 kind.
It does not qualify new version 1 Check creation, production throughput, HA,
CNI NetworkPolicy enforcement, a post-upgrade database downgrade or coordinated
archive-store recovery. Hosted CI and publication remain maintainer actions.

## Delivery candidate acceptance (2026-11-17)

This pass checks the local `dev` working tree after `4184f981dbf6`. Installed
checks use frozen application inputs and recorded image IDs. The documentation
was rebuilt after the final English and Chinese upgrade-guide corrections.

Function invocation now retains its last observed phase when waiting ends
without a newer observation. HTTP and Console exports retain an incomplete
result after archive loss on any page and describe missing gap bounds as
unknown. Regression cases distinguish these fixes from the preceding behavior.
Prerelease instructions now include the required installation configuration
and all seven component tags, including overrides of retained component tags.

The installed CI path now authorizes its dedicated native fixture writer after
the installation lifecycle, before fixture preparation. Cluster, context and
administrator identity checks bound that authorization to the disposable kind
environment. Production `admission.nativeWriters` remains empty by default.
The account installer's refusal to reuse a claimed installation was preserved;
the complete run used a new installation rather than resetting account state.

| Check | Result and scope |
|---|---|
| Backend | `make check`, the 51-package coverage gate and Helm checks passed after the Function and export corrections. The two declaration-only coverage exceptions remain documented. |
| PostgreSQL baseline | The eight-package Go 1.27.4 integration result in the toolchain section below covers the unchanged migrations and database code. This pass additionally exercised a published-release database through upgrade and isolated restore. |
| Vulnerabilities | Govulncheck passed for `linux/amd64` and `linux/arm64`; three module-level advisories remain outside imported vulnerable packages and reachable calls. |
| Console | Generated references, TypeScript, lint, 515 unit cases, formatting and production build passed. All 400 browser-fixture cases passed without retries or skips. |
| Documentation | All 18 script cases and 37 browser cases passed at both `/orbitjob` and `/review`, including the final upgrade clarifications. Each build checked 577 current pages, 34 reviewed topic pairs and 106 operations. The two historical benchmark files retained their bytes, and the default-base artifact was restored. |
| Fresh installation | A dedicated Linux arm64 kind cluster with Kubernetes and task-local kubectl 1.37 passed fresh install, repeated upgrade, expected migration failure and rollback, uninstall and reinstall. The exact corrected CI writer step then prepared the fixtures. |
| Complete installed Console | All 10 spec files and 40 cases passed with `completeSuite=true` and `passed=true`, without skips, expected failures or retries. The harness input manifest remained equal before and after the run. |
| First-job tutorial | The installed tutorial passed. A separate interactive pass used the documented 60-second timeout, one platform attempt and zero Kubernetes backoff with a 63-character name. Empty-name review sent no write; cluster checking saved no CR. Confirmed save, readiness, trigger, one successful Attempt, exact stdout, sealed archive, no recorded gaps and the 15-day protection timestamp passed. Screenshots and viewport checks covered review and output. |
| Candidate artifacts | All seven component images built for Linux arm64. A final build produced all 11 Linux amd64 ELF binaries with Go 1.27.4 after the runtime corrections. This does not establish Linux amd64 image execution. |
| Workflow checks | Actionlint passed all changed workflows. Focused workflow tests and independent review covered mandatory documentation, immutable source selection, component tag overrides and the guarded fixture authorization step. The language hook passed against the 42 intended paths without staging. |
| Repeat and recovery recheck | The final normal Helm upgrade reached revision 5 / deployed without force flags. The 28-row migration ledger, retained identities, original API key and personal-account login remained valid. The isolated old database still matched its six migrations, 21 table summaries and 14 sequences. |

The upgrade used the actual five published Linux arm64 images for `v0.2.1`,
with source commit `d12cca01a2e16eba808043dc98358f35ace4709e`. All five published
manifest byte hashes, config descriptors and rootfs diffIDs matched the node
content. The Admin CRI record retained a synthetic alias from the initial image
transfer; the published manifest and exact config/rootfs chain establish the
payload identity rather than that display alias.

The old Operator produced real Job, Function and Workflow history. Maintenance
recorded CR identities, original specifications, Kubernetes version preconditions
and acknowledged suspension writes. JobRun status initially lagged terminal
ledger, Attempt and physical-workload observations; the old Operator's normal
resynchronization reported the terminal CR phases. No status was patched to
complete the drain. Nonterminal runs, active physical workloads and outstanding
termination-evidence obligations were checked before and after stopping the old
writers.

The original Helm ConfigMap migration input channel was preserved. All 28
migrations completed, with the original six ledger checksums and ConfigMap bytes
unchanged. The original API key authenticated and executed the retained Job,
Function and Workflow successfully after upgrade. The old Workflow remained
suspended for automatic scheduling while its manual run succeeded. Historical
run, revision, actor and Attempt identities were checked separately. New version
1 Job and Workflow execution and personal-account setup, logout and login passed.

A new version 1 Workflow referencing the retained version 0 Job stayed
`Pending` with `workflow_reference_not_ready`, before ledger or step creation.
The Operator confirmed cancellation after a UID/version-guarded stop request;
no child run was created. This compatibility observation is separate from the
successful new version 1 path. The upgrade guide now states the resolved-policy
and immutable-image requirements for those references.

Before the schema cutover, the old database backup was restored in an isolated
namespace. All 21 public-table content summaries, 14 sequences and six migration
rows matched the backup boundary. The actual old Admin read the retained
history using the original API key. No restored Operator or Scheduler ran.
The old release has no archive or collector; this establishes restoration of
that backup, not coordinated recovery of new 0.3.0 archive and collector state.

The temporary upgrade values initially requested an unsupported 1-GiB archive,
which the application rejected after migrations. The new archive directory and
archive catalog were verified empty before correcting only that new volume and
its template to the supported 10-GiB/default-budget profile. Database, history,
Secrets and collector state were preserved. A targeted field-ownership handoff
after the temporary rebuild preserved the StatefulSet/PVC identities and
defaulted specification. Normal Helm convergence then passed without force
flags; failed preparation evidence was retained.

Before retirement, all current Deployments, archive and collector were ready.
Nonterminal Job and Workflow runs, active workload Pods and evidence obligations
were zero before and after stopping writers. The collector stopped before the
archive. Fresh-install fixture credentials were revoked. Both disposable kind
environments, including the recovery namespace, were removed. Private backups were retained;
the default kubeconfig and existing shared services were preserved.

These are local source, browser, Linux arm64 installation and published-release
upgrade checks. The old installation fixture contained no Check definitions,
so its Check pause/resume procedure was checked against source but not exercised
through an installed upgrade. Current Check execution and monitoring are covered
by the complete fresh installed suite. Hosted workflows, registry publication,
production load and HA qualification were not performed. No staging, commit,
push or release was part of this pass.

## Toolchain and documentation release gates (2026-11-17)

The local `dev` working tree after `4184f981dbf6` raises the main Go module's
minimum version and Docker builder to Go 1.27.4. This resolves the standard-library
finding [GO-2026-6859](https://pkg.go.dev/vuln/GO-2026-6859) detected during the
release audit. Matching English and Chinese prerequisites were reviewed before
their input hashes were recorded again.

Release and prerelease verification now require a shared, read-only documentation
workflow at the selected full source SHA. The standalone Documentation workflow
uses the same checks and retains ownership of Pages publication. Regression
checks reject mutable or mismatched refs and preserve `.nojekyll` through its
artifact transfer. Independent cross-review exercised failures against the old
release graph and against an artifact upload that omitted hidden files.

The root static-host 404 artifact now contains English and Chinese recovery
links without JavaScript. With JavaScript, the original request pathname selects
the document language, title and recovery order. Recovery targets do not reuse
the unknown path, query or fragment. Same-site absolute links are now checked
alongside relative links, including target files and HTML anchors.

| Check | Result and scope |
|---|---|
| Backend | `make check`, the 51-package coverage gate and Helm checks passed with Go 1.27.4; two declaration-only coverage exceptions remain documented. |
| Vulnerabilities | Govulncheck 1.8.0 passed for `linux/amd64` and `linux/arm64` using the module-selected toolchain, without a version override. Three module-level advisories remain outside imported vulnerable packages and reachable calls. |
| PostgreSQL | All eight integration packages passed serially without cached results on isolated PostgreSQL 17, using an explicit twenty-minute package bound. |
| Documentation | All 18 script tests and 37 browser cases passed at both `/orbitjob` and `/review`. Each build checked 577 current pages, 34 reviewed topic pairs and 106 operations; both historical benchmark files retained their original bytes. The default-base artifact was restored afterwards. |
| Console | Generated inputs, TypeScript, lint, 511 unit cases, formatting and production build passed. Console browser inputs were unchanged, so the preceding 400-case result was not repeated. |
| Images | All seven component targets built locally for Linux arm64 with the pinned builder. Their binaries report Go 1.27.4; the Admin image passed the Console asset check. This does not establish Linux amd64 image or installed execution acceptance. |
| Workflow checks | Actionlint and the workflow regressions passed. Cross-review corrected explicit Bash 3.2 rejection of invalid source refs and verified hidden-file retention during Pages artifact transfer. |

The first database run was rejected because the temporary database name lacked
the required test marker. The verification setup was corrected and the complete
suite passed on a fresh test database; the name guard was preserved. Test-owned
database and browser services were removed. No cluster installation, upgrade,
hosted workflow execution, push or release was performed in this pass.

## Audit corrections (2026-11-17)

The 18 confirmed origin/dev-to-local audit findings and the Workflow reference
ambiguity were corrected on the uncommitted `dev` tree. Verification covered
grant containment and tenant selection, historical invocation replay,
cancellation convergence, Console exports and account state, publication
components, webhook scope, maintenance recovery and fixture retirement.
The English and Chinese guides now distinguish volatile drafts from accepted
operation receipts and definition projection from referenced-job readiness.

| Check | Result and scope |
|---|---|
| Backend | `make check` passed: lint, vet, race tests, OpenAPI freshness and module consistency. |
| Coverage | `make test-cover-check` passed all 51 business-tier packages, including two documented declaration-only exceptions. |
| PostgreSQL | `GOFLAGS=-timeout=20m make integration` passed all eight packages serially without cached results on disposable PostgreSQL 17. The new member regression checks actual saved policies, boundary, audit data and effective grants. The fixture regression uses the production key repository and preserves unrelated credentials. |
| Console | A fresh `npm ci`, generation, `npm run check` and formatting passed on Node 26.12.1. All 50 unit files / 511 cases and all 400 browser cases passed; the complete browser run used two workers. |
| Documentation | The rebuilt site passed checks for 577 current pages, 34 reviewed topic pairs and 106 operations. All 33 site browser cases passed, including localized navigation, search and language switching. |
| Installation | `make kind-verify` passed on a disposable single-node ARM64 kind cluster running Kubernetes 1.37, with a matching task-local kubectl. It checked migration/role/bootstrap behavior, repeated upgrades, failed-migration rollback, uninstall and reinstall. |
| Installed Console | The selected `monitoring.spec.ts`, `roadmap.spec.ts` and `product.spec.ts` suites passed all 13 cases with no skips or retries. They exercised zero-retry Check creation, independent SLO budget observations, authorization and real workload forms. |

The live webhook outage check stopped the admission Deployment, confirmed that
a definition server dry-run remained allowed outside the mapped namespace and
was rejected inside it, then restored both admission replicas. Fail-closed
behavior remains in the installation's managed scope.

Fixture cleanup was also exercised against the installed API and database. An
active Function was placed behind 100 newer paused Functions and confirmed to
appear only on the second page. Cleanup paused it. A newly issued API key was
deliberately omitted from local key journals; cleanup found it through the
transactional audit relationship, revoked it and left its identity request
returning 401. The credential manifest was retired and no owned fixture account
remained active. Historical execution and audit evidence were retained.

The first concurrent integration attempt reached Go's default ten-minute
package timeout while core-store tests were still progressing. The complete
rerun used an explicit twenty-minute package bound and passed; core store took
248 seconds. Browser investigation corrected an obsolete management-guidance
expectation and synchronized a new login recovery test with its stable
post-login page. The final 400-case run passed without changing functional
assertions or their timeouts. Unicode fixture literals remain equivalent and
the existing language hook passed against the complete candidate path list.

These installed results cover the three selected suites, not the complete
installed Console suite. Registry publication and production load or HA
qualification were outside this run.

## Console dependency maintenance (2026-11-17)

A fresh installation of the updated Console lockfile and a full `npm audit`
reported zero findings, down from ten moderate dependency findings. ESLint
10.15.0 adopts minimatch 11; the parser's remaining minimatch dependency is
scoped to an override under `@typescript-eslint/typescript-estree`. The lockfile
uses minimatch 11.0.8 and fast-uri 3.1.9, resolving the
[minimatch advisory](https://github.com/advisories/GHSA-g3hv-75wg-m249) and the
[fast-uri advisories](https://github.com/fastify/fast-uri/security/advisories).
Six changed lockfile package entries belong to development tooling. Direct
browser-runtime dependency versions remain unchanged. Reassess the scoped
override when upgrading typescript-eslint.

The first browser pass completed with 395 passing cases and one failure: an
archive poll could clear a copy-refusal message. A controlled polling response
reproduced that failure before the fix. Read/reconnect progress now has separate
state from copy, download and cleanup results. The regression waits for a new
log record before checking that copy feedback remains, and the reconnect case
checks that recovery still clears the waiting message.

A later pass exposed an HTTP fixture teardown error from an unfinished asset
forwarding request. That fixture now supplies its OpenAPI capability response
and waits for active route handlers before page teardown. UUID generation,
accepted receipts, insecure-origin behavior and copy assertions remain checked.

| Check | Observed result and scope |
| --- | --- |
| Environment | Node.js 26.12.1 and npm 11.20.0 on macOS |
| Install and audit | Fresh `npm ci` passed; full `npm audit` reported zero findings; dependency resolution had no invalid or conflicting peer entries |
| Parser compatibility | The actual TypeScript parser project-service path accepted a matching default-project file and rejected a nonmatching file with the patched minimatch |
| Console checks | Generated references, TypeScript, lint, 500 unit cases, production build and formatting passed after the log-feedback fix |
| Focused browser regression | Copy-feedback retention and reconnect recovery passed three repetitions each, six cases in total; the copy regression failed on the previous implementation |
| HTTP fixture isolation | All six HTTP-origin cases passed three repetitions each, eighteen cases in total, with explicit metadata and awaited route cleanup |
| Full browser regression | All 396 mocked-HTTP cases passed after the product fix and HTTP fixture cleanup |
| Documentation | All 34 translation pairs and review hashes passed; ten site script cases and the production artifact check passed for 577 current pages and the unchanged historical benchmark archive |

The Node engine declaration now matches the existing ESLint/Vitest requirements:
`^22.13.0 || ^24.0.0 || >=26.0.0`. English and Chinese installation and local
Console-development prerequisites match that range. This is declared dependency
compatibility; the execution checks above used Node.js 26.12.1. No installed
cluster, upgrade or recovery qualification was repeated for this maintenance.

## Simplified Chinese documentation maintenance (2026-11-16)

All 34 published English topics now have complete reviewed Chinese counterparts.
The topic review covered permissions, conditions, failure handling, numbers,
commands, examples and actual Console labels. Separate cross-reviews corrected
authorization conditions, timeout enforcement, cancellation receipts, account
capabilities, load profiles and recovery evidence. All 417 current OpenAPI prose
entries were reviewed, including a separate review of the 35 distinct strings
added or changed during the accuracy corrections. Function creation replay,
Check/SLO query authorization and SLI criteria required semantic corrections to
earlier translations.

The accuracy follow-up corrected account request constraints and per-operation
authority at the owning HTTP metadata, the archive page-size default, Workflow
creation guidance, cancellation evidence and Function wait semantics. The local
checkout now precedes repository commands. Small database claims explicitly
require a separate migration plan; no supported in-place storage procedure is
claimed. Workflow failure-policy and repeated-side-effect explanations now state
the actual behavior. A further full Chinese reading corrected 15 groups of
ambiguous or literal translations and aligned the affected English explanations.

The maintained Chinese sources are under `site/i18n/zh-CN/topics/` and
`site/i18n/zh-CN/api/current.json`. Earlier translations and tests are retained
under `site/i18n/zh-CN/legacy/` and do not participate in the build. Reviewed input
hashes cover each topic pair and the original OpenAPI/API dictionary. A changed
source or translation fails the build until it is reviewed and recorded again;
recording hashes does not perform that review.

| Check | Observed result and scope |
| --- | --- |
| Production artifact | 577 current HTML pages, including the shared English 404; 34 topics, 106 operations and 132 HTTP schemas per language, four CRD references and Helm values |
| Literal equivalence | All 34 topic pairs matched heading structure, 116 fenced blocks, 1,132 inline code literals, link targets and explicit anchors; corresponding generated pages retained the same heading IDs |
| HTTP contract | OpenAPI freshness passed; localized changes were restricted to display prose and tag display labels, retaining request examples, fields, enums, defaults, paths, methods, operation IDs and authentication data |
| Command syntax | 208 topic Bash/Shell blocks and 214 generated cURL blocks across both languages passed Bash syntax checks; account examples retain Origin, Cookie and CSRF requirements |
| Script checks | All 10 cases passed, including changed-prose/contract review-baseline refusal and preservation of literal data and heading identities |
| Default-base browser checks | All 33 cases passed, covering English and Chinese navigation, keyboard/full-text/title search, language isolation, copyable requests, same-page query/hash switching, 320-pixel themes/navigation, read-only API viewing, failure recovery, account write labels, required request fields, archive defaults and narrow-screen Schema tables |
| Alternate base | The production build and all 33 browser cases passed with `SITE_BASE=/review` |
| Historical archive | The default production artifact included the existing `origin/gh-pages` benchmark archive; both copied files retained their original bytes, and the historical HTML remained outside current-language assertions |
| Backend regressions | `make check` and `make test-cover-check` passed, including the HTTP metadata/null-PATCH regressions and undersized-PVC preflight cases |
| Generated Console references | Fresh `npm ci`, generation, type checks, lint, 500 unit cases, production build, formatting and all 396 mocked-HTTP browser cases passed; the initial locked development-tool dependency audit reported 10 moderate findings, resolved by the [subsequent dependency maintenance](#console-dependency-maintenance-2026-11-17) |
| Dependencies | Fresh lockfile installation passed with Node.js 26.12.1 and npm 11.20.0; Astro 7.4.12 and its native Markdown processor 0.4.10 built successfully; the site dependency audit reported zero vulnerabilities |

The API language-switching test exposed a small bundled script being inlined
and blocked by the explorer's CSP. Bundled assets now remain external, and the
artifact check rejects executable inline scripts on that page. Both language
directions retain the selected operation. A separate read-only implementation
review found no additional reproducible generation or navigation defect.

Chinese prose received project-style, humanizer and language-appropriate Lieflat
checks, with technical lists and literal material preserved. The short Chinese
maintenance note and English maintenance guide were below the detector's Han
threshold; their prose was reviewed directly. Desktop, mobile, search and API
screenshots were inspected separately from interaction assertions.

This is local documentation and browser verification. Commands were checked
against the maintained English sources and syntax; this localization pass did
not repeat installed-cluster, upgrade or recovery qualification. The installed
tutorial acceptance below retains its own environment scope. No publication or
deployment was performed.

## Installed Console and tutorial acceptance (2026-11-16)

A fresh dedicated `orbitjob-console-e2e` kind installation used source-built
Linux arm64 images tagged `installed-fixes-20261116` and migration 0028. Its
installation lifecycle passed fresh install, repeated upgrade, expected migration
failure, uninstall and reinstall. The kubeconfig and fixture credentials were
kept separate from working installations.

The final candidate refreshes the Admin and Operator images as
`installed-fixes-r2-20261116`. The archive and other components retain their
source-built images. Runtime receipts record the actual image IDs for each run.

The archive PVC's actual btrfs filesystem reported zero total/free inodes while
retaining more than 60 GiB of available bytes. The old unconditional free-inode
guard rejected this unreported count. The corrected guard accepts an unavailable
inode count while retaining finite-inode, available-byte, configured-allocation
and reserve checks. The regression failed with the previous guard and passed
with the fix; the archive package also passed its race suite.

The first-job browser tutorial passed the current Shell form, server-side check,
confirmed save, saved-version readiness, manual trigger, single successful
Attempt and output reading through both the archive API and Console panel. Its
63-character name in the `orbitjob` namespace exceeds the former combined
64-character audit limit. A later read verified `state=sealed`,
`outcome=Succeeded`, no recorded gaps, the expected stdout and a 15-day protection
deadline. Sealing retains the window; it does not independently certify source
coverage beyond the observed output.

The long-identity repository regression reproduced PostgreSQL error 22001 before
migration 0028. The migration expands the audit resource column without
truncation. PostgreSQL integration verified preserved rows in the default and an
existing monthly partition, full 317-character Kubernetes identities and complete
checked/dispatched/accepted operation audit records. All seven required
integration packages, `make check` and the coverage gate passed.

The complete installation exercise also exposed a Check scheduling stall:
`next_run_at` preceded the first revision activation, and admission correctly
rejected that slot on every tick. The scheduler now skips only this unaccepted
stale slot and advances by the existing interval/cron rule. Conditional cursor
writes match the version, old cursor and active state. Real PostgreSQL and race
regressions cover initial/resumed versions, other admission refusals, cursor
failures, replay without duplicate ledger/audit rows and concurrent changes.
The full Go checks, coverage gate and seven PostgreSQL packages passed again.

Console API-key rename and revocation now send the selected management
workspace. Client and dialog regressions check an explicit management target
separately from the workload connection, preserving Cookie, CSRF and server
authorization. Installed tests continue to require successful revocation and
subsequent rejection of the revoked key.

The installed tests now replay an intent through its original human session and
retain the cross-actor conflict check. Current controls, history disclosures and
unknown-save recovery replace obsolete UI assertions while preserving real
requests, identity checks and single-write requirements. Positive typed-definition
fixtures declare version 1 and retry safety; negative legacy, unsafe-retry,
digest and checked-intent conflict cases retain their distinct validation rules.

| Final check | Observed result and scope |
| --- | --- |
| Complete installed Console suite | All 10 spec files and 40 tests passed on the same candidate; no skips, expected failures or retries; `completeSuite=true`, `passed=true` |
| Input consistency | The harness source manifest matched before and after the complete run and at artifact collection; runtime image IDs are retained in its receipt |
| Installed first-job tutorial | Checked and saved a 63-character name, observed one successful Attempt, then verified exact stdout, sealed archive, no recorded gaps and approximately 15 days of protection through the API and actual Console |
| Monitoring | Real success/failure Check executions produced SLI events, healthy/exhausted/zero-allowance budgets and alerts; pause, resume, stale versions and independent grants passed |
| Management and recovery | Actual key revocation rejected the revoked key; target workspace context, typed CAS, exact int64 values, lost-response reconciliation and single-write behavior passed |
| Frontend checks | Fresh `npm ci`, generated inputs, TypeScript, lint, format, build, 500 unit tests and all 396 browser-fixture cases passed; the final browser run used two workers |
| Backend checks | `make check`, the 51-package coverage gate and all seven PostgreSQL integration packages passed after the Check scheduling correction |
| Documentation | Production generation and checks passed for 288 English pages, one preserved historical page, 34 complete topics and 106 HTTP operations |

The final documentation browser suite also passed all 16 cases. During retirement,
the resource-level helper refused an execution-label/recorded-ownership mismatch.
That guard was preserved. The separate owner helper revoked fixture credentials
and descendants, then the identity-verified disposable kind cluster and dedicated
PostgreSQL test container were removed. Verification receipts and tutorial output
were retained outside those environments.

This is local Linux arm64 kind acceptance with loopback Console access and the
recorded configuration. It does not establish a production rollout, an upgrade
from a published older release or independent certification of all log-source
coverage. The earlier failures below describe the initial documentation exercise.

## English documentation maintenance: initial exercise (2026-11-16)

This record describes the initial exercise before the installed tutorial
recovery recorded above.

This maintenance reviewed all 21 existing manually maintained English reader
sources, then organized 42 maintained reader documents into repository entry
points, product topics, contributor procedures and project records. The site
publishes 34 complete topics. Historical ADRs, plans, published changelog entries
and prior verification observations retain their original scope. Chinese sources
remain outside the new English publication until localization is reviewed.

Product and operating explanations were checked against handlers, domain and
Operator logic, command parsers, migrations, chart templates and the Console.
Independent reviews corrected Workflow suspension, WorkflowRun `Rejected`, typed
v1 creation, rate-limit groups, time-zone validation, namespace ownership and
load-fixture Pod patch authority. Generated assistant examples now declare v1;
the package includes the task, client and resource references with source hashes.

| Check | Observed result and scope |
| --- | --- |
| English production artifact | 288 pages, 34 source topics, 106 HTTP operations, complete operation/schema pages, four CRD schemas and Helm values; local and repository targets checked |
| Alternate deployment base | Production build and all 16 documentation browser cases passed with `SITE_BASE=/review` |
| Benchmark archive inclusion | A production build using the existing `origin/gh-pages` archive passed; archived HTML retains its original bytes and is excluded from the current English-page language assertion |
| Maintained-source checks | 42 English reader files: local file/anchor checks and 119 Bash/Shell fences passed syntax checks |
| HTTP request templates | 107 generated curl blocks passed Bash syntax checks; account templates distinguish new and existing invited accounts and protect session writes |
| Generated contracts and assistant | OpenAPI freshness and Console generated-type/manifest/assistant checks passed; the assistant package validates CRD shapes and explicit v1 examples |
| Console source checks | All 497 unit tests passed after the assistant corpus assertions were updated; TypeScript and the changed installed-test/package-test lint and formatting checks passed |
| Targeted Go checks | CLI, HTTP contracts, workload types, execution policy and load-helper tests passed; product documentation review also exercised account, archive, definition, projection, Function, SLI/SLO and admission packages |
| Chart and load generation | Helm checks passed for 27 migrations and chart assertions; all five load profiles generated isolated fixtures. No complete load qualification was run |
| Dedicated kind installation | Source-built Linux arm64 installation passed fresh install, repeated upgrade, expected migration failure, uninstall and reinstall. This is not an old-release upgrade, external PostgreSQL or restore qualification |
| API reader journey | The documented list, trigger and observation commands completed a successful job. Same-principal/key replay returned 200 with one matching occurrence; the archive endpoint returned a readable page without a completeness claim |

The dedicated fixture setup initially failed because native definition writers
were not configured. Repeating in a fresh owned cluster with an explicit native
fixture identity completed preparation. The installed Console's first selected
file passed five cases and failed two: API-key replay of a human-session trigger
returned 409, and an assertion expected an obsolete observation-pause string.
The product authoring selection also waited for obsolete editor controls and
did not pass. These outcomes remain failures; the complete installed suite was
not accepted.

The dedicated cluster was removed after the exercise. The local documentation
preview remains independent of that installation.

A new installed documentation case follows the current Shell editor, checked
save, readiness, trigger, terminal execution and archive steps. Its initial long
fixture identity exposed an audit insertion error: `namespace/name` exceeded the
64-character audit resource column. A service-level reproduction confirmed the
constraint; a shorter identity passed that check. The implementation limitation
is addressed by [migration 0028](database-setup.md#migration-execution-and-compatibility).
The test retains a 200-response assertion rather than accepting the error.

With a shorter identity, the installed tutorial completed the current Shell
form, server check, confirmed save, readiness, manual trigger and successful
single-attempt execution. Its output assertion failed because the archive
returned `state=capacity` with no retained records. Sixteen authorized cleanup
requests for owned terminal fixture archives were accepted, and asynchronous
`deleted` state was observed; a later tutorial run still returned capacity.
The installed tutorial therefore remains failed at the output stage. This
observation does not establish a root cause for the capacity decision, a complete
archive or a failed workload. The guide states the capacity prerequisite and
links the degraded-log behavior; complete installed output acceptance remains
outstanding.

These checks establish the documented source/build boundaries and the exercised
reader paths. They do not establish production availability, HA, current-image
publication, clean-machine onboarding time, old-version upgrade, external
database installation or full archive/database recovery. English prose was
reviewed under the project style and humanizer guidance; the Chinese-oriented
lieflat detector declined these English inputs, so its statistical metrics were
not used as English quality evidence.

## Evidence levels

| Level | What the result establishes |
|---|---|
| Unit and race tests | Domain, application, adapter and concurrency behavior under the supplied test boundaries |
| PostgreSQL integration | Actual migration, transaction, RLS, role, authorization and repository behavior on the recorded database version |
| Helm and schema checks | Migration-copy consistency, rendered configuration, RBAC and generated API/CRD contracts |
| Installed scenarios | Behavior observed with the tested images, Kubernetes topology and controlled failure cases |
| Browser fixtures | Console navigation and interaction against the declared fixture or mocked HTTP profile |
| Documentation checks | Topic/reference generation, example syntax, links, anchors, search and browser navigation; localization is a separate scope |

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

The v0.3.0 candidate is undergoing final acceptance. The following results apply
to the recorded source and environment; pending gates remain release blockers.

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
| 2026-10-30 | Console generated contracts, TypeScript, lint, 196 unit tests, production build and formatting | Passed |
| 2026-10-30 | Controller-acknowledged cancellation and retained Pod evidence | Focused unit/race tests and independent implementation review passed |
| 2026-10-30 | Independent PostgreSQL 17.11 integration | All seven packages passed, with 1,312 passing test events and no failed, skipped or incomplete events |
| 2026-11-06 | Linux amd64 and arm64 vulnerability scans | No reachable vulnerabilities; three findings in required modules were outside the code's detected call paths |
| 2026-11-06 | Six Linux arm64 candidate images and a three-node Kubernetes v1.34.3 kind installation | Passed fresh installation, repeated upgrade, expected migration-failure handling, uninstall and reinstall; running image identities matched the recorded builds |
| 2026-11-06 | Native execution and retained-evidence recovery | All nine lifecycle scenarios and both supplemental probes passed; original Deployment replicas, node scheduling state and controller manifest were restored |
| 2026-11-06 | Complete installed Console suite, distributed business recovery and upgrade from released v0.2.1 | Pending |
| 2026-11-15 | Personal-account service, HTTP authentication journey and account database boundaries on disposable PostgreSQL | Passed the scoped cases described below; no account-system rollout to the working kind installation |
| 2026-11-15 | Current backend lint, vet, full race suite, OpenAPI/module consistency and Helm checks | Passed; coverage passed all 51 tier packages with two documented no-statement exceptions |
| 2026-11-15 | Console generated contracts, TypeScript, lint, 232 unit tests, production build and formatting | Passed; complete browser fixtures passed 354 cases, with a later 73-case account/layout rerun on the final affected inputs |
| 2026-11-15 | Console interface refinement across account, administration, authoring, execution and monitoring pages | 235 unit tests passed; 358 browser cases completed, with seven assertion repairs and a nine-case follow-up passing; installed and public-demo builds passed |
| 2026-11-15 | Bilingual documentation and search | Ten script tests, 585 rendered pages, 106 API operations, schemas/local links and all 22 browser cases passed |

The 1,194 recorded build inputs were unchanged when rechecked on 2026-11-06.
The PostgreSQL test driver initially reported a cleanup-check failure after
the database tests passed. A separate inspection confirmed removal of the exact
disposable container and volume; the original failed driver result was retained.

Console browser fixtures passed all 297 cases on 2026-10-29. They establish
navigation and interaction against mocked HTTP responses. The installed suite
contains 39 cases and must pass separately against the candidate's running API,
Operator and database. An installed subset does not establish full acceptance.

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

A separate browser used the compiled Console with the actual account HTTP and
PostgreSQL services. It completed first-administrator setup, rejected an incorrect
password, restored a session after reload, created a seven-day remembered cookie
and signed out. The local test explicitly enabled insecure loopback mode;
production cookie attributes were checked by HTTP tests. This browser check
does not establish a production HTTPS deployment or an installed-cluster upgrade.

The final focused browser run passed 51 cases with one worker against mocked
HTTP fixtures. It covers account/session recovery, stale-response isolation,
workspace-management permission probes, and centered login, setup, invitation
and reset forms at widths of 320, 390, 1440 and 3200 pixels. TypeScript, lint,
formatting, unit tests and the installed and Pages builds passed. An earlier
broad fixture run retained failures and was stopped; the focused result does
not establish that the complete browser gate passes.

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

The Service/Ingress schema, rendered routing and installation notes passed their
focused Go tests and `make helm-check`. Console generation, TypeScript, lint,
220 unit tests, production build and formatting passed. Six browser regressions
used a non-localhost HTTP origin with native `randomUUID` and Clipboard APIs
unavailable; their API responses were fixtures.

The complete browser fixture suite executed 304 cases: 276 passed and 28 failed.
Failures include definition-fixture contract mismatches, outdated navigation
selectors, ambiguous alert locators and timeouts. Not all failures have a
before-change baseline; this result does not establish full Console acceptance.

The compiled installed Console also connected to the real upgraded API from a
non-localhost HTTP browser origin. Transport requests were forwarded to the local
Admin service without replacing API responses. A manual trigger completed with
`Succeeded`, retained output was visible, and reauthentication after reload
returned to the original Run URL. Native `randomUUID` and Clipboard APIs were
absent; the page reported no runtime errors or horizontal overflow at 1440 px.

A separate loopback reverse-proxy probe passed 21 checks across an HTTP hostname,
an HTTPS hostname and an HTTPS IP address with nondefault ports. It preserved
same-origin authentication and rejected public metrics and health paths. TLS
checks verified the probe certificate against its explicit local trust root.
This does not verify an external Ingress controller, public DNS, certificate
issuance or renewal, a fresh-machine install, or a production deployment.

## Reliability and account maintenance checks (2026-11-15)

An independent PostgreSQL 17 instance passed all seven integration packages for
the current reliability and personal-account candidate. Tests recreated isolated
schemas from migrations 0001–0027. They covered migration/schema authority,
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

The complete Console fixture run used mocked HTTP responses. Account entry and
styling changed during that run; the later 73-case account/layout rerun checked
those final inputs, which remained unchanged throughout the rerun. These browser
results do not establish the installed Console suite or an account upgrade on the
working cluster. Both documentation languages also passed generation and browser
checks after the affected guide translations and title-search repair.

A subsequent independent review reproduced a same-user session race: an old
account request could return 401 after a new session binding was installed and
clear the valid session's UI state. The account transport now renews its request
lifetime when the CSRF binding changes, discarding old success and failure
responses while preserving requests during an unchanged-binding refresh. The
focused transport suite passed 11 cases, three browser regressions passed, and
the reviewer independently passed four additional lifetime/body-parsing cases.

The broader UI workspace continued changing during that review. Two observed
TypeScript errors and a readiness-copy assertion failure were subsequently
repaired. The final frontend check passed generated-contract checks, type
checking, lint, all 235 unit tests and the production build.

The subsequent interface-refinement run completed all 358 browser-fixture cases
with one worker: 351 passed initially. Seven failures concerned outdated labels,
an unavailable retry control and hard-coded test ports. After repairing those
assertions, all seven cases and two affected Job/Workflow configuration cases
passed in a nine-case follow-up. The earlier full run also exercised the new
regressions for keeping time-range and log controls open during editing.
Generated-contract, TypeScript, lint and formatting checks passed, as did the
installed-profile and public-demo builds. Visual inspection of the compiled
profiles covered login, account, administration, overview, authoring, workflow
and monitoring pages at 1440px and 390px; browser fixtures additionally covered
320px and dark mode. These checks used sample data or mocked HTTP responses.
They do not establish installed account cutover, the complete installed Console
suite or release acceptance on the working cluster.

The Job authoring slice checked on 2026-11-16 passed all 274 frontend unit tests
and all 375 browser-fixture cases with one worker. After the complete browser
run, a correction made the draft explanation distinguish creating a Job from
continuing to edit the saved Job; both affected browser cases passed again.
Final generated-contract, TypeScript, lint and formatting checks passed, along
with installed-profile and public-demo builds. Regressions covered configuration
reuse without source identity, command and argument preservation, explicit shell
selection, actionable draft explanations and version-bound first-run controls.
A mocked HTTP journey connected confirmed save, exact saved-generation readiness,
manual-run confirmation and the receipt's correlated recorded result.

Visual inspection of the compiled installed profile covered empty and example
drafts, command modes and repair links at 1440px, plus the preview navigation at
390px and at 320px in dark mode. The checked views had no horizontal overflow or
page errors. These checks used mocked HTTP responses; they do not establish image
availability, actual execution, installed-cluster acceptance or usability with
real users.
The working cluster was not updated for this slice.

The compiled public demo also completed configuration reuse from a sample Job,
retaining the target namespace and clearing its schedule, without API requests.

A subsequent command-entry refinement passed 87 scoped unit tests and 33 related
browser-fixture cases with one worker. Coverage included combined command parsing,
quoted and empty arguments, explicit scripts, exact imported arrays, unfinished
input across navigation, same-version live reload and direct Check/Apply guards.
An empty explicit executable also blocked review without requiring a raw input
draft. Generated-contract, TypeScript, lint and formatting checks passed, along
with installed-profile and public-demo builds. A later preview-copy correction
was rebuilt and inspected in the compiled demo.

Compiled-demo inspection at 1440px and 320px in dark mode confirmed that a
two-option menu occupied 80px instead of a full viewport. The Runs phase menu
also followed its option count. Exact-token editing scrolled within a 320px
panel, and a 60-line script stayed within its editor. The checked views had no
horizontal overflow or page errors. These checks used sample data or mocked HTTP
responses; no installed-cluster execution was performed.

The subsequent guided authoring update passed 192 scoped unit tests across six
files. Its 86-case browser run passed 80 cases initially. After runtime-component
identity, repair-focus and fixture corrections, the 49 authoring/catalog cases
were covered by a 48-case pass and one affected-case follow-up. The 37 account
and session cases had passed before those Job-form corrections. The final
execution-before-schedule layout passed 14 related cases. Sample naming and
retained-definition checks completed a further 17-case run with one fixture
correction confirmed in a two-case follow-up. All browser runs used one worker.
Final generated-contract, TypeScript, lint, formatting and both Console builds
passed.

Compiled public-demo inspection at 1440px and 320px in dark mode checked aligned
starting actions, namespace selection on first entry and after sample reset,
editable runtime presets, replacement cancellation and the schedule generator.
The checked views had no horizontal overflow, page errors or API requests.
Separate local Docker smoke checks started all three preset images as UID 65532
with a read-only filesystem, no network, dropped capabilities, 128 MiB memory and
0.5 CPU. They ran sequentially and removed their containers. An initial image-pull
EOF was retained; a bounded retry completed all three checks. These results do
not establish cluster image access, installed Console acceptance or production
execution. This update added no namespace-provisioning API or Job time-zone field.
The configured kind API was unreachable during the final read-only cluster check.

The task-template update on 2026-11-16 added message, HTTP status and JSON tasks.
Nineteen related browser-fixture cases passed with one worker; after compacting
the code preview, all five template cases passed again. The final scoped unit
run passed 168 cases. An earlier complete unit run passed 491 of 495 cases; four
workload-assistant package assertions still expected the former nine-file corpus
instead of the current 22-file package. Type, lint, generated-contract and both
Console build checks passed. Formatting passed for affected frontend files;
the complete format check reported an unrelated installed documentation test.

Compiled-demo inspection covered the default entry, HTTP configuration, JSON
field selection, expanded code, cancellation and generated editable code at
1440px and 320px, including dark mode. Checked views had no horizontal overflow,
page errors or API requests. Eleven local script scenarios passed in sequential
non-root, read-only Docker containers with no external network, 128 MiB memory
and 0.5 CPU. HTTP responses came from loopback within the test container. These
checks modeled Kubernetes dollar escaping; they did not execute installed Jobs
or establish cluster acceptance. No deployment was performed.

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
independent business-recovery probes remain separate work. Compatibility tests
must identify the actual old/new images and migration input channel before they
can support an upgrade or rollback claim.
