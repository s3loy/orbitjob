import { signInAccount } from './account-support';
import { test, expect, type APIRequestContext } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import type { DefinitionInput, DefinitionResult } from '../src/api/definitions';

const privatePath = process.env.ORBITJOB_CONSOLE_CREDENTIALS!;
const credentials = JSON.parse(readFileSync(privatePath, 'utf8'));
const prefix: string = credentials.prefix;
const runPrefix = `${prefix}-${Date.now()}`;
const namespace: string = credentials.namespace;
const target = `tenant_id=${encodeURIComponent(credentials.tenantId)}`;
let prior: Record<string, unknown> = {};
try {
  const saved = JSON.parse(
    readFileSync(join(dirname(privatePath), 'roadmap-next-evidence.json'), 'utf8'),
  );
  if (saved.prefix === prefix) prior = saved;
} catch {
  /* First owned run. */
}
const names = new Set<string>((prior.definitions ?? []) as string[]);
const evidence: Record<string, unknown> = { ...prior, prefix, runPrefix, definitions: [...names] };
const jobName = `${runPrefix}-logs`;
const workflowName = `${runPrefix}-workflow`;
let jobId = '';
let runId = '';
const jobSpec = JSON.stringify({
  executionPolicyVersion: 1,
  retrySafe: false,
  schedule: '0 0 1 1 *',
  suspend: false,
  concurrencyPolicy: 'Forbid',
  misfirePolicy: 'Skip',
  timeoutSeconds: 60,
  history: { successfulRuns: 3, failedRuns: 3 },
  retryPolicy: { maxAttempts: 1 },
  jobTemplate: {
    image: 'busybox:1.37',
    command: ['sh', '-c'],
    args: [
      'printf "owned-native-ready\\n"; i=0; while [ "$i" -lt 2000 ]; do printf "owned-line-%04d-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\\n" "$i"; i=$((i+1)); done',
    ],
    backoffLimit: 0,
  },
});
function record() {
  evidence.definitions = [...names];
  writeFileSync(
    join(dirname(privatePath), 'roadmap-next-evidence.json'),
    JSON.stringify(evidence, null, 2),
  );
}
async function api(
  request: APIRequestContext,
  path: string,
  status = 200,
  data?: unknown,
  key = credentials.key,
  method = data === undefined ? 'GET' : 'PUT',
) {
  const response = await request.fetch(`/api/v1/${path}`, {
    method,
    headers: { Authorization: `Bearer ${key}` },
    ...(data === undefined ? {} : { data }),
  });
  if (response.status() !== status) {
    const payload = await response.json().catch(() => null);
    await test.info().attach('unexpected-api-response.json', {
      body: JSON.stringify({
        method,
        path: new URL(response.url()).pathname,
        expectedStatus: status,
        observedStatus: response.status(),
        error: payload?.error,
      }),
      contentType: 'application/json',
    });
  }
  expect(response.status(), `${method} ${path.split('?')[0]}`).toBe(status);
  return response;
}
const definitionPath = (kind: string, name: string) => `definitions/${kind}/${namespace}/${name}`;
async function create(request: APIRequestContext, kind: string, name: string, spec: string) {
  names.add(name);
  record();
  const path = definitionPath(kind, name);
  const input = {
    request_id: randomUUID(),
    expected_uid: '',
    expected_resource_version: '',
    spec_json: spec,
    dry_run: true,
  };
  const checked = await (await api(request, path, 200, input)).json();
  expect(checked.state).toBe('validated');
  await api(request, path, 404);
  const accepted = await (
    await api(request, path, 200, {
      ...input,
      spec_json: checked.spec_json,
      manifest_digest: checked.manifest_digest,
      dry_run: false,
    })
  ).json();
  expect(accepted.state).toBe('accepted');
  expect(accepted.uid).not.toBe('');
  const repeat = await (
    await api(request, path, 200, {
      ...input,
      spec_json: checked.spec_json,
      manifest_digest: checked.manifest_digest,
      dry_run: false,
    })
  ).json();
  expect(repeat.state).toBe('reconciled');
  expect(repeat.uid).toBe(accepted.uid);
  const wrongDigest = await api(request, path, 400, {
    ...input,
    spec_json: checked.spec_json,
    dry_run: false,
    manifest_digest: 'wrong',
  });
  expect(await wrongDigest.json()).toMatchObject({
    error: { code: 'VALIDATION_ERROR', field: 'manifest_digest' },
  });
  const alteredIntent = await api(request, path, 409, {
    ...input,
    expected_uid: accepted.uid,
    expected_resource_version: accepted.resource_version,
    spec_json: checked.spec_json,
    dry_run: false,
    manifest_digest: checked.manifest_digest,
  });
  expect(await alteredIntent.json()).toMatchObject({
    error: { code: 'CONFLICT', field: 'request_id' },
  });
  return accepted;
}

test.describe.configure({ mode: 'serial' });
test.beforeAll(() => {
  expect(credentials.context).toBe('kind-orbitjob-console-e2e');
  expect(credentials.prepared).toBe(true);
  expect(credentials.retired).not.toBe(true);
  expect(prefix).toMatch(/^console-installed-\d+$/);
  const context = execFileSync('kubectl', ['config', 'current-context'], {
    encoding: 'utf8',
  }).trim();
  expect(context).toBe(credentials.context);
  const deployment = JSON.parse(
    execFileSync(
      'kubectl',
      ['get', 'deployment', 'orbitjob-admin-api', '-n', namespace, '-o', 'json'],
      { encoding: 'utf8' },
    ),
  );
  const selector = Object.entries(deployment.spec.selector.matchLabels)
    .map(([key, value]) => `${key}=${value}`)
    .join(',');
  const pods = JSON.parse(
    execFileSync('kubectl', ['get', 'pods', '-n', namespace, '-l', selector, '-o', 'json'], {
      encoding: 'utf8',
    }),
  );
  expect(pods.items.length).toBeGreaterThan(0);
  evidence.images = pods.items.flatMap(
    (p: { status: { containerStatuses?: { imageID: string }[] } }) =>
      p.status.containerStatuses?.map((c) => c.imageID) ?? [],
  );
});
test.afterEach(record);

test('native platform exact-target grants, self boundary and invalid boundary fail closed', async ({
  request,
}) => {
  const identity = await (
    await api(request, 'identity', 200, undefined, credentials.platformKey)
  ).json();
  expect(identity.kind).toBe('platform');
  expect(identity.capabilities).toEqual([]);
  for (const forbidden of ['key', 'key_hash', 'key_prefix', 'document', 'policies'])
    expect(identity).not.toHaveProperty(forbidden);
  await api(request, 'jobs', 403, undefined, credentials.platformKey);
  await api(request, `jobs?${target}`, 200, undefined, credentials.platformKey);
  await api(request, `jobs?${target}`, 403, undefined, credentials.unboundPlatformKey);
  await api(request, `jobs?${target}`, 403, undefined, credentials.selfPlatformKey);
  await api(request, 'identity', 401, undefined, credentials.expiredPlatformKey);
  await api(request, 'identity', 401, undefined, credentials.revokedPlatformKey);
  const invalidBoundary = await api(
    request,
    `jobs?${target}`,
    503,
    undefined,
    credentials.invalidBoundaryKey,
  );
  expect(await invalidBoundary.json()).toEqual({
    error: {
      code: 'SERVICE_UNAVAILABLE',
      message: 'permissions are temporarily unavailable',
    },
  });
  await api(request, 'jobs?tenant_id=00000000000000000000000099', 403);
  evidence.platform = {
    discovery: 200,
    missingTarget: 403,
    actualGrant: 200,
    unbound: 403,
    selfBoundary: 403,
    expired: 401,
    invalidBoundary: 503,
  };
});

test('native definition grants, namespace isolation and admission checks', async ({ request }) => {
  const path = definitionPath('jobs', jobName);
  const input = {
    request_id: randomUUID(),
    expected_uid: '',
    expected_resource_version: '',
    spec_json: jobSpec,
    dry_run: true,
  };
  await api(request, path, 403, input, credentials.readKey);
  await api(request, path, 403, input, credentials.groupKey);
  await api(request, path, 403, undefined, credentials.groupKey);
  await api(request, `definitions/jobs/unmapped/${jobName}`, 403, input);
  await api(request, path, 400, { ...input, metadata: { namespace: 'other' } });
  await api(request, path, 400, {
    ...input,
    spec_json: '{"schedule":"* * * * *","metadata":{"name":"other"}}',
  });
  await api(request, path, 400, { ...input, spec_json: 'x'.repeat(270000) });
  const legacy = await api(request, path, 400, {
    ...input,
    spec_json: JSON.stringify({ ...JSON.parse(jobSpec), executionPolicyVersion: 0 }),
  });
  expect(await legacy.json()).toMatchObject({
    error: { code: 'VALIDATION_ERROR', field: 'executionPolicyVersion' },
  });
  const unsafeRetry = await api(request, path, 400, {
    ...input,
    spec_json: JSON.stringify({ ...JSON.parse(jobSpec), retryPolicy: { maxAttempts: 2 } }),
  });
  expect(await unsafeRetry.json()).toMatchObject({
    error: {
      code: 'VALIDATION_ERROR',
      field: 'executionPolicyVersion',
      message: 'retrySafe is required before enabling repeated execution',
    },
  });
  evidence.definitionDenials = {
    ungrantedApply: 403,
    unmapped: 403,
    metadata: 400,
    oversized: 400,
    legacyCreate: 400,
    unsafeRetry: 400,
  };
});

test('native owned typed definition create, CAS, reconciliation and exact int64 workflow spec', async ({
  request,
}) => {
  const accepted = await create(
    request,
    'jobs',
    jobName,
    jobSpec.replace('"suspend":false', '"suspend":true'),
  );
  const loaded = await (await api(request, definitionPath('jobs', jobName))).json();
  const savedJob = JSON.parse(loaded.spec_json);
  expect(savedJob.jobTemplate.args).toEqual(JSON.parse(jobSpec).jobTemplate.args);
  expect(savedJob.executionPolicyVersion).toBe(1);
  expect(savedJob.jobTemplate.image).toMatch(/@sha256:[0-9a-f]{64}$/);
  expect(savedJob.jobTemplate.resources).toEqual({
    requests: { cpu: '100m', memory: '128Mi', 'ephemeral-storage': '100Mi' },
    limits: { cpu: '1', memory: '256Mi', 'ephemeral-storage': '1Gi' },
  });
  await api(request, definitionPath('jobs', jobName), 409, {
    request_id: randomUUID(),
    expected_uid: accepted.uid,
    expected_resource_version: 'stale',
    spec_json: loaded.spec_json,
    dry_run: true,
  });
  const workflowSpec = `{"executionPolicyVersion":1,"schedule":"0 0 1 1 *","suspend":true,"tasks":[{"name":"first","jobRef":{"name":"${jobName}"}},{"name":"second","jobRef":{"name":"${jobName}"},"dependsOn":["first"],"condition":{"type":"all","rules":[{"task":"first","field":"duration_ms","operator":"lt","number":9223372036854775807}]}}]}`;
  const workflow = await create(request, 'workflows', workflowName, workflowSpec);
  expect(workflow.spec_json).toContain('9223372036854775807');
  expect(workflow.spec_json).not.toContain('9223372036854776000');
  evidence.definitionsAccepted = {
    jobUID: accepted.uid,
    workflowUID: workflow.uid,
    exactInt64: true,
    dryRunAbsent: true,
    repeatReconciled: true,
    wrongDigest: 400,
    alteredIntent: 409,
    staleCAS: 409,
  };
});

test('native new owned execution exposes only bounded retained task logs with separate permission', async ({
  request,
}) => {
  test.setTimeout(240_000);
  const loaded = await (await api(request, definitionPath('jobs', jobName))).json();
  const update = {
    request_id: randomUUID(),
    expected_uid: loaded.uid,
    expected_resource_version: loaded.resource_version,
    spec_json: jobSpec,
    dry_run: true,
  };
  const checked = await (await api(request, definitionPath('jobs', jobName), 200, update)).json();
  await api(request, definitionPath('jobs', jobName), 200, {
    ...update,
    dry_run: false,
    spec_json: checked.spec_json,
    manifest_digest: checked.manifest_digest,
  });
  await expect
    .poll(
      async () => {
        const list = await (
          await api(request, `jobs?search=${encodeURIComponent(jobName)}`)
        ).json();
        const found = list.items.find((j: { name: string }) => j.name === jobName);
        if (found) jobId = String(found.id);
        return Boolean(found && !found.suspend);
      },
      { timeout: 90_000 },
    )
    .toBe(true);
  const receipt = await (
    await api(request, `jobs/${jobId}/trigger`, 201, {}, credentials.key, 'POST')
  ).json();
  await expect
    .poll(
      async () => {
        const list = await (
          await api(
            request,
            `instances?source_uid=${encodeURIComponent((await (await api(request, `jobs/${jobId}`)).json()).source_uid)}&occurrence_key=${receipt.occurrence_key}`,
          )
        ).json();
        if (list.items[0]) runId = String(list.items[0].id);
        return Boolean(runId);
      },
      { timeout: 90_000 },
    )
    .toBe(true);
  const path = `instances/${runId}/attempts/1/logs`;
  await api(request, path, 403, undefined, credentials.readKey);
  await api(request, path, 403, undefined, credentials.groupKey);
  await api(request, `${path}?pod=untrusted`, 400);
  await expect
    .poll(
      async () =>
        (await (await api(request, `${path}?limit_bytes=128&tail_lines=500`)).json()).state,
      { timeout: 90_000 },
    )
    .toBe('ready');
  const response = await api(request, `${path}?limit_bytes=128&tail_lines=500`);
  const snapshot = await response.json();
  expect(response.headers()['cache-control']).toBe('no-store');
  expect(snapshot.container).toBe('task');
  expect(snapshot.attempt).toBe(1);
  expect(snapshot.truncated).toBe(true);
  expect(Buffer.byteLength(snapshot.text, 'utf8')).toBeLessThanOrEqual(128);
  expect(snapshot.text).toContain('owned-');
  evidence.logs = {
    jobId,
    runId,
    independentDenied: 403,
    ready: true,
    truncated: true,
    bytes: Buffer.byteLength(snapshot.text),
    container: snapshot.container,
    noStore: true,
  };
});

async function triggerOwned(request: APIRequestContext, name: string) {
  let id = '';
  await expect
    .poll(
      async () => {
        const list = await (await api(request, `jobs?search=${encodeURIComponent(name)}`)).json();
        const found = list.items.find((item: { name: string }) => item.name === name);
        if (found) id = String(found.id);
        return Boolean(found);
      },
      { timeout: 90_000 },
    )
    .toBe(true);
  const receipt = await (
    await api(request, `jobs/${id}/trigger`, 201, {}, credentials.key, 'POST')
  ).json();
  let run = '';
  await expect
    .poll(
      async () => {
        const parent = await (await api(request, `jobs/${id}`)).json();
        const list = await (
          await api(
            request,
            `instances?source_uid=${parent.source_uid}&occurrence_key=${receipt.occurrence_key}`,
          )
        ).json();
        if (list.items[0]) run = String(list.items[0].id);
        return Boolean(run);
      },
      { timeout: 90_000 },
    )
    .toBe(true);
  return run;
}

test('native fresh JobRun controller ownership preserves retry and cancellation', async ({
  request,
}) => {
  test.setTimeout(300_000);
  const retryName = `${runPrefix}-retry`;
  const retry = JSON.parse(jobSpec);
  retry.retryPolicy.maxAttempts = 2;
  retry.retrySafe = true;
  retry.jobTemplate.args = ['printf "owned-retry-failure\\n"; exit 1'];
  await create(request, 'jobs', retryName, JSON.stringify(retry));
  const retryRun = await triggerOwned(request, retryName);
  await expect
    .poll(async () => (await (await api(request, `instances/${retryRun}`)).json()).phase, {
      timeout: 150_000,
    })
    .toBe('Failed');
  const retried = await (await api(request, `instances/${retryRun}`)).json();
  expect(retried.attempts.map((a: { attempt_number: number }) => a.attempt_number).sort()).toEqual([
    1, 2,
  ]);
  for (const attempt of [1, 2]) {
    const logs = await (
      await api(request, `instances/${retryRun}/attempts/${attempt}/logs`)
    ).json();
    expect(logs.state).toBe('ready');
    expect(logs.text).toContain('owned-retry-failure');
  }
  const cancelName = `${runPrefix}-cancel`;
  const long = JSON.parse(jobSpec);
  long.timeoutSeconds = 180;
  long.jobTemplate.args = ['printf "owned-cancel-start\\n"; sleep 120'];
  await create(request, 'jobs', cancelName, JSON.stringify(long));
  const cancelRun = await triggerOwned(request, cancelName);
  await expect
    .poll(
      async () =>
        (await (await api(request, `instances/${cancelRun}/attempts/1/logs`)).json()).state,
      { timeout: 90_000 },
    )
    .toBe('ready');
  await api(request, `instances/${cancelRun}/cancel`, 200, {}, credentials.key, 'POST');
  await expect
    .poll(async () => (await (await api(request, `instances/${cancelRun}`)).json()).phase, {
      timeout: 90_000,
    })
    .toBe('Canceled');
  evidence.ownerExecution = {
    retryRun,
    attempts: 2,
    retryTerminal: 'Failed',
    retryLogsVerified: true,
    cancelRun,
    cancelTerminal: 'Canceled',
  };
});

test('native owned workflow step cancellation preserves JobRun ownership', async ({ request }) => {
  test.setTimeout(240_000);
  const name = `${runPrefix}-cancel-workflow`;
  await create(
    request,
    'workflows',
    name,
    JSON.stringify({
      executionPolicyVersion: 1,
      schedule: '0 0 1 1 *',
      suspend: false,
      concurrencyPolicy: 'Forbid',
      failPolicy: 'FailFast',
      timeoutSeconds: 180,
      tasks: [{ name: 'long', jobRef: { name: `${runPrefix}-cancel` } }],
    }),
  );
  let id = '';
  await expect
    .poll(
      async () => {
        const list = await (
          await api(request, `workflows?search=${encodeURIComponent(name)}`)
        ).json();
        const item = list.items.find((w: { name: string }) => w.name === name);
        if (item) id = String(item.id);
        return Boolean(item);
      },
      { timeout: 90_000 },
    )
    .toBe(true);
  const receipt = await (
    await api(request, `workflows/${id}/runs`, 201, {}, credentials.key, 'POST')
  ).json();
  let workflowRun = '';
  let step = '';
  await expect
    .poll(
      async () => {
        const list = await (await api(request, `workflows/${id}/runs?limit=100`)).json();
        const item = list.items.find(
          (r: { occurrence_key: string }) => r.occurrence_key === receipt.occurrence_key,
        );
        if (!item) return false;
        workflowRun = String(item.id);
        const run = await (await api(request, `workflows/${id}/runs/${workflowRun}`)).json();
        if (run.steps[0]) step = String(run.steps[0].id);
        if (!step) return false;
        return (
          (await (await api(request, `instances/${step}/attempts/1/logs`)).json()).state === 'ready'
        );
      },
      { timeout: 90_000 },
    )
    .toBe(true);
  await api(
    request,
    `workflows/${id}/runs/${workflowRun}/cancel`,
    200,
    {},
    credentials.key,
    'POST',
  );
  await expect
    .poll(
      async () => (await (await api(request, `workflows/${id}/runs/${workflowRun}`)).json()).phase,
      { timeout: 90_000 },
    )
    .toBe('Canceled');
  await expect
    .poll(async () => (await (await api(request, `instances/${step}`)).json()).phase, {
      timeout: 90_000,
    })
    .toBe('Canceled');
  evidence.workflowOwner = {
    id,
    workflowRun,
    step,
    logsReady: true,
    phase: 'Canceled',
    stepPhase: 'Canceled',
  };
});

test('native Function logs keep original history UUID and exact parent scope', async ({
  request,
}) => {
  test.setTimeout(180_000);
  const functions = await (await api(request, 'functions?limit=100')).json();
  const item = functions.items.find((f: { name: string }) => f.name.endsWith('-success'));
  expect(item, 'existing success Function fixture').toBeDefined();
  const id = String(item.id);
  const receipt = await (
    await api(request, `functions/${id}/invoke`, 201, undefined, credentials.key, 'POST')
  ).json();
  const object = JSON.parse(
    execFileSync('kubectl', ['get', 'jobrun', receipt.name, '-n', namespace, '-o', 'json'], {
      encoding: 'utf8',
    }),
  );
  expect(object.spec.actor).toBe(credentials.keyId);
  evidence.jobRuns = [
    ...((evidence.jobRuns ?? []) as { name: string; uid: string }[]),
    { name: object.metadata.name, uid: object.metadata.uid },
  ];
  record();
  const digest: string = receipt.occurrence_key;
  const run = `${digest.slice(0, 8)}-${digest.slice(8, 12)}-${digest.slice(12, 16)}-${digest.slice(16, 20)}-${digest.slice(20, 32)}`;
  await expect
    .poll(
      async () => {
        const runs = await (await api(request, `functions/${id}/runs?limit=100`)).json();
        return runs.items.find((r: { run_id: string }) => r.run_id === run)?.status;
      },
      { timeout: 90_000 },
    )
    .toBe('success');
  expect(run).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/);
  const parent = await (await api(request, `functions/${id}/runs/${run}`)).json();
  expect(parent.run_id).toBe(run);
  await api(
    request,
    `functions/${id}/runs/${run}/attempts/1/logs`,
    403,
    undefined,
    credentials.readKey,
  );
  await api(request, `functions/9223372036854775807/runs/${run}/attempts/1/logs`, 403);
  const snapshot = await (await api(request, `functions/${id}/runs/${run}/attempts/1/logs`)).json();
  expect(snapshot.state).toBe('ready');
  expect(snapshot.attempt).toBe(1);
  expect(snapshot.container).toBe('task');
  evidence.functionHistory = {
    id,
    originalUUID: run,
    independentDenied: 403,
    foreignParentDenied: 403,
    actualRetainedState: snapshot.state,
    reason: snapshot.reason,
  };
});

test('native UI unknown write retains request and reconciles real accepted definition', async ({
  page,
}) => {
  const name = `${runPrefix}-ui`;
  names.add(name);
  record();
  await page.goto('/console/connection');
  await signInAccount(page, credentials.key);
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  const sourceName = [...names].filter((value) => value.endsWith('-logs')).at(-1) ?? jobName;
  await page.getByRole('link', { name: sourceName, exact: true }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await page.getByLabel('Definition namespace', { exact: true }).fill(namespace);
  await page.getByLabel('Definition name', { exact: true }).fill(name);
  await page.locator('summary').filter({ hasText: 'Advanced spec JSON' }).click();
  await page
    .getByLabel('Definition spec JSON', { exact: true })
    .fill(jobSpec.replace('"suspend":false', '"suspend":true'));
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
  let intercepted = 0;
  let accepted!: DefinitionResult;
  let acceptedInput!: DefinitionInput;
  let releaseReconciliation = () => {};
  const allowReconciliation = new Promise<void>((resolve) => {
    releaseReconciliation = resolve;
  });
  const path = `/api/v1/${definitionPath('jobs', name)}`;
  await page.route(`**/api/v1/definitions/jobs/${namespace}/${name}*`, async (route) => {
    if (route.request().method() !== 'PUT') return route.continue();
    intercepted++;
    acceptedInput = route.request().postDataJSON();
    expect(acceptedInput.dry_run).toBe(false);
    const response = await route.fetch();
    expect(response.status()).toBe(200);
    accepted = await response.json();
    expect(accepted.state).toBe('accepted');
    await route.abort('failed');
  });
  await page.route(`**${path}/operations/*`, async (route) => {
    const response = await route.fetch();
    expect(response.status()).toBe(200);
    // Hold the real receipt until the unresolved draft has survived navigation.
    await allowReconciliation;
    await route.fulfill({ response });
  });
  const reconciled = page.waitForResponse(
    (response) =>
      response.request().method() === 'GET' &&
      new URL(response.url()).pathname.startsWith(`${path}/operations/`),
  );
  try {
    await page.getByRole('button', { name: 'Save definition', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
    await expect(page.getByText('Save outcome unknown', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeDisabled();
    await page.getByRole('link', { name: 'Jobs', exact: true }).first().click();
    const activity = page.getByRole('group', { name: 'Operation activity', exact: true });
    await expect(activity).not.toHaveAttribute('open');
    await activity.locator(':scope > summary').click();
    await activity.getByRole('link', { name: 'Review draft', exact: true }).click();
    await expect(page.getByLabel('Definition name', { exact: true })).toHaveValue(name);
    releaseReconciliation();
    const receipt = await reconciled;
    expect(receipt.status()).toBe(200);
    expect(new URL(receipt.url()).pathname).toBe(`${path}/operations/${acceptedInput.request_id}`);
    expect(await receipt.json()).toMatchObject({
      request_id: acceptedInput.request_id,
      kind: 'jobs',
      namespace,
      name,
      state: 'accepted',
      result: {
        uid: accepted.uid,
        request_id: acceptedInput.request_id,
        manifest_digest: accepted.manifest_digest,
        spec_json: accepted.spec_json,
      },
    });
    await expect(
      page.getByText('This save was confirmed. The live definition may have changed since then.', {
        exact: true,
      }),
    ).toBeVisible();
    const stored = await (await api(page.request, definitionPath('jobs', name))).json();
    expect(stored).toMatchObject({
      uid: accepted.uid,
      last_request_id: acceptedInput.request_id,
      last_manifest_digest: accepted.manifest_digest,
    });
  } finally {
    releaseReconciliation();
  }
  expect(intercepted).toBe(1);
  expect(
    await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage })),
  ).not.toContain(credentials.key);
  evidence.unknownWrite = {
    actualAcceptedBeforeResponseLoss: true,
    writes: intercepted,
    navigationRetained: true,
    realGetReconciled: true,
    reconciledRequestID: acceptedInput.request_id,
    reconciledUID: accepted.uid,
    keyAbsentStorage: true,
  };
});

// Run independently after the core eight-case verdict; only fresh keys and new invocation.
test('native grouped Function logs enforce actual group and exact Deny without instance bypass', async ({
  request,
}) => {
  test.setTimeout(180_000);
  const functions = await (await api(request, 'functions?limit=100')).json();
  const grouped = functions.items.find((f: { resource_group_id?: string }) =>
    Boolean(f.resource_group_id),
  );
  const other = functions.items.find((f: { resource_group_id?: string }) => !f.resource_group_id);
  expect(grouped, 'existing declared M4 grouped Function').toBeDefined();
  expect(other).toBeDefined();
  const state = JSON.parse(readFileSync(privatePath, 'utf8'));
  function retainKey(row: { id: string; key: string }) {
    state.keys.push(row.id);
    state.extraTenantKeyIDs = [...(state.extraTenantKeyIDs ?? []), row.id];
    state.extraSecrets = [...(state.extraSecrets ?? []), row.key];
    writeFileSync(privatePath, JSON.stringify(state, null, 2), { mode: 0o600 });
  }
  const ownedGroupKey = await (
    await api(
      request,
      `tenants/${credentials.tenantId}/api_keys`,
      201,
      { policies: ['00000000000000000000000010'], resource_group_id: grouped.resource_group_id },
      credentials.seedKey,
      'POST',
    )
  ).json();
  retainKey(ownedGroupKey);
  const receipt = await (
    await api(request, `functions/${grouped.id}/invoke`, 201, undefined, ownedGroupKey.key, 'POST')
  ).json();
  const object = JSON.parse(
    execFileSync('kubectl', ['get', 'jobrun', receipt.name, '-n', namespace, '-o', 'json'], {
      encoding: 'utf8',
    }),
  );
  expect(object.spec.actor).toBe(ownedGroupKey.id);
  evidence.jobRuns = [
    ...((evidence.jobRuns ?? []) as { name: string; uid: string }[]),
    { name: object.metadata.name, uid: object.metadata.uid },
  ];
  record();
  const digest: string = receipt.occurrence_key;
  const uuid = `${digest.slice(0, 8)}-${digest.slice(8, 12)}-${digest.slice(12, 16)}-${digest.slice(16, 20)}-${digest.slice(20, 32)}`;
  await expect
    .poll(
      async () => {
        const rows = await (
          await api(
            request,
            `functions/${grouped.id}/runs?limit=100`,
            200,
            undefined,
            ownedGroupKey.key,
          )
        ).json();
        return rows.items.find((r: { run_id: string }) => r.run_id === uuid)?.status;
      },
      { timeout: 90_000 },
    )
    .toBe('success');
  const path = `functions/${grouped.id}/runs/${uuid}/attempts/1/logs`;
  const snapshot = await (await api(request, path, 200, undefined, ownedGroupKey.key)).json();
  expect(snapshot.state).toBe('ready');
  await api(
    request,
    `functions/${other.id}/runs/${uuid}/attempts/1/logs`,
    403,
    undefined,
    ownedGroupKey.key,
  );
  await api(request, 'instances', 403, undefined, ownedGroupKey.key);
  await api(
    request,
    `instances/${String((evidence.logs as { runId: string }).runId)}/attempts/1/logs`,
    403,
    undefined,
    ownedGroupKey.key,
  );
  const policy = await (
    await api(
      request,
      'policies',
      201,
      {
        name: `${runPrefix}-exact-deny`,
        document: {
          version: '1',
          statement: [
            {
              effect: 'Allow',
              action: ['function:ReadLogs'],
              resource: ['orbitjob:self:*:function/*'],
            },
            {
              effect: 'Deny',
              action: ['function:ReadLogs'],
              resource: [`orbitjob:self:${grouped.resource_group_id}:function/${grouped.id}`],
            },
          ],
        },
      },
      credentials.key,
      'POST',
    )
  ).json();
  state.policies.push(policy.id);
  writeFileSync(privatePath, JSON.stringify(state, null, 2), { mode: 0o600 });
  const deniedKey = await (
    await api(
      request,
      `tenants/${credentials.tenantId}/api_keys`,
      201,
      { policies: [policy.id] },
      credentials.key,
      'POST',
    )
  ).json();
  retainKey(deniedKey);
  await api(request, path, 403, undefined, deniedKey.key);
  evidence.groupFunction = {
    functionId: grouped.id,
    groupId: grouped.resource_group_id,
    uuid,
    ready: true,
    keyIDs: [ownedGroupKey.id, deniedKey.id],
    policyID: policy.id,
    foreignParentDenied: 403,
    instanceBypassDenied: 403,
    exactDeny: 403,
  };
});
