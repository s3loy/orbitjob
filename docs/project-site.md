# Documentation maintenance

The static site publishes English and Simplified Chinese topics and HTTP, CRD
and chart references. It is independent of the installed Console and accepts no
installation credentials. English is published at the site root; Chinese uses
`/zh-cn/` beneath the configured base.

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

The Documentation workflow validates `main` and `dev` changes and pull requests.
It checks OpenAPI freshness at the same source revision, builds the site and
runs browser checks before uploading the Pages artifact. Failure diagnostics
are retained for seven days. Only `main` can enter the deployment job, with
separate Pages write permissions. Workflow configuration does not establish that
a public deployment exists or passed; check its actual run before announcing a URL.

Existing benchmark archives can be included with `BENCHMARK_ARCHIVE`. Their
source, hardware and configuration limits remain attached to the measurements;
rebuilding documentation does not produce a new performance result. Source
changes remain reviewable independently of publication.
