import { signInAccount } from './account-support';
import { test, expect, type Page } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import type { Budget } from '../src/api/monitoring';
const credentials = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));
const prefix = `${credentials.prefix}-monitoring-${Date.now()}`;
const headers = { Authorization: `Bearer ${credentials.key}` };
async function open(page: Page, key = credentials.key) {
  await page.goto('/console/connection');
  await signInAccount(page, key);
}
async function section(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}
async function createCheck(page: Page, status: number) {
  await section(page, 'Checks');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  const name = `${prefix}-${status}`;
  await page.getByLabel('Name', { exact: true }).fill(name);
  await page
    .getByLabel('URL', { exact: true })
    .fill('http://orbitjob-admin-api.orbitjob.svc.cluster.local:8080/healthz');
  await page.getByLabel('Expected status', { exact: true }).fill(String(status));
  await page.getByLabel('Interval (seconds)').fill('30');
  const retryLimit = status === 200 ? 0 : 1;
  await page.getByLabel('Retry limit').fill(String(retryLimit));
  const response = page.waitForResponse(
    (r) => new URL(r.url()).pathname.endsWith('/api/v1/checks') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create check', exact: true }).click();
  const r = await response;
  expect(r.status()).toBe(201);
  const item = await r.json();
  expect(item.retry_limit).toBe(retryLimit);
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  return item;
}

test('Installed M3 executes Checks, records SLI events, evaluates budgets and creates alerts', async ({
  page,
}) => {
  test.setTimeout(650_000);
  expect(process.env.KUBECONFIG).toBeTruthy();
  expect(
    execFileSync('kubectl', ['config', 'current-context'], { encoding: 'utf8' }).trim(),
  ).toMatch(/^kind-/);
  await open(page);
  const success = await createCheck(page, 200);
  const failed = await createCheck(page, 201);
  await section(page, 'SLOs');
  await page.getByRole('link', { name: 'SLIs', exact: true }).click();
  await page.getByRole('button', { name: 'New SLI', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill(`${prefix}-availability`);
  await page.getByText('Enter source ID manually', { exact: true }).click();
  await page.getByLabel('Source UID', { exact: true }).fill(`check-${failed.id}`);
  const sliResponse = page.waitForResponse(
    (r) => new URL(r.url()).pathname.endsWith('/api/v1/slis') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  const sr = await sliResponse;
  expect(sr.status()).toBe(201);
  const sli = await sr.json();
  await section(page, 'SLOs');
  await page.getByRole('button', { name: 'New SLO', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill(`${prefix}-objective`);
  await page.getByText('Enter indicator ID manually', { exact: true }).click();
  await page.getByLabel('SLI ID', { exact: true }).fill(String(sli.id));
  await page.getByLabel('Target (%)').fill('99');
  await page.getByLabel('Rolling window (hours)').fill('1');
  await page.getByLabel('Fast burn threshold').fill('1');
  await page.getByLabel('Slow burn threshold').fill('0.5');
  const sloResponse = page.waitForResponse(
    (r) => new URL(r.url()).pathname.endsWith('/api/v1/slos') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Create SLO', exact: true }).click();
  const or = await sloResponse;
  expect(or.status()).toBe(201);
  const slo = await or.json();
  expect(slo.window_type).toBe('rolling');
  const initial = await page.request.get(`/api/v1/slos/${slo.id}/budget`, { headers });
  expect(initial.status()).toBe(200);
  expect(['no_data', 'unevaluated', 'exhausted']).toContain((await initial.json()).status);
  const goodSLIResponse = await page.request.post('/api/v1/slis', {
    headers,
    data: {
      name: `${prefix}-good-sli`,
      sli_type: 'availability',
      source_type: 'job_run',
      source_config: { source_uid: `check-${success.id}` },
      good_event_criteria: { status: 'success' },
    },
  });
  expect(goodSLIResponse.status()).toBe(201);
  const goodSLI = await goodSLIResponse.json();
  const healthyResponse = await page.request.post('/api/v1/slos', {
    headers,
    data: {
      name: `${prefix}-healthy`,
      sli_id: goodSLI.id,
      target: 0.99,
      window_type: 'rolling',
      window_duration: '1h',
    },
  });
  expect(healthyResponse.status()).toBe(201);
  const healthy = await healthyResponse.json();
  const zeroResponse = await page.request.post('/api/v1/slos', {
    headers,
    data: {
      name: `${prefix}-zero`,
      sli_id: sli.id,
      target: 1,
      window_type: 'rolling',
      window_duration: '1h',
    },
  });
  expect(zeroResponse.status()).toBe(201);
  const zero = await zeroResponse.json();

  for (const [item, status] of [
    [success, 'success'],
    [failed, 'failed'],
  ] as const) {
    await expect
      .poll(
        async () => {
          const r = await page.request.get(`/api/v1/check-runs?check_id=${item.id}`, { headers });
          return (await r.json()).items?.some((i: { status: string }) => i.status === status);
        },
        { timeout: 180_000, intervals: [3000, 5000] },
      )
      .toBe(true);
  }
  async function measuredBudget(id: number | string, measured: (value: Budget) => boolean) {
    let budget!: Budget;
    await expect
      .poll(
        async () => {
          const response = await page.request.get(`/api/v1/slos/${id}/budget`, { headers });
          expect(response.status()).toBe(200);
          budget = await response.json();
          return measured(budget);
        },
        { timeout: 360_000, intervals: [5000, 10000] },
      )
      .toBe(true);
    return budget;
  }
  const [b, healthyBudget, zeroBudget] = await Promise.all([
    measuredBudget(slo.id, (value) => value.budget_consumed > 0 && value.budget_remaining < 0),
    measuredBudget(
      healthy.id,
      (value) =>
        value.status === 'healthy' && value.budget_total > 0 && value.budget_consumed === 0,
    ),
    measuredBudget(
      zero.id,
      (value) =>
        value.status === 'exhausted' &&
        value.budget_total === 0 &&
        value.budget_consumed > 0 &&
        value.budget_remaining < 0,
    ),
  ]);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Current error budget' })).toContainText(
    'exhausted',
  );
  expect(healthyBudget.budget_total).toBeGreaterThan(0);
  expect(healthyBudget.budget_consumed).toBe(0);
  expect(zeroBudget.budget_total).toBe(0);
  expect(zeroBudget.budget_consumed).toBeGreaterThan(0);
  expect(zeroBudget.budget_remaining).toBeLessThan(0);

  await page.getByRole('tab', { name: 'History', exact: true }).click();
  await expect(page.locator('tbody tr')).not.toHaveCount(0);
  await page.getByRole('tab', { name: 'Alerts', exact: true }).click();
  await expect(page.getByRole('link', { name: 'fast_burn', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'fast_burn', exact: true }).click();
  await expect(page.getByText('Burn at trigger', { exact: true })).toBeVisible();
  const alerts = await (
    await page.request.get(`/api/v1/slo-alerts?slo_id=${slo.id}`, { headers })
  ).json();
  const get = (path: string) => page.request.get('/api/v1/' + path, { headers });
  const results = await (await get(`check-runs?check_id=${failed.id}`)).json();
  await section(page, 'Checks');
  await page.getByRole('link', { name: success.name, exact: true }).click();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
  const paused = await (await get(`checks/${success.id}`)).json();
  const stale = await page.request.post(`/api/v1/checks/${success.id}/resume`, {
    headers,
    data: { version: 1 },
  });
  expect(stale.status()).toBe(409);
  await page.getByRole('button', { name: 'Resume', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm resume', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Pause', exact: true })).toBeVisible();
  for (const item of [success, failed]) {
    const current = await (await get(`checks/${item.id}`)).json();
    const r = await page.request.post(`/api/v1/checks/${item.id}/pause`, {
      headers,
      data: { version: current.version },
    });
    expect(r.status()).toBe(200);
  }
  await section(page, 'SLOs');
  await page.getByRole('link', { name: slo.name, exact: true }).click();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Resume', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm resume', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Pause', exact: true })).toBeVisible();
  const owned = (name: string) =>
    [success.id, failed.id].some(
      (id) => name.startsWith(`check-${id}-`) || name.startsWith(`oj-check-${id}-`),
    );
  const kubernetes = Object.fromEntries(
    ['jobruns', 'jobs', 'pods'].map((kind) => {
      const objects = JSON.parse(
        execFileSync('kubectl', ['get', kind, '-n', 'orbitjob', '-o', 'json'], {
          encoding: 'utf8',
          maxBuffer: 16 * 1024 * 1024,
        }),
      );
      return [
        kind,
        objects.items
          .filter((o: { metadata: { name: string } }) => owned(o.metadata.name))
          .map(
            (o: {
              metadata: { name: string };
              spec: {
                scheduledJobRef?: unknown;
                occurrenceKey?: string;
                containers?: { image: string }[];
              };
              status: unknown;
            }) => ({
              name: o.metadata.name,
              source: o.spec.scheduledJobRef,
              occurrenceKey: o.spec.occurrenceKey,
              images: o.spec.containers?.map((c) => c.image),
              status: o.status,
            }),
          ),
      ];
    }),
  );
  expect(
    kubernetes.jobruns.some((r: { status: { phase: string } }) => r.status.phase === 'Succeeded'),
  ).toBe(true);
  expect(
    kubernetes.jobruns.some((r: { status: { phase: string } }) => r.status.phase === 'Failed'),
  ).toBe(true);
  const evidence = {
    healthyId: healthy.id,
    zeroId: zero.id,
    healthySLIId: goodSLI.id,
    healthyBudget,
    zeroBudget,
    kubernetes,
    successId: success.id,
    failedId: failed.id,
    sliId: sli.id,
    sloId: slo.id,
    budget: b,
    alerts: alerts.items,
    result: results.items[0],
    pausedVersion: paused.version,
  };
  writeFileSync(process.env.ORBITJOB_M3_EVIDENCE!, JSON.stringify(evidence, null, 2));
  await section(page, 'SLOs');
  await page.getByRole('link', { name: zero.name, exact: true }).click();
  await expect(page.getByText('Zero allowance', { exact: true })).toBeVisible();
  await expect(page.getByText('Not comparable', { exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath('m3-zero.png'), fullPage: true });
  await section(page, 'SLOs');
  await page.getByRole('link', { name: healthy.name, exact: true }).click();
  await expect(page.getByRole('region', { name: 'Current error budget' })).toContainText('healthy');

  await section(page, 'SLOs');
  await page.getByRole('link', { name: slo.name, exact: true }).click();
  await page.screenshot({ path: test.info().outputPath('m3-budget.png'), fullPage: true });
});

test('Installed M3 group evidence stays visible only through its own parents', async ({ page }) => {
  test.setTimeout(180_000);
  const groupHeaders = { Authorization: `Bearer ${credentials.groupKey}` };
  const check = await page.request.post('/api/v1/checks', {
    headers: groupHeaders,
    data: {
      name: `${prefix}-group`,
      check_type: 'http_health',
      check_config: { url: 'http://orbitjob-admin-api.orbitjob.svc:8080/healthz' },
      schedule_type: 'interval',
      interval_sec: 30,
    },
  });
  expect(check.status()).toBe(201);
  const item = await check.json();
  expect(
    (await page.request.get(`/api/v1/checks/${item.id}`, { headers: groupHeaders })).status(),
  ).toBe(200);
  let ownCheckRunId: number | undefined;
  await expect
    .poll(
      async () => {
        const response = await page.request.get(`/api/v1/check-runs?check_id=${item.id}`, {
          headers: groupHeaders,
        });
        expect(response.status()).toBe(200);
        const body = await response.json();
        expect(body.items.every((run: { check_id: number }) => run.check_id === item.id)).toBe(
          true,
        );
        ownCheckRunId = body.items[0]?.id;
        return ownCheckRunId !== undefined;
      },
      { timeout: 90_000, intervals: [1000, 3000] },
    )
    .toBe(true);
  const ownRun = await page.request.get(`/api/v1/check-runs/${ownCheckRunId}`, {
    headers: groupHeaders,
  });
  expect(ownRun.status()).toBe(200);
  expect((await ownRun.json()).check_id).toBe(item.id);
  const sliResponse = await page.request.post('/api/v1/slis', {
    headers: groupHeaders,
    data: {
      name: `${prefix}-group-sli`,
      sli_type: 'availability',
      source_type: 'job_run',
      source_config: { source_uid: `check-${item.id}` },
      good_event_criteria: { status: 'success' },
    },
  });
  expect(sliResponse.status()).toBe(201);
  const groupSli = await sliResponse.json();
  const evidence = JSON.parse(readFileSync(process.env.ORBITJOB_M3_EVIDENCE!, 'utf8'));
  expect(evidence.result.id).toBeGreaterThan(0);
  expect(evidence.alerts.length).toBeGreaterThan(0);
  expect(evidence.sloId).toBeGreaterThan(0);
  for (const path of [
    `check-runs/${evidence.result.id}`,
    `slos/${evidence.sloId}/budget`,
    `slos/${evidence.sloId}/budgets`,
    ...evidence.alerts.map((alert: { id: number }) => `slo-alerts/${alert.id}`),
  ])
    expect((await page.request.get('/api/v1/' + path, { headers: groupHeaders })).status()).toBe(
      404,
    );
  for (const path of [
    `check-runs?check_id=${evidence.failedId}`,
    `slo-alerts?slo_id=${evidence.sloId}`,
  ]) {
    const response = await page.request.get('/api/v1/' + path, { headers: groupHeaders });
    expect(response.status()).toBe(200);
    expect((await response.json()).items).toEqual([]);
  }
  const inaccessible = await page.request.post('/api/v1/slos', {
    headers: groupHeaders,
    data: {
      name: `${prefix}-hidden-reference`,
      sli_id: evidence.sliId,
      target: 0.99,
      window_type: 'rolling',
      window_duration: '1h',
    },
  });
  expect(inaccessible.status()).toBe(404);
  const sloResponse = await page.request.post('/api/v1/slos', {
    headers: groupHeaders,
    data: {
      name: `${prefix}-group-slo`,
      sli_id: groupSli.id,
      target: 0.99,
      window_type: 'rolling',
      window_duration: '1h',
    },
  });
  expect(sloResponse.status()).toBe(201);
  const groupSlo = await sloResponse.json();
  for (const path of [
    `slos/${groupSlo.id}/budget`,
    `slos/${groupSlo.id}/budgets`,
    `slo-alerts?slo_id=${groupSlo.id}`,
  ])
    expect((await page.request.get('/api/v1/' + path, { headers: groupHeaders })).status()).toBe(
      200,
    );
  const groupDefinition = await page.request.get(`/api/v1/slos/${groupSlo.id}`, {
    headers: groupHeaders,
  });
  expect(groupDefinition.status()).toBe(200);
  expect((await groupDefinition.json()).current_budget).toBeUndefined();
  for (const [kind, id] of [
    ['slos', groupSlo.id],
    ['slis', groupSli.id],
  ] as const) {
    const existing = await page.request.get(`/api/v1/${kind}/${id}`, { headers: groupHeaders });
    expect(existing.status()).toBe(200);
    const removed = await page.request.delete(`/api/v1/${kind}/${id}`, {
      headers: groupHeaders,
      data: { version: (await existing.json()).version },
    });
    expect(removed.status()).toBe(200);
  }
  const currentCheck = await page.request.get(`/api/v1/checks/${item.id}`, {
    headers: groupHeaders,
  });
  expect(currentCheck.status()).toBe(200);
  const deleted = await page.request.delete(`/api/v1/checks/${item.id}`, {
    headers: groupHeaders,
    data: { version: (await currentCheck.json()).version },
  });
  expect(deleted.status()).toBe(200);
});
