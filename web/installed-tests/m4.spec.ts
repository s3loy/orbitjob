import { accountWriteHeaders, signInAccount } from './account-support';
import { test, expect, type Page } from '@playwright/test';
import type { Workflow, WorkflowRun, FunctionRun, FunctionDefinition } from '../src/api/m4';
import type { Run, RunDetail } from '../src/api/contracts';
type Attempt = RunDetail['attempts'][number];
type TaskDecision = { task: string; phase: string; attempt: number; skipped?: boolean };
import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
const c = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));
const manifest = JSON.parse(
  readFileSync(
    join(dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!), 'm4-functions.json'),
    'utf8',
  ),
);
const cases = Object.fromEntries(
  manifest.functions.map((f: { case: string; id: number; name: string }) => [f.case, f]),
);
const evidence: Record<string, unknown> = {};
const headers = (key = c.key) => ({ Authorization: `Bearer ${key}` });
async function open(page: Page, key = c.key) {
  await page.goto('/console/connection');
  await signInAccount(page, key);
}
async function nav(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}
async function get(page: Page, path: string, key = c.key) {
  const response = await page.request.get(`/api/v1/${path}`, { headers: headers(key) });
  expect(response.status()).toBe(200);
  return response.json();
}
async function invoke(page: Page, item: { id: number; name: string }) {
  await nav(page, 'Functions');
  await page.getByRole('link', { name: item.name, exact: true }).click();
  await page.getByRole('button', { name: `Invoke ${item.name}`, exact: true }).click();
  const response = page.waitForResponse(
    (r) => r.url().includes(`/functions/${item.id}/invoke`) && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Confirm invoke', exact: true }).click();
  const r = await response;
  expect(r.status()).toBe(201);
  const receipt = await r.json();
  await expect(
    page.getByRole('region', { name: `Function invocation receipt for ${item.name}` }),
  ).toBeVisible();
  return { receipt, url: r.url(), key: r.request().headers()['x-orbitjob-idempotency-key'] };
}
function kube(resource: string, name?: string) {
  return JSON.parse(
    execFileSync(
      'kubectl',
      ['get', resource, ...(name ? [name] : []), '-n', 'orbitjob', '-o', 'json'],
      { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 },
    ),
  );
}
test.afterEach(() =>
  writeFileSync(
    join(dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!), 'm4-evidence.json'),
    JSON.stringify(evidence, null, 2),
  ),
);

test('native Functions execute, retry, deduplicate and record terminal history; real cancellation', async ({
  page,
}) => {
  test.setTimeout(350_000);
  expect(execFileSync('kubectl', ['config', 'current-context'], { encoding: 'utf8' }).trim()).toBe(
    'kind-orbitjob-console-e2e',
  );
  await open(page);
  for (const kind of ['success', 'failed', 'retry']) {
    const item = cases[kind];
    const priorIds = new Set(
      (await get(page, `functions/${item.id}/runs?limit=100`)).items.map(
        (r: FunctionRun) => r.run_id,
      ),
    );
    const { receipt, url, key } = await invoke(page, item);
    const replay = await page.request.post(url, {
      headers: { ...(await accountWriteHeaders(page)), 'X-OrbitJob-Idempotency-Key': key },
    });
    expect(replay.status()).toBe(200);
    expect(await replay.json()).toMatchObject({
      name: receipt.name,
      occurrence_key: receipt.occurrence_key,
      created: false,
    });
    const cr = kube('jobrun', receipt.name);
    expect(cr.spec.actor).toBe(c.userId);
    expect(cr.spec.trigger).toBe('Function');
    expect(cr.spec.definitionRevision).toBeGreaterThan(0);
    expect(cr.spec.scheduledJobRef.uid).toBe(`function-${item.id}`);
    let outcome!: FunctionRun;
    await expect
      .poll(
        async () => {
          const history = await get(page, `functions/${item.id}/runs?limit=20`);
          outcome = history.items.find((r: FunctionRun) => !priorIds.has(r.run_id));
          return outcome?.status;
        },
        { timeout: 100_000, intervals: [2000] },
      )
      .toBe(kind === 'failed' ? 'failed' : 'success');
    const ledger = (
      await get(
        page,
        `instances?source_uid=function-${item.id}&occurrence_key=${receipt.occurrence_key}`,
      )
    ).items;
    expect(ledger).toHaveLength(1);
    expect(ledger[0].actor).toBe(c.userId);
    expect(Date.parse(outcome.triggered_at)).toBe(Date.parse(ledger[0].created_at));
    expect(ledger[0].attempt).toBe(kind === 'retry' ? 2 : 1);
    const detail = await get(page, `instances/${ledger[0].id}`);
    expect(detail.attempts).toHaveLength(kind === 'retry' ? 2 : 1);
    if (kind === 'retry')
      expect(detail.attempts.map((a: Attempt) => a.phase)).toEqual(['Failed', 'Succeeded']);
    await page.getByRole('button', { name: 'Refresh invocations', exact: true }).click();
    await page.getByTitle(outcome.run_id, { exact: true }).click();
    await expect(
      page.getByRole('heading', { name: `Invocation ${outcome.run_id.slice(0, 8)}…` }),
    ).toBeVisible();
    expect(new URL(page.url()).pathname).toContain(outcome.run_id);
    await expect(page.getByRole('link', { name: /View run/ })).toHaveCount(0);
    evidence[kind] = {
      receipt,
      revision: cr.spec.definitionRevision,
      outcome,
      attempts: detail.attempts.map((a: Attempt) => ({
        phase: a.phase,
        attempt_number: a.attempt_number,
      })),
    };
  }
  const item = cases.long;
  const priorIds = new Set(
    (await get(page, `functions/${item.id}/runs?limit=100`)).items.map(
      (r: FunctionRun) => r.run_id,
    ),
  );
  const { receipt } = await invoke(page, item);
  let ledger!: Run;
  await expect
    .poll(
      async () => {
        ledger = (
          await get(
            page,
            `instances?source_uid=function-${item.id}&occurrence_key=${receipt.occurrence_key}`,
          )
        ).items[0];
        return ledger?.phase;
      },
      { timeout: 60_000, intervals: [1000] },
    )
    .toBe('Running');
  const canceled = await page.request.post(`/api/v1/instances/${ledger.id}/cancel`, {
    headers: headers(),
  });
  expect(canceled.status()).toBe(200);
  let canceledOutcome!: FunctionRun;
  await expect
    .poll(
      async () => {
        canceledOutcome = (await get(page, `functions/${item.id}/runs?limit=100`)).items.find(
          (r: FunctionRun) => !priorIds.has(r.run_id),
        );
        return canceledOutcome?.status;
      },
      {
        timeout: 80_000,
        intervals: [2000],
      },
    )
    .toBe('canceled');
  await expect
    .poll(() => kube('jobrun', receipt.name).status?.phase, { timeout: 30_000, intervals: [500] })
    .toBe('Canceled');
  const functionCancel = {
    receipt,
    ledgerId: ledger.id,
    outcome: canceledOutcome,
    crPhase: kube('jobrun', receipt.name).status.phase,
  };
  evidence.functionCancel = functionCancel;
  expect(functionCancel.crPhase).toBe('Canceled');
});

test('native Workflows execute dependencies/conditions/retries and cancel active steps', async ({
  page,
}) => {
  test.setTimeout(350_000);
  await open(page);
  await nav(page, 'Workflows');
  const definitions = (await get(page, 'workflows?limit=100')).items;
  const dag = definitions.find((w: Workflow) => w.name === 'console-m4-dag');
  const cancel = definitions.find((w: Workflow) => w.name === 'console-m4-cancel');
  expect(dag).toBeTruthy();
  expect(cancel).toBeTruthy();
  await page.getByRole('link', { name: dag.name, exact: true }).click();
  await page
    .getByRole('button', { name: /publish console-m4-success After validate, failed/ })
    .click();
  await expect(page.getByRole('region', { name: 'Selected task' })).toContainText(
    'validate.attempts >= 0',
  );
  async function trigger(def: Workflow) {
    await page.getByRole('button', { name: `Trigger ${def.name}`, exact: true }).click();
    const wait = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname.endsWith(`/workflows/${def.id}/runs`) &&
        r.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Confirm trigger' }).click();
    const response = await wait;
    expect(response.status()).toBe(201);
    const receipt = await response.json();
    expect(new URL(response.url()).searchParams.get('tenant_id')).toBe(c.tenantId);
    const replay = await page.request.post(response.url(), {
      headers: {
        ...(await accountWriteHeaders(page)),
        'X-OrbitJob-Idempotency-Key': response.request().headers()['x-orbitjob-idempotency-key'],
      },
    });
    expect(replay.status()).toBe(200);
    expect(await replay.json()).toMatchObject({
      name: receipt.name,
      occurrence_key: receipt.occurrence_key,
      created: false,
    });
    expect(kube('workflowrun', receipt.name).spec.actor).toBe(c.userId);
    return receipt;
  }
  const receipt = await trigger(dag);
  let run!: WorkflowRun;
  await expect
    .poll(
      async () => {
        run = (await get(page, `workflows/${dag.id}/runs?limit=20`)).items.find(
          (r: WorkflowRun) => r.occurrence_key === receipt.occurrence_key,
        );
        return run?.phase;
      },
      { timeout: 150_000, intervals: [2000] },
    )
    .toBe('Failed');
  const detail = await get(page, `workflows/${dag.id}/runs/${run.id}`);
  expect(detail.steps).toHaveLength(5);
  const cr = kube('workflowrun', receipt.name);
  const byTask = Object.fromEntries(cr.status.steps.map((s: TaskDecision) => [s.task, s]));
  expect(byTask.failed).toMatchObject({ phase: 'Failed', attempt: 2 });
  expect(byTask.unconditional.phase).toBe('Succeeded');
  expect(byTask.publish.phase).toBe('Succeeded');
  expect(byTask.skipped.skipped).toBe(true);
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('button', { name: 'Refresh', exact: true }).last().click();
  await page.getByRole('link', { name: `#${run.id}`, exact: true }).click();
  await expect(page.getByRole('heading', { name: `Workflow run #${run.id}` })).toBeVisible();
  const step = detail.steps[0];
  await page.getByRole('link', { name: `#${step.id}`, exact: true }).click();
  await expect(page.getByRole('heading', { name: `Run #${step.id}`, exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Workflow run', exact: true }).click();
  evidence.workflowDAG = { receipt, run: detail, observedTaskDecisions: cr.status.steps };
  await nav(page, 'Workflows');
  await page.getByRole('link', { name: cancel.name, exact: true }).click();
  const cancelReceipt = await trigger(cancel);
  let active!: WorkflowRun;
  await expect
    .poll(
      async () => {
        active = (await get(page, `workflows/${cancel.id}/runs`)).items.find(
          (r: WorkflowRun) => r.occurrence_key === cancelReceipt.occurrence_key,
        );
        if (!active) return '';
        const detail = await get(page, `workflows/${cancel.id}/runs/${active.id}`);
        return detail.steps[0]?.phase;
      },
      { timeout: 60_000, intervals: [1000] },
    )
    .toBe('Running');
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('button', { name: 'Refresh', exact: true }).last().click();
  await page.getByRole('link', { name: `#${active.id}`, exact: true }).click();
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancel request' }).click();
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
  await expect(page.locator('.run-meta')).toContainText('Canceled', { timeout: 90_000 });
  const final = await get(page, `workflows/${cancel.id}/runs/${active.id}`);
  expect(final.steps).toHaveLength(1);
  expect(final.steps[0].phase).toBe('Canceled');
  expect(kube('workflowrun', cancelReceipt.name).spec.cancelRequested).toBe(true);
  evidence.workflowCancel = { receipt: cancelReceipt, run: final };
  await page.getByRole('link', { name: 'Back to Workflow runs', exact: true }).click();
  await page.getByRole('link', { name: `#${active.id}`, exact: true }).click();
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
});

test('native M4 grants, group/tenant scope, independent step permission and revoked-key recovery', async ({
  page,
}) => {
  test.setTimeout(180_000);
  await open(page);
  const definitions = (await get(page, 'workflows?limit=100')).items;
  const dag = definitions.find((w: Workflow) => w.name === 'console-m4-dag');
  const run = (await get(page, `workflows/${dag.id}/runs`)).items[0];
  expect(run).toBeTruthy();
  const outcome = (await get(page, `functions/${cases.success.id}/runs`)).items[0];
  expect(outcome).toBeTruthy();
  const matrix: unknown[] = [];
  async function status(path: string, key: string, want: number, method = 'GET') {
    const r = await page.request.fetch(`/api/v1/${path}`, { method, headers: headers(key) });
    expect(r.status(), `${method} ${path}`).toBe(want);
    matrix.push({ path, method, status: r.status() });
  }
  const grouped = (await get(page, 'functions?limit=100', c.groupKey)).items;
  expect(grouped.map((f: FunctionDefinition) => f.id)).toEqual([cases.grouped.id]);
  async function unavailable(path: string, key: string, method = 'GET') {
    const r = await page.request.fetch(`/api/v1/${path}`, { method, headers: headers(key) });
    expect(r.status(), `${method} ${path}`).toBe(403);
    expect(await r.json()).toEqual({
      error: {
        code: 'FORBIDDEN',
        message: 'resource is unavailable in the requested scope',
      },
    });
    matrix.push({ path, method, status: r.status() });
  }
  // The persisted-parent guard hides absent and out-of-scope definitions identically.
  for (const suffix of ['', '/runs', `/runs/${outcome.run_id}`])
    await unavailable(`functions/${cases.success.id}${suffix}`, c.groupKey);
  await unavailable(`functions/${cases.success.id}/invoke`, c.groupKey, 'POST');
  for (const suffix of ['', '/runs'])
    await unavailable(`functions/${cases.foreign.id}${suffix}`, c.key);
  await unavailable(`functions/${cases.foreign.id}/invoke`, c.key, 'POST');
  await unavailable(`functions/${cases.success.id}`, c.foreignKey);
  await unavailable('functions/9223372036854775807', c.groupKey);
  await status(`functions/${cases.foreign.id}/invoke`, c.foreignKey, 409, 'POST');
  for (const [path, method] of [
    ['workflows', 'GET'],
    [`workflows/${dag.id}`, 'GET'],
    [`workflows/${dag.id}/runs`, 'GET'],
    [`workflows/${dag.id}/runs`, 'POST'],
    [`workflows/${dag.id}/runs/${run.id}`, 'GET'],
    [`workflows/${dag.id}/runs/${run.id}/cancel`, 'POST'],
  ])
    await status(path, c.groupKey, 403, method);
  for (const key of [c.readKey, c.denyKey]) {
    await status(`functions/${cases.success.id}/invoke`, key, 403, 'POST');
    await status(`workflows/${dag.id}/runs`, key, 403, 'POST');
    await status(`workflows/${dag.id}/runs/${run.id}/cancel`, key, 403, 'POST');
  }
  await status(`functions/${cases.success.id}`, c.getOnlyKey, 200);
  await status('functions', c.getOnlyKey, 403);
  await status(`functions/${cases.success.id}/runs`, c.getOnlyKey, 403);
  await status(`functions/${cases.success.id}/runs/${outcome.run_id}`, c.getOnlyKey, 200);
  await status(`workflows/${dag.id}`, c.getOnlyKey, 200);
  await status(`workflows/${dag.id}/runs`, c.getOnlyKey, 403);
  await status(`workflows/${dag.id}/runs/${run.id}`, c.getOnlyKey, 200);
  await status(`functions/${cases.paused.id}/invoke`, c.key, 409, 'POST');
  const paused = definitions.find((w: Workflow) => w.name === 'console-m4-paused');
  // Suspension stops automatic scheduling; manual Workflow runs remain available.
  await status(`workflows/${paused.id}/runs`, c.key, 201, 'POST');
  await open(page, c.workflowKey);
  await nav(page, 'Workflows');
  await page.getByRole('link', { name: dag.name, exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: `#${run.id}`, exact: true }).click();
  const detail = await get(page, `workflows/${dag.id}/runs/${run.id}`, c.workflowKey);
  await page.getByRole('link', { name: `#${detail.steps[0].id}`, exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await page.getByRole('link', { name: 'Back to Workflow run', exact: true }).click();
  await expect(page.getByRole('heading', { name: `Workflow run #${run.id}` })).toBeVisible();
  await status('workflows', c.groupKey, 403);
  await status(`functions/${cases.grouped.id}`, c.groupKey, 200);
  // A fresh key makes revocation repeatable without sharing browser credentials.
  const bootstrap = c.seedKey;
  const minted = await page.request.post(`/api/v1/tenants/${c.tenantId}/api_keys`, {
    headers: headers(bootstrap),
    data: { policies: [c.policies[0]] },
  });
  expect(minted.status()).toBe(201);
  const revokable = await minted.json();
  await status('functions', revokable.key, 200);
  await status(`api_keys/${revokable.id}/revoke`, bootstrap, 200, 'POST');
  await status('functions', revokable.key, 401);
  evidence.permissions = matrix;
});

test('native Function canceled before first attempt keeps unrecorded start and duration', async ({
  page,
}) => {
  test.setTimeout(180_000);
  await open(page);
  const priorIds = new Set(
    (await get(page, `functions/${cases.long.id}/runs?limit=100`)).items.map(
      (r: FunctionRun) => r.run_id,
    ),
  );
  const kubectl = (args: string[]) =>
    execFileSync('kubectl', [...args, '-n', 'orbitjob'], { encoding: 'utf8', timeout: 90_000 });
  const priorReplicas = JSON.parse(kubectl(['get', 'deployment/orbitjob-operator', '-o', 'json']))
    .spec.replicas;
  kubectl(['scale', 'deployment/orbitjob-operator', '--replicas=0']);
  let receipt: { name: string; occurrence_key: string } | undefined;
  try {
    await expect
      .poll(
        () =>
          JSON.parse(kubectl(['get', 'deployment/orbitjob-operator', '-o', 'json'])).status
            .replicas ?? 0,
        { timeout: 60_000, intervals: [1000] },
      )
      .toBe(0);
    const response = await page.request.post(`/api/v1/functions/${cases.long.id}/invoke`, {
      headers: { ...headers(), 'X-OrbitJob-Idempotency-Key': crypto.randomUUID() },
    });
    expect(response.status()).toBe(201);
    receipt = await response.json();
    kubectl([
      'patch',
      'jobrun',
      receipt!.name,
      '--type=merge',
      '-p',
      '{"spec":{"cancelRequested":true}}',
    ]);
  } finally {
    kubectl(['scale', 'deployment/orbitjob-operator', `--replicas=${priorReplicas}`]);
    kubectl(['rollout', 'status', 'deployment/orbitjob-operator', '--timeout=90s']);
  }
  let outcome!: FunctionRun;
  await expect
    .poll(
      async () => {
        const rows = (await get(page, `functions/${cases.long.id}/runs`)).items;
        outcome = rows.find(
          (r: FunctionRun) =>
            r.status === 'canceled' && r.started_at == null && !priorIds.has(r.run_id),
        );
        return outcome?.status;
      },
      { timeout: 80_000, intervals: [2000] },
    )
    .toBe('canceled');
  expect(outcome.started_at).toBeNull();
  expect(outcome.duration_ms).toBeNull();
  const ledger = (
    await get(
      page,
      `instances?source_uid=function-${cases.long.id}&occurrence_key=${receipt!.occurrence_key}`,
    )
  ).items;
  expect(ledger).toHaveLength(1);
  expect(ledger[0].attempt).toBe(0);
  expect(Date.parse(outcome.triggered_at)).toBe(Date.parse(ledger[0].created_at));
  await nav(page, 'Functions');
  await page.getByRole('link', { name: cases.long.name, exact: true }).click();
  await page.getByTitle(outcome.run_id, { exact: true }).click();
  await expect(page.getByText('Not recorded', { exact: true })).toHaveCount(2);
  evidence.prestartCancellation = { receipt, outcome, ledgerId: ledger[0].id };
});

test('native API Function group invocation and independently denied history; policy boundary caps writes', async ({
  page,
}) => {
  test.setTimeout(140_000);
  const bootstrap = c.key;
  const policies: string[] = [];
  const keys: string[] = [];
  async function createPolicy(name: string, statement: unknown[]) {
    const r = await page.request.post('/api/v1/policies', {
      headers: headers(bootstrap),
      data: { name, document: { version: '2026-01-01', statement } },
    });
    expect(r.status()).toBe(201);
    const p = await r.json();
    policies.push(p.id);
    return p.id;
  }
  async function createKey(
    policy: string,
    options: { boundary?: string; setupKey?: string; group?: string } = {},
  ) {
    const r = await page.request.post(`/api/v1/tenants/${c.tenantId}/api_keys`, {
      headers: headers(options.setupKey ?? bootstrap),
      data: {
        policies: [policy],
        ...(options.boundary ? { boundary_policy_id: options.boundary } : {}),
        ...(options.group ? { resource_group_id: options.group } : {}),
      },
    });
    expect(r.status()).toBe(201);
    const k = await r.json();
    keys.push(k.id);
    return k.key;
  }
  try {
    const groupInvocation = await createPolicy(`m4-group-invoke-${Date.now()}`, [
      {
        effect: 'Allow',
        action: ['function:List', 'function:Invoke'],
        resource: [`orbitjob:self:${c.groupId}:function/${cases.grouped.id}`],
      },
    ]);
    const groupKey = await createKey(groupInvocation, { group: c.groupId });
    const priorIds = new Set(
      (await get(page, `functions/${cases.grouped.id}/runs?limit=100`, groupKey)).items.map(
        (r: FunctionRun) => r.run_id,
      ),
    );
    const invokePath = `/api/v1/functions/${cases.grouped.id}/invoke?wait_seconds=0`;
    const invocation = await page.request.post(invokePath, {
      headers: headers(groupKey),
    });
    expect(invocation.status()).toBe(201);
    const receipt = await invocation.json();
    await expect
      .poll(
        async () =>
          (await get(page, `functions/${cases.grouped.id}/runs`, groupKey)).items.some(
            (r: FunctionRun) => r.status === 'success' && !priorIds.has(r.run_id),
          ),
        { timeout: 70_000, intervals: [1000] },
      )
      .toBe(true);
    const completed = (await get(page, `functions/${cases.grouped.id}/runs`, groupKey)).items.find(
      (row: FunctionRun) => row.status === 'success' && !priorIds.has(row.run_id),
    ) as FunctionRun;
    expect(completed).toBeDefined();
    const detailPath = `/api/v1/functions/${cases.grouped.id}/runs/${completed.run_id}`;
    expect((await page.request.get(detailPath, { headers: headers(groupKey) })).status()).toBe(403);
    expect((await page.request.get(detailPath, { headers: headers(c.groupKey) })).status()).toBe(
      200,
    );
    const qualified = await createPolicy(`m4-history-${Date.now()}`, [
      {
        effect: 'Allow',
        action: ['function:List', 'function:Get', 'function:Invoke'],
        resource: ['orbitjob:self:*:function/*'],
      },
      {
        effect: 'Deny',
        action: ['function:List'],
        resource: [`orbitjob:self:*:function/${cases.success.id}`],
      },
    ]);
    const key = await createKey(qualified);
    const deniedHistory = await page.request.get(`/api/v1/functions/${cases.success.id}/runs`, {
      headers: headers(key),
    });
    expect(deniedHistory.status()).toBe(403);
    expect(
      (
        await page.request.get(`/api/v1/functions/${cases.success.id}`, { headers: headers(key) })
      ).status(),
    ).toBe(200);
    const boundary = await createPolicy(`m4-boundary-${Date.now()}`, [
      {
        effect: 'Allow',
        action: ['function:Get', 'function:List', 'workflow:Get', 'workflow:List'],
        resource: ['orbitjob:self:*:*/*'],
      },
    ]);
    const boundKey = await createKey(c.policies[0], { boundary, setupKey: c.seedKey });
    const denied = await page.request.post(`/api/v1/functions/${cases.success.id}/invoke`, {
      headers: headers(boundKey),
    });
    expect(denied.status()).toBe(403);
    const definitions = (await get(page, 'workflows', boundKey)).items;
    const w = definitions.find((w: Workflow) => w.name === 'console-m4-dag');
    expect(
      (
        await page.request.post(`/api/v1/workflows/${w.id}/runs`, { headers: headers(boundKey) })
      ).status(),
    ).toBe(403);
    evidence.boundaries = {
      groupInvocation: receipt,
      groupListInvokeWithoutGet403: true,
      groupFullDetail200: true,
      qualifiedHistory403: true,
      configuration200: true,
      boundaryFunctionInvoke403: true,
      boundaryWorkflowTrigger403: true,
    };
  } finally {
    for (const id of keys)
      expect(
        (
          await page.request.post(`/api/v1/api_keys/${id}/revoke`, { headers: headers(bootstrap) })
        ).status(),
      ).toBe(200);
    const retainedPolicies = [];
    for (const id of policies.reverse()) {
      const response = await page.request.delete(`/api/v1/policies/${id}`, {
        headers: headers(bootstrap),
      });
      expect([204, 409]).toContain(response.status());
      if (response.status() === 409) retainedPolicies.push(id);
    }
    evidence.retainedPolicies = retainedPolicies;
  }
});

test('native Workflow revision change retains historical pinned identity and recovers inactive links', async ({
  page,
}) => {
  test.setTimeout(120_000);
  await open(page);
  const before = (await get(page, 'workflows?limit=100')).items.find(
    (w: Workflow) => w.name === 'console-m4-dag',
  );
  const historic = (await get(page, `workflows/${before.id}/runs`)).items[0];
  expect(historic).toBeTruthy();
  execFileSync(
    'kubectl',
    [
      'patch',
      'workflowjob',
      'console-m4-dag',
      '-n',
      'orbitjob',
      '--type=merge',
      '-p',
      JSON.stringify({ spec: { timeoutSeconds: before.timeout_seconds === 300 ? 301 : 300 } }),
    ],
    { encoding: 'utf8' },
  );
  let current!: Workflow;
  await expect
    .poll(
      async () => {
        current = (await get(page, 'workflows?limit=100')).items.find(
          (w: Workflow) => w.name === 'console-m4-dag',
        );
        return String(current.id) !== String(before.id);
      },
      { timeout: 45_000, intervals: [1000] },
    )
    .toBe(true);
  expect(
    (await page.request.get(`/api/v1/workflows/${before.id}`, { headers: headers() })).status(),
  ).toBe(404);
  const detail = await get(page, `workflows/${current.id}/runs/${historic.id}`);
  expect(detail.revision_id).toBe(historic.revision_id);
  await page.goto(`/console/workflows/${before.id}`);
  await signInAccount(page, c.key);
  await expect(page.getByRole('alert')).toBeVisible();
  await page.getByRole('link', { name: 'Return to Workflows', exact: true }).click();
  await page.getByRole('link', { name: 'console-m4-dag', exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: `#${historic.id}`, exact: true }).click();
  const pinned = page.getByRole('region', { name: 'Pinned workflow history', exact: true });
  await expect(
    pinned.getByText(`Revision #${historic.revision_id}`, { exact: true }),
  ).toBeVisible();
  const revisionNotice = page.getByRole('status').filter({
    hasText: 'This run uses a different revision',
  });
  await expect(revisionNotice).toContainText(
    `This run is pinned to revision #${historic.revision_id}.`,
  );
  await expect(
    revisionNotice.getByRole('link', { name: `#${current.id}`, exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('group', { name: 'Current definition tasks' })).toHaveCount(0);
  evidence.revisionChange = {
    previousRevision: before.id,
    currentRevision: current.id,
    historicalRevision: historic.revision_id,
    historicalRun: historic.id,
  };
});
