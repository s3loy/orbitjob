# Documentation maintenance

The static site publishes English and Simplified Chinese topics and HTTP, CRD
and chart references. It is independent of the installed Console and accepts no
installation credentials. English uses the configured site base. Chinese uses
`/zh-cn/` beneath that base.

## Content and navigation

Maintain complete topics in the repository's Markdown files. The topic list in
[guide-sources.mjs](../site/scripts/guide-sources.mjs) assigns each source one
page identity and section. The build reads the complete topic; it does not
extract README excerpts or split USAGE headings. README is the repository entry
and USAGE is the task index. The site has a separate homepage for reader paths.

Choose the content type before writing:

- A task gives the permissions, inputs, ordered actions, success condition and
  failure handling for one result.
- A tutorial joins steps into a runnable example and includes cleanup.
- A concept explains behavior, relationships and limits, with links to operations.
- A reference specifies fields, commands, constraints or error behavior.

Use relative Markdown links between maintained sources. Give each topic one H1,
use descriptive headings and keep prerequisites before the steps that need them.
The generator maps published topics to site routes and links other repository
files to the development branch. Check the destination when changing a heading.
New routes follow the topic hierarchy; unpublished legacy routes are not kept.

Product behavior must be checked against its implementation. Configuration comes
from its parser, chart, schema or manifest; API behavior comes from the owning
handler and validation; operational claims require the corresponding environment
checks. Existing prose and code comments can both be stale. Record unresolved
behavior in [known issues](known-issues.md), and distinguish current evidence
from historical observations in [verification](verification.md).

## Local checks

Use Node.js compatible with [site/package.json](../site/package.json), currently
`>=22.23.4`. From the repository root:

```bash
npm --prefix site ci
make openapi-check
npm --prefix site run build
npm --prefix site exec -- playwright install chromium
npm --prefix site run test:browser
```

The production build runs script tests, prepares the topic and API sources,
builds Astro and Pagefind, assembles the artifact and checks pages, schemas,
operation coverage, language, local links and repository targets. The browser
suite checks navigation, request examples, keyboard search, code copying,
responsive layout and API-explorer failure recovery. These checks do not execute
an installation guide or establish universal accessibility conformance.

Preview the production artifact:

```bash
npm --prefix site run preview -- --port 18432 --ignore-lock
```

Open `http://127.0.0.1:18432/orbitjob/`. The browser suite uses port 18433.
Set `SITE_BASE` consistently for build and test when verifying another base:

```bash
SITE_BASE=/preview npm --prefix site run build
SITE_BASE=/preview npm --prefix site run test:browser
```

Generated Markdown under `site/src/content/docs/generated` and
`site/src/content/docs/zh-cn/generated`, catalogs, OpenAPI JSON, search indexes
and `site/dist` are build outputs. Edit their owning inputs, then regenerate.
An existing output check does not rebuild changed inputs.

## HTTP reference and downloadable assistant

The HTTP contract is generated from route registration and models. Distributed
CRD schemas and Helm defaults have separate generated references; their fields
do not replace semantic checks or installation prerequisites. Regenerate after
changing those inputs. For HTTP changes:

```bash
make openapi-gen
make openapi-check
npm --prefix web run generate
npm --prefix site run build
```

Review descriptions, authentication, permission requirements, examples and error
responses against actual handlers. A fresh generated file only establishes
agreement with its generator. The site emits a searchable operation/schema
reference and a locally bundled API explorer. The explorer disables request
execution, credential entry and external services.

The Console's workload-assistant package also includes current documentation,
CRDs and OpenAPI. Update its generator's source list when adding a topic required
for workload authoring; regenerate with `npm --prefix web run generate` and check
with `npm --prefix web run types:check`. Keep packaged relative links and source
hashes consistent. Edit the assistant instructions only when their workflow or
supported fields change.

## Chinese maintenance

Complete and verify the English topic first. Each published topic has a full
Chinese counterpart under `site/i18n/zh-CN/topics/`, using the identity in
`guide-sources.mjs`. Check every paragraph for the same permissions, conditions,
normal and failure behavior, numbers and qualifications. Preserve literal API
fields, UI labels, commands, examples and link targets. The generator resolves
Chinese source links against the corresponding English source and sends links
to published topics to their Chinese pages. Non-topic repository links retain
their original language. Chinese headings keep the English heading identities
so cross-topic links and language changes preserve their destination.

Review HTTP descriptions in `site/i18n/zh-CN/api/current.json` against the exact
English keys. Translation changes display text only; paths, fields, schemas,
examples, enums and authentication requirements retain their contract values.
The Chinese API explorer uses a localized display copy; the download link
provides the original OpenAPI contract. Helm values and CRD literals remain
unchanged in both languages.

After completing semantic review, record the reviewed inputs:

```bash
node site/scripts/translation.mjs --record-reviewed
npm --prefix site run build
npm --prefix site run test:browser
```

The review baseline records source and translation hashes. A changed input
requires another review before updating it. Hashes and matching code blocks do
not establish semantic equivalence. Build and browser checks cover both
languages, including navigation, page identities, search and language switching.
Earlier translations under `site/i18n/zh-CN/legacy/` are historical working
materials and are excluded from publication.

## CI and publication

The shared Documentation verification workflow runs for a manual GitHub Pages
deployment or a formal release. With read-only repository permissions, it
checks out and confirms the requested full commit SHA, checks OpenAPI freshness,
builds the site and runs browser checks. Failed browser diagnostics are
retained for seven days.

Formal releases require these checks before installation verification and image
publication. The release check uses the GitHub Pages build configuration.
Only site publication builds import benchmark history from a separate branch.

### GitHub Pages

To publish to GitHub Pages, open the **Documentation** workflow in GitHub
Actions, select **Run workflow**, choose `main` and start the run. The workflow
builds and verifies the site at the selected commit using the `/orbitjob` base.
It retains the verified artifact for one day. A separate job uses Pages write
permissions to deploy the artifact.

### Cloudflare Pages

Connect `s3loy/orbitjob` using [Cloudflare Pages Git integration](https://developers.cloudflare.com/pages/configuration/git-integration/).
Configure the production and preview branch settings under **Settings → Builds
& deployments**. [Cloudflare Pages branch deployment controls](https://developers.cloudflare.com/pages/configuration/branch-build-controls/)

| Setting | Value |
|---|---|
| Production branch | `main` |
| Enable automatic production branch deployments | Enabled |
| Preview deployments | **Custom branches** with `dev` as the only included branch |
| Root directory | `site` |
| Build command | `npm ci --no-audit --no-fund && npm run build:cloudflare` |
| Build output directory | `dist` |
| Environment variable `NODE_VERSION` | `26.13.1` |
| Environment variable `SKIP_DEPENDENCY_INSTALL` | `1` |
| Production environment variable `SITE_ORIGIN` | The production Pages origin or custom domain, including `https://` and without a path |

Set `NODE_VERSION` and `SKIP_DEPENDENCY_INSTALL` in both Production and Preview.
The explicit `npm ci` installs the locked dependencies. Set `SITE_ORIGIN` in
Production only so that preview builds use Cloudflare's `CF_PAGES_URL` for
canonical links. The script sets `SITE_BASE=/`, so the artifact is served from
the domain root.

Cloudflare automatically builds and deploys the site on pushes to the
production branch and publishes a preview on pushes to `dev`. Open the `dev`
preview to verify documentation changes before merging them into `main`.
Cloudflare runs the site build and its artifact checks independently of GitHub
Actions. Before announcing a public URL, confirm the deployment result on that
host and open the published site.

### Benchmark history

Both hosts import the available `dev/bench/` archive from the `gh-pages` branch
at build time. To include the newest measurements on GitHub Pages, wait until
Benchmark succeeds before starting the manual deployment. If a Cloudflare build
fetched the archive before Benchmark completed, retry the current production
deployment after Benchmark succeeds or wait for the next build.

To include an existing archive when running `npm --prefix site run build`
locally, set `BENCHMARK_ARCHIVE` to the archive directory. The measurements
retain their original source, hardware and configuration limits. A site build
reuses recorded measurements without running benchmarks. Source changes remain
reviewable independently of publication.
