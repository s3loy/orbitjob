import { signInAccount } from './account-support';
import { test, expect, type APIRequestContext, type Page } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { timestampNanos } from '../src/api/query-window';
const privatePath = process.env.ORBITJOB_CONSOLE_CREDENTIALS!;
const credentials = JSON.parse(readFileSync(privatePath, 'utf8'));
const report: Record<string, unknown> = {};
const owned: {
  keys: string[];
  policies: string[];
  groups: string[];
  tenants: string[];
  bindings: { key: string; policy: string }[];
} = {
  keys: [],
  policies: [],
  groups: [],
  tenants: [],
  bindings: [],
};
const prefix = `${credentials.prefix}-roadmap-${Date.now().toString(36)}`;
function headers(key: string) {
  return { Authorization: `Bearer ${key}` };
}
async function api(
  request: APIRequestContext,
  path: string,
  key = credentials.key,
  method = 'GET',
  data?: unknown,
  status = 200,
) {
  const response = await request.fetch(`/api/v1/${path}`, {
    method,
    headers: headers(key),
    ...(data === undefined ? {} : { data }),
  });
  expect(response.status(), `${method} ${path.split('?')[0]} status`).toBe(status);
  return response;
}
function record() {
  const path = join(dirname(privatePath), 'roadmap-evidence.json');
  let existing: { runs?: Record<string, unknown> } = {};
  try {
    existing = JSON.parse(readFileSync(path, 'utf8'));
  } catch {
    /* First write. */
  }
  writeFileSync(
    path,
    JSON.stringify(
      {
        context: credentials.context,
        runs: { ...existing.runs, [prefix]: { prefix, owned, ...report } },
      },
      null,
      2,
    ),
  );
}

test.beforeAll(() => {
  expect(`${prefix}-seed-catalog`.length).toBeLessThanOrEqual(64);
  expect(credentials.context).toBe('kind-orbitjob-console-e2e');
  expect(execFileSync('kubectl', ['config', 'current-context'], { encoding: 'utf8' }).trim()).toBe(
    credentials.context,
  );
  const deployment = JSON.parse(
    execFileSync(
      'kubectl',
      ['get', 'deployment', 'orbitjob-admin-api', '-n', 'orbitjob', '-o', 'json'],
      { encoding: 'utf8' },
    ),
  );
  const selector = Object.entries(deployment.spec.selector.matchLabels)
    .map(([key, value]) => `${key}=${value}`)
    .join(',');
  const pods = JSON.parse(
    execFileSync('kubectl', ['get', 'pods', '-n', 'orbitjob', '-l', selector, '-o', 'json'], {
      encoding: 'utf8',
    }),
  );
  report.image = {
    tag: deployment.spec.template.spec.containers[0].image,
    observedImageIDs: pods.items.flatMap(
      (pod: { status: { containerStatuses?: { imageID: string }[] } }) =>
        pod.status.containerStatuses?.map((item) => item.imageID) ?? [],
    ),
  };
  report.blocked = [];
});
test.afterEach(record);
test.afterAll(async ({ request }) => {
  const cleanup: Record<string, number> = {};
  for (const id of owned.keys) {
    const r = await request.post(`/api/v1/api_keys/${id}/revoke`, {
      headers: headers(credentials.key),
      data: {},
    });
    cleanup[`key:${id}`] = r.status();
  }
  if (owned.bindings.length) {
    const statements = owned.bindings.map((binding) => {
      for (const value of [binding.key, binding.policy])
        if (!/^[0-9A-HJKMNP-TV-Z]{26}$/.test(value)) throw new Error('Invalid owned binding ID');
      return `DELETE FROM key_policies WHERE key_id='${binding.key}' AND policy_id='${binding.policy}';`;
    });
    execFileSync(
      'kubectl',
      [
        '--context',
        credentials.context,
        'exec',
        '-i',
        '-n',
        'orbitjob',
        'deploy/orbitjob-postgres',
        '--',
        'sh',
        '-c',
        'psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At',
      ],
      {
        input: 'BEGIN;' + statements.join('') + 'COMMIT;',
        encoding: 'utf8',
        stdio: ['pipe', 'pipe', 'pipe'],
      },
    );
    report.bindingCleanup = {
      ownedPairs: owned.bindings.length,
      scope: 'Exact recorded test key_id + policy_id only',
    };
  }
  for (const id of [...owned.policies].reverse()) {
    const r = await request.delete(`/api/v1/policies/${id}?tenant_id=${credentials.tenantId}`, {
      headers: headers(credentials.key),
    });
    cleanup[`policy:${id}`] = r.status();
  }
  report.cleanup = cleanup;
  record();
});

test('native platform identity enforces explicit grants', async ({ request }) => {
  const platform = await (await api(request, 'identity', credentials.platformKey)).json();
  expect(platform.kind).toBe('platform');
  expect(platform.tenant_id).toBe('');
  expect(platform.capabilities.length).toBe(0);
  expect(
    Object.keys(platform).some((name) =>
      ['key', 'key_hash', 'document', 'policies'].includes(name),
    ),
  ).toBe(false);
  await api(request, 'jobs', credentials.platformKey, 'GET', undefined, 403);
  await api(request, `jobs?tenant_id=${credentials.tenantId}`, credentials.platformKey);
  const probe = new URLSearchParams({
    action: 'job:List',
    resource: `orbitjob:${credentials.tenantId}:*:job/*`,
  });
  const target = await (
    await api(
      request,
      `identity?tenant_id=${credentials.tenantId}&${probe}`,
      credentials.platformKey,
    )
  ).json();
  expect(target.target_tenant_id).toBe(credentials.tenantId);
  expect(target.capabilities[0].allowed).toBe(true);
  report.platformIdentity = {
    platformDiscovery: 200,
    explicitTarget: 200,
    missingPlatformTarget: 403,
  };
});

test('native tenant identity preserves tenant, explicit deny, boundary, group and foreign restrictions', async ({
  request,
}) => {
  const identity = await (await api(request, 'identity')).json();
  expect(identity.kind).toBe('tenant');
  expect(identity.tenant_id).toBe(credentials.tenantId);
  await api(
    request,
    `identity?tenant_id=${credentials.foreignTenantId}`,
    credentials.key,
    'GET',
    undefined,
    403,
  );
  const probe = new URLSearchParams({
    action: 'job:List',
    resource: `orbitjob:${credentials.tenantId}:*:job/*`,
  });
  const denied = await (await api(request, `identity?${probe}`, credentials.denyKey)).json();
  expect(denied.capabilities[0].allowed).toBe(false);
  await api(request, 'jobs', credentials.denyKey, 'GET', undefined, 403);
  await api(request, 'workflows', credentials.boundaryKey, 'GET', undefined, 403);
  await api(request, 'workflows', credentials.groupKey, 'GET', undefined, 403);
  await api(
    request,
    `tenants/${credentials.tenantId}/api_keys`,
    credentials.foreignKey,
    'GET',
    undefined,
    403,
  );
  report.identity = {
    tenantDiscovery: 200,
    tenantSwitch: 403,
    explicitDeny: 403,
    boundary: 403,
    group: 403,
    foreign: 403,
  };
});

test('native platform tenant catalog lifecycle uses explicit target grants', async ({
  request,
}) => {
  const tenant = await (
    await api(
      request,
      `tenants?tenant_id=${credentials.tenantId}`,
      credentials.platformKey,
      'POST',
      { name: prefix, slug: prefix },
      201,
    )
  ).json();
  owned.tenants.push(tenant.id);
  record();
  await api(request, `tenants/${tenant.id}?tenant_id=${tenant.id}`, credentials.platformKey);
  report.platformCatalog = { tenantCreated: tenant.id, explicitTargetRead: 200 };
});

test('native existing tenant group, policy and key lifecycle enforces grants and one-time response', async ({
  request,
}) => {
  await api(request, `tenants/${credentials.tenantId}`);
  const group = await (
    await api(
      request,
      'resource_groups',
      credentials.key,
      'POST',
      { name: prefix, slug: prefix },
      201,
    )
  ).json();
  owned.groups.push(group.id);
  record();
  const body = {
    name: prefix,
    document: {
      version: '1',
      statement: [{ effect: 'Allow', action: ['job:List'], resource: ['orbitjob:self:*:job/*'] }],
    },
  };
  await api(request, 'policies', credentials.listOnlyKey, 'POST', body, 403);
  const policy = await (await api(request, 'policies', credentials.key, 'POST', body, 201)).json();
  owned.policies.push(policy.id);
  record();
  const created = await (
    await api(
      request,
      `tenants/${credentials.tenantId}/api_keys`,
      credentials.key,
      'POST',
      { policies: [policy.id] },
      201,
    )
  ).json();
  owned.keys.push(created.id);
  owned.bindings.push({ key: created.id, policy: policy.id });
  record();
  expect(typeof created.key === 'string' && created.key.length > 12).toBe(true);
  const rows = (await (await api(request, `tenants/${credentials.tenantId}/api_keys`)).json())
    .items;
  const listed = rows.find((row: { id: string }) => row.id === created.id);
  expect(!!listed).toBe(true);
  expect(Object.hasOwn(listed, 'key')).toBe(false);
  expect(Object.hasOwn(listed, 'key_hash')).toBe(false);
  await api(request, 'jobs', created.key);
  await api(request, `policies/${policy.id}`, credentials.key, 'DELETE', undefined, 409);
  await api(request, 'jobs', created.key);
  await api(
    request,
    `api_keys/${credentials.foreignKeyId}/revoke`,
    credentials.key,
    'POST',
    {},
    403,
  );
  await api(request, 'identity', credentials.foreignKey);
  await api(request, `api_keys/${created.id}/revoke`, credentials.key, 'POST', {});
  owned.keys = owned.keys.filter((id) => id !== created.id);
  await api(request, 'identity', created.key, 'GET', undefined, 401);
  await api(request, `policies/${policy.id}`, credentials.key, 'DELETE', undefined, 409);
  const unbound = await (
    await api(
      request,
      'policies',
      credentials.key,
      'POST',
      { ...body, name: prefix + '-unbound' },
      201,
    )
  ).json();
  owned.policies.push(unbound.id);
  await api(request, `policies/${unbound.id}`, credentials.key, 'DELETE', undefined, 204);
  owned.policies = owned.policies.filter((id) => id !== unbound.id);
  report.lifecycle = {
    existingTenantUsed: credentials.tenantId,
    groupCreated: group.id,
    createdKeyId: created.id,
    listNeverRevealsSecret: true,
    listOnlyCreate: 403,
    foreignRevoke: 403,
    foreignStillAuthenticates: true,
    revokedAuthentication: 401,
    unboundPolicyDeleted: true,
    activeBoundPolicyDelete: 409,
    activeChildGrantPreserved: true,
    revokedBindingPolicyDelete: 409,
  };
});

test('native server literal search, range, count and pagination use the same real ledger scope', async ({
  request,
}) => {
  const key = credentials.historyKey;
  for (const term of ['%', '_']) {
    const query = new URLSearchParams({ search: term });
    const jobs = await (await api(request, `jobs?${query}`, key)).json();
    expect(jobs.items.length).toBe(0);
    const workflows = await (await api(request, `workflows?${query}`, key)).json();
    expect(workflows.items.length).toBe(0);
  }
  const definitions = (
    await (await api(request, 'jobs?search=console&limit=2&offset=0', key)).json()
  ).items;
  expect(definitions.length).toBeGreaterThan(0);
  expect(
    definitions.every((row: { name: string; namespace: string }) =>
      (row.name + ' ' + row.namespace).toLowerCase().includes('console'),
    ),
  ).toBe(true);
  const cutoff = new Date().toISOString();
  const query = new URLSearchParams({ created_before: cutoff, limit: '2', offset: '0' });
  const first = (await (await api(request, `instances?${query}`, key)).json()).items;
  expect(first.length).toBe(2);
  query.set('offset', '2');
  const second = (await (await api(request, `instances?${query}`, key)).json()).items;
  expect(second.length).toBe(2);
  expect(new Set([...first, ...second].map((row) => String(row.id))).size).toBe(4);
  const summary = await (
    await api(request, `instances/summary?created_before=${encodeURIComponent(cutoff)}`, key)
  ).json();
  expect(BigInt(summary.total) >= 4n).toBe(true);
  expect(
    Object.values(summary.phases).reduce<bigint>(
      (sum, value) => sum + BigInt(String(value)),
      0n,
    ) === BigInt(summary.total),
  ).toBe(true);
  const baseNanos = timestampNanos(first[0].created_at)!;
  expect(baseNanos % 1000n).toBe(0n);
  const plusNanos = baseNanos + 1n;
  const plusOne = new Date(Number(plusNanos / 1000000n))
    .toISOString()
    .replace(/\.\d{3}Z$/, '.' + String(plusNanos % 1000000000n).padStart(9, '0') + 'Z');
  const exact = {
    source_uid: String(first[0].source_uid),
    occurrence_key: String(first[0].occurrence_key),
  };
  const nanosBefore = new URLSearchParams({ ...exact, created_before: plusOne });
  const nanosFrom = new URLSearchParams({ ...exact, created_from: plusOne });
  const included = (await (await api(request, `instances?${nanosBefore}`, key)).json()).items;
  const excluded = (await (await api(request, `instances?${nanosFrom}`, key)).json()).items;
  expect(included.length).toBe(1);
  expect(String(included[0].id)).toBe(String(first[0].id));
  expect(excluded.length).toBe(0);
  const beforeSummary = await (await api(request, `instances/summary?${nanosBefore}`, key)).json();
  const fromSummary = await (await api(request, `instances/summary?${nanosFrom}`, key)).json();
  expect(BigInt(beforeSummary.total)).toBe(1n);
  expect(BigInt(fromSummary.total)).toBe(0n);
  report.nanosecondBoundary = {
    runId: first[0].id,
    createdAt: first[0].created_at,
    plusOneNanosecond: plusOne,
    beforeList: 1,
    fromList: 0,
    beforeSummary: 1,
    fromSummary: 0,
    uniqueSourceOccurrence: true,
  };
  const boundary = first[0].created_at;
  const exclusive = (
    await (
      await api(request, `instances?created_before=${encodeURIComponent(boundary)}&limit=20`, key)
    ).json()
  ).items;
  expect(
    exclusive.some((row: { id: number | string }) => String(row.id) === String(first[0].id)),
  ).toBe(false);
  await api(
    request,
    `instances?created_from=${encodeURIComponent(boundary)}&created_before=${encodeURIComponent(boundary)}`,
    key,
    'GET',
    undefined,
    400,
  );
  const functions = (await (await api(request, 'functions?limit=50', key)).json()).items;
  expect(functions.length).toBeGreaterThan(0);
  let functionID: number | string | undefined;
  let originalUUID: string | undefined;
  const literal = (await (await api(request, 'functions?search=%25', key)).json()).items;
  expect(literal.length).toBe(0);
  for (const definition of functions) {
    const outcomes = (
      await (
        await api(
          request,
          `functions/${definition.id}/runs?limit=2&offset=0&created_before=${encodeURIComponent(cutoff)}`,
          key,
        )
      ).json()
    ).items;
    if (outcomes.length) {
      functionID = definition.id;
      originalUUID = outcomes[0].run_id;
      break;
    }
  }
  expect(!!originalUUID).toBe(true);
  const detail = await (
    await api(request, `functions/${functionID}/runs/${originalUUID}`, key)
  ).json();
  expect(detail.run_id).toBe(originalUUID);
  report.queries = {
    literalSearch: true,
    stablePages: first.length + second.length,
    summaryTotal: summary.total,
    phaseSumMatches: true,
    exclusiveBoundary: true,
    invalidRange: 400,
    functionOriginalUUID: true,
  };
});

async function findHistory(request: APIRequestContext) {
  const workflows = (
    await (await api(request, 'workflows?limit=100', credentials.historyKey)).json()
  ).items;
  for (const workflow of workflows) {
    const runs = (
      await (
        await api(request, `workflows/${workflow.id}/runs?limit=30`, credentials.historyKey)
      ).json()
    ).items;
    const old = runs.find(
      (run: { revision_id: number | string }) => String(run.revision_id) !== String(workflow.id),
    );
    if (old)
      return {
        workflow,
        run: await (
          await api(request, `workflows/${workflow.id}/runs/${old.id}`, credentials.historyKey)
        ).json(),
      };
  }
  throw new Error('No real old-revision Workflow history exists; no synthetic fallback was used');
}
async function open(page: Page, key: string) {
  await page.goto('/console/connection');
  await signInAccount(page, key);
}
async function section(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}
async function reviewChanges(page: Page) {
  const activity = page.getByRole('group', { name: 'Operation activity', exact: true });
  await expect(activity).toBeVisible();
  await expect(activity).not.toHaveAttribute('open', '');
  await activity.locator(':scope > summary').click();
  await expect(activity).toHaveAttribute('open', '');
  await activity.getByRole('link', { name: /View recent changes/ }).click();
}
async function createdReceipt(page: Page, kind: string, name: string, id: string) {
  const entry = page.locator('.admin-activity-entry').filter({ hasText: name });
  if ((await entry.getAttribute('open')) === null) await entry.locator(':scope > summary').click();
  const receipt = entry.getByRole('region', { name: `Created ${kind} receipt`, exact: true });
  const identity = receipt.locator('.admin-record-identity');
  if ((await identity.getAttribute('open')) === null)
    await identity.locator(':scope > summary').click();
  await expect(receipt.getByLabel(`Created ${kind} ID`, { exact: true })).toHaveText(id);
  return receipt;
}

test('native immutable Workflow history survives a different current DAG and independent grants', async ({
  page,
  request,
}) => {
  const { workflow, run } = await findHistory(request);
  expect(run.pinned_revision_available).toBe(true);
  expect(Array.isArray(run.tasks)).toBe(true);
  expect(String(run.revision_id) !== String(workflow.id)).toBe(true);
  const taskNames = new Set(run.tasks.map((task: { name: string }) => task.name));
  expect(
    run.steps.every(
      (step: { task_name?: string }) => !step.task_name || taskNames.has(step.task_name),
    ),
  ).toBe(true);
  expect(run.steps.some((step: { task_name?: string }) => !!step.task_name)).toBe(true);
  await open(page, credentials.historyKey);
  await section(page, 'Workflows');
  await page.getByRole('link', { name: workflow.name, exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: `#${run.id}`, exact: true }).click();
  const pinned = page.getByRole('region', { name: 'Pinned workflow history' });
  await expect(pinned.getByRole('group', { name: 'Pinned revision tasks' })).toBeVisible();
  await expect(pinned.getByText(`Revision #${run.revision_id}`, { exact: true })).toBeVisible();
  await page.locator('details[aria-label="Current workflow definition context"] > summary').click();
  await expect(
    page.getByRole('link', { name: `Revision #${workflow.id}`, exact: true }),
  ).toBeVisible();
  const skips = Object.entries(run.task_decisions ?? {}).filter(
    ([, value]) => (value as { skipped: boolean }).skipped,
  );
  for (const [name, value] of skips) {
    const reason = (value as { reason?: string }).reason;
    await expect(
      pinned.getByRole('row').filter({ has: page.getByRole('rowheader', { name, exact: true }) }),
    ).toContainText(reason || 'Not recorded');
  }
  report.history = {
    workflowId: workflow.id,
    runId: run.id,
    pinnedRevision: run.revision_id,
    currentRevision: workflow.id,
    mappedSteps: run.steps.filter((step: { task_name?: string }) => step.task_name).length,
    skipDecisions: skips.length,
    realLedger: true,
  };
});

test('native UI retains one-time key across navigation, then discards and revokes without persistence', async ({
  page,
  request,
}) => {
  await open(page, credentials.seedKey);
  await page.getByRole('link', { name: 'Administration', exact: true }).first().click();
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.locator('summary').filter({ hasText: 'Use an exact workspace ID' }).click();
  await page.getByLabel('Workspace ID', { exact: true }).fill(credentials.tenantId);
  await page.getByRole('button', { name: 'Select workspace', exact: true }).click();
  await page.getByLabel('Key name', { exact: true }).fill(prefix + '-ui-key');
  await page.getByRole('checkbox', { name: /AdministratorAccess/ }).check();
  const createdResponse = page.waitForResponse(
    (response) =>
      response.url().includes(`/tenants/${credentials.tenantId}/api_keys`) &&
      response.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  const created = await (await createdResponse).json();
  owned.keys.push(created.id);
  record();
  const region = page.getByRole('region', { name: 'One-time API key' });
  await expect(region).toBeVisible();
  await expect(page.getByRole('button', { name: 'Select workspace', exact: true })).toBeDisabled();
  await expect(
    page.getByRole('button', { name: 'Management workspace', exact: true }),
  ).toBeDisabled();
  const matches = await page.getByLabel('Generated API key').inputValue();
  expect(matches === created.key).toBe(true);
  expect(
    await page.evaluate(
      (value) => JSON.stringify({ ...localStorage, ...sessionStorage }).includes(value),
      created.key,
    ),
  ).toBe(false);
  expect(page.url().includes(created.key)).toBe(false);
  await section(page, 'Jobs');
  await page.getByRole('link', { name: 'Administration', exact: true }).first().click();
  await expect(region).toBeVisible();
  await page.getByRole('button', { name: 'Discard key', exact: true }).click();
  await expect(region).toHaveCount(0);
  const row = page.getByRole('row').filter({
    has: page.getByRole('rowheader').getByText(prefix + '-ui-key', { exact: true }),
  });
  await row.locator('.admin-record-identity > summary').click();
  await expect(row.getByLabel('ID', { exact: true })).toHaveText(created.id);
  await row.getByRole('button', { name: 'Revoke', exact: true }).click();
  const revoke = page.waitForResponse(
    (response) =>
      response.url().includes(`/api_keys/${created.id}/revoke`) &&
      response.request().method() === 'POST',
  );
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Confirm revoke', exact: true })
    .click();
  expect((await revoke).status()).toBe(200);
  owned.keys = owned.keys.filter((id) => id !== created.id);
  await api(request, 'identity', created.key, 'GET', undefined, 401);
  report.oneTimeUI = {
    retainedAcrossNavigation: true,
    loadDisabledDuringReveal: true,
    explicitDiscard: true,
    nativeRevoke: 200,
    revokedAuthentication: 401,
    secretAbsentFromStorageAndURL: true,
  };
});

test('native independent list denial does not disable an explicitly granted create operation', async ({
  page,
}) => {
  await open(page, credentials.createOnlyKey);
  await page.getByRole('link', { name: 'Administration', exact: true }).first().click();
  await page.getByRole('tab', { name: 'Resource groups', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await page.getByLabel('Name', { exact: true }).fill(prefix + '-independent');
  await page.getByLabel('Slug', { exact: true }).fill(prefix + '-independent');
  await expect(
    page.getByRole('button', { name: 'Create resource group', exact: true }),
  ).toBeEnabled();
  const response = page.waitForResponse(
    (value) => value.url().includes('/resource_groups') && value.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create resource group', exact: true }).click();
  const result = await response;
  expect(result.status()).toBe(201);
  const group = await result.json();
  owned.groups.push(group.id);
  record();
  await createdReceipt(page, 'resource group', prefix + '-independent', group.id);
  await section(page, 'Jobs');
  await reviewChanges(page);
  const groupReceipt = await createdReceipt(
    page,
    'resource group',
    prefix + '-independent',
    group.id,
  );
  await groupReceipt
    .getByRole('button', { name: 'Dismiss resource group receipt', exact: true })
    .click();
  await expect(groupReceipt).toHaveCount(0);
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await page.getByLabel('Name', { exact: true }).fill(prefix + '-receipt-policy');
  await page.getByRole('button', { name: 'Advanced JSON', exact: true }).click();
  await page.getByRole('textbox', { name: 'Policy document', exact: true }).fill(
    JSON.stringify({
      version: '1',
      statement: [{ effect: 'Allow', action: ['job:Get'], resource: ['orbitjob:self:*:job/*'] }],
    }),
  );
  const policyResponse = page.waitForResponse(
    (value) => value.url().includes('/policies') && value.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  const policyResult = await policyResponse;
  expect(policyResult.status()).toBe(201);
  const policy = await policyResult.json();
  owned.policies.push(policy.id);
  record();
  await createdReceipt(page, 'policy', prefix + '-receipt-policy', policy.id);
  await section(page, 'Jobs');
  await reviewChanges(page);
  const policyReceipt = await createdReceipt(page, 'policy', prefix + '-receipt-policy', policy.id);
  await policyReceipt.getByRole('button', { name: 'Dismiss policy receipt', exact: true }).click();
  await expect(policyReceipt).toHaveCount(0);
  report.independentGrant = {
    list: 403,
    create: 201,
    groupId: group.id,
    policyId: policy.id,
    groupReceiptRetained: true,
    policyReceiptRetained: true,
  };
});

test('native tenant global grant can create a tenant while catalog reads retain current RLS scope', async ({
  request,
  page,
}) => {
  const before = await (await api(request, 'tenants', credentials.seedKey)).json();
  expect(before.items.every((row: { id: string }) => row.id === credentials.tenantId)).toBe(true);
  await api(request, `tenants/${credentials.tenantId}`, credentials.seedKey);
  await open(page, credentials.seedKey);
  await page.getByRole('link', { name: 'Administration', exact: true }).first().click();
  await page.getByRole('tab', { name: 'Workspaces', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill(prefix + '-seed-catalog');
  await page.getByLabel('Slug', { exact: true }).fill(prefix + '-seed-catalog');
  const createdResponse = page.waitForResponse(
    (value) =>
      new URL(value.url()).pathname === '/api/v1/tenants' && value.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  const createdResult = await createdResponse;
  expect(createdResult.status()).toBe(201);
  const created = await createdResult.json();
  owned.tenants.push(created.id);
  record();
  // The route grant is global, but ordinary tenant-key catalog queries retain
  // their own tenant transaction; do not disguise this as global visibility.
  await api(request, `tenants/${created.id}`, credentials.seedKey, 'GET', undefined, 404);
  const after = await (await api(request, 'tenants', credentials.seedKey)).json();
  expect(after.items.some((row: { id: string }) => row.id === created.id)).toBe(false);
  await createdReceipt(page, 'workspace', prefix + '-seed-catalog', created.id);
  await section(page, 'Jobs');
  await reviewChanges(page);
  const receipt = await createdReceipt(page, 'workspace', prefix + '-seed-catalog', created.id);
  await receipt.getByRole('button', { name: 'Dismiss workspace receipt', exact: true }).click();
  await expect(receipt).toHaveCount(0);
  report.seedCatalog = {
    ownList: 200,
    ownGet: 200,
    create: 201,
    createdTenantId: created.id,
    createdTenantGet: 404,
    createdTenantListed: false,
    nativeTenantReceiptRetained: true,
    scopeLimitation: 'Tenant-key RLS keeps credential tenant context even with global route grant.',
  };
});
