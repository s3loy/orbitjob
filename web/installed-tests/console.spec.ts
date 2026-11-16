import { accountWriteHeaders, signInAccount } from './account-support';
import { test, expect, type Page } from '@playwright/test';
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import { execFileSync } from 'node:child_process';
import type { RunDetail } from '../src/api/contracts';
import {
  expectCancellationResourcesGone,
  holdCancellationJob,
  readTrustedCancellationEvidence,
  releaseCancellationHold,
} from './cancellation-evidence';

// No route mocks or persisted browser credentials. Tracing is disabled in the
// dedicated config because HTTP recordings contain account passwords and API credentials.
const credentials = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8')) as {
  key: string;
  username: string;
  password: string;
  userId: string;
  groupKey: string;
  foreignKey: string;
  keyId: string;
};
const namespace = 'orbitjob';
async function attachEvidence(
  name: string,
  options: { body: string | Buffer; contentType: string },
) {
  const path = test.info().outputPath(name);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, options.body);
  await test.info().attach(name, { path, contentType: options.contentType });
}

async function cluster(resource: string, selector?: string) {
  const args = ['get', resource, '-n', namespace, '-o', 'json'];
  if (selector) args.push('-l', selector);
  return JSON.parse(execFileSync('kubectl', args, { encoding: 'utf8' }));
}
test.beforeAll(async () => {
  const origin = new URL(process.env.ORBITJOB_CONSOLE_URL ?? 'http://127.0.0.1:18081');
  expect(['127.0.0.1', 'localhost']).toContain(origin.hostname);
  expect(
    process.env.KUBECONFIG,
    'Use an explicit kubeconfig for a disposable kind cluster',
  ).toBeTruthy();
  const context = execFileSync('kubectl', ['config', 'current-context'], {
    encoding: 'utf8',
  }).trim();
  expect(context).toMatch(/^kind-/);
  const fixture = await cluster('scheduledjob/console-success');
  expect(fixture.metadata.labels['orbitjob.io/console-check']).toBeTruthy();
});

async function open(page: Page) {
  await page.goto('/console/');
  await signInAccount(page, credentials.key);
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await expect(page.getByRole('link', { name: 'console-success', exact: true })).toBeVisible();
}
async function trigger(page: Page, job: string) {
  await page.getByRole('button', { name: `Trigger ${job}`, exact: true }).click();
  const accepted = page.waitForResponse(
    (r) => new URL(r.url()).pathname.endsWith('/trigger') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const response = await accepted;
  expect(response.status()).toBe(201);
  const receipt = await response.json();
  const runUID = (await cluster(`jobrun/${receipt.name}`)).metadata.uid;
  const intent = response.request().headers()['x-orbitjob-idempotency-key'];
  const region = page.getByRole('region', { name: `Trigger receipt for ${job}`, exact: true });
  await expect(region).toContainText(`Trigger accepted for ${job}`);
  await page.getByRole('navigation').getByRole('link', { name: 'Runs', exact: true }).click();
  const activity = page.getByRole('group', { name: 'Operation activity', exact: true });
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator('summary').first().click();
  await expect(region).toBeVisible();
  const link = region.getByRole('link', { name: 'View run', exact: true });
  await expect(link).toBeVisible();
  const id = (await link.getAttribute('href'))!.split('/').at(-1)!;
  await link.click();
  return { id, receipt, runUID, intent, triggerURL: response.url() };
}
function readRun(page: Page, id: string) {
  return page.request.get(`/api/v1/instances/${id}`, {
    headers: { Authorization: `Bearer ${credentials.key}` },
  });
}
async function outcome(page: Page, id: string, phase: string, count: number) {
  await expect(page.locator('.run-meta').getByText(phase, { exact: true })).toBeVisible();
  const response = await readRun(page, id);
  expect(response.status()).toBe(200);
  const run = (await response.json()) as RunDetail;
  expect(run.phase).toBe(phase);
  expect(run.attempts).toHaveLength(count);
  expect(run.actor).toBe(credentials.userId);
  for (const attempt of run.attempts) {
    expect(attempt.completed_at).toBeTruthy();
    expect(Date.parse(attempt.completed_at!)).toBeGreaterThanOrEqual(
      Date.parse(attempt.started_at!),
    );
  }
  await expect
    .poll(
      async () =>
        (await cluster('jobruns')).items.find(
          (r: { spec: { occurrenceKey: string } }) => r.spec.occurrenceKey === run.occurrence_key,
        )?.status.phase,
    )
    .toBe(phase);
  await attachEvidence('observed-run.json', {
    body: JSON.stringify(run, null, 2),
    contentType: 'application/json',
  });
  await attachEvidence('console.png', {
    body: await page.screenshot({ fullPage: true }),
    contentType: 'image/png',
  });
  return run;
}

test('Installed Console triggers once and observes a successful workload', async ({ page }) => {
  await open(page);
  const active = await trigger(page, 'console-success');
  const run = await outcome(page, active.id, 'Succeeded', 1);
  expect(run.attempts[0].phase).toBe('Succeeded');
  const replay = await page.request.post(active.triggerURL, {
    headers: {
      ...(await accountWriteHeaders(page)),
      'X-OrbitJob-Idempotency-Key': active.intent,
    },
    data: {},
  });
  expect(replay.status()).toBe(200);
  expect(await replay.json()).toMatchObject({
    name: active.receipt.name,
    occurrence_key: active.receipt.occurrence_key,
    created: false,
  });
  const anotherActor = await page.request.post(active.triggerURL, {
    headers: {
      Authorization: `Bearer ${credentials.key}`,
      'X-OrbitJob-Idempotency-Key': active.intent,
    },
    data: {},
  });
  expect(anotherActor.status()).toBe(409);
  expect((await cluster(`jobrun/${active.receipt.name}`)).spec.actor).toBe(run.actor);
  const query = new URLSearchParams({
    source_uid: run.source_uid,
    occurrence_key: run.occurrence_key,
  });
  const exact = await page.request.get(`/api/v1/instances?${query}`, {
    headers: { Authorization: `Bearer ${credentials.key}` },
  });
  expect((await exact.json()).items.map((row: { id: number }) => String(row.id))).toEqual([
    active.id,
  ]);
});

for (const [job, phase, attemptPhases] of [
  ['console-retry', 'Failed', ['Failed', 'Failed']],
  ['console-recovery', 'Succeeded', ['Failed', 'Succeeded']],
] as const)
  test(`Installed Console records both attempts for ${job}`, async ({ page }) => {
    await open(page);
    const active = await trigger(page, job);
    const run = await outcome(page, active.id, phase, 2);
    expect(run.attempts.map((attempt) => attempt.phase)).toEqual([...attemptPhases]);
    expect(Date.parse(run.attempts[1].started_at!)).toBeGreaterThanOrEqual(
      Date.parse(run.attempts[0].completed_at!),
    );
    await page.getByRole('button', { name: /^Attempt 1, Failed,/ }).click();
    await expect(page.getByRole('heading', { name: 'Attempt 1', exact: true })).toBeVisible();
    await expect(page.getByText('Timing not recorded', { exact: true })).toHaveCount(0);
  });

test('Installed cancellation waits for Pod deletion and retains the receipt across navigation', async ({
  page,
}) => {
  await open(page);
  const active = await trigger(page, 'console-cancel');
  await expect(page.locator('.run-meta').getByText('Running', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
  await page.getByRole('navigation').getByRole('link', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: `View run ${active.id}`, exact: true }).click();
  await expect(page.getByRole('button', { name: 'Cancel requested', exact: true })).toBeDisabled();
  const observations: { phase: string; pods: number; at: string }[] = [];
  await expect
    .poll(
      async () => {
        const response = await readRun(page, active.id);
        const run = await response.json();
        const pods = await cluster('pods', `orbitjob.io/job-run-uid=${active.runUID}`);
        observations.push({
          phase: run.phase,
          pods: pods.items.length,
          at: new Date().toISOString(),
        });
        if (run.phase === 'Canceled') expect(pods.items).toHaveLength(0);
        return run.phase;
      },
      { intervals: [250, 500, 1000] },
    )
    .toBe('Canceled');
  expect(observations.some((row) => row.phase === 'CancelRequested' && row.pods > 0)).toBe(true);
  await attachEvidence('cancel-observations.json', {
    body: JSON.stringify(observations, null, 2),
    contentType: 'application/json',
  });
  await outcome(page, active.id, 'Canceled', 1);
});

test('Installed API rejects a granted group key and hides another tenant’s Run', async ({
  page,
}) => {
  await open(page);
  const jobs = await page.request.get('/api/v1/jobs', {
    headers: { Authorization: `Bearer ${credentials.key}` },
  });
  const job = (await jobs.json()).items.find(
    (row: { name: string }) => row.name === 'console-success',
  );
  const runs = await page.request.get('/api/v1/instances', {
    headers: { Authorization: `Bearer ${credentials.key}` },
  });
  const run = (await runs.json()).items[0];
  for (const [path, method] of [
    ['/jobs', 'GET'],
    ['/instances', 'GET'],
    [`/jobs/${job.id}/trigger`, 'POST'],
    [`/instances/${run.id}/cancel`, 'POST'],
  ]) {
    const response = await page.request.fetch('/api/v1' + path, {
      method,
      headers: { Authorization: `Bearer ${credentials.groupKey}` },
    });
    expect(response.status()).toBe(403);
  }
  const foreign = await page.request.get(`/api/v1/instances/${run.id}`, {
    headers: { Authorization: `Bearer ${credentials.foreignKey}` },
  });
  expect(foreign.status()).toBe(404);
});

test('A stop remains unknown while held and confirms cancellation after trusted cleanup', async ({
  page,
}) => {
  test.setTimeout(420_000);
  await open(page);
  const active = await trigger(page, 'console-cancel');
  await expect(page.locator('.run-meta').getByText('Running', { exact: true })).toBeVisible();
  await expect
    .poll(async () => (await (await readRun(page, active.id)).json()).attempts[0].phase)
    .toBe('Running');
  const observed = (await (await readRun(page, active.id)).json()) as RunDetail;
  const hold = 'console.orbitjob.io/verify-stop';
  const target = holdCancellationJob(observed, hold);
  let unknownEvidence: ReturnType<typeof readTrustedCancellationEvidence> | undefined;
  try {
    await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
    const requestedAt = Date.now();
    await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
    await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
    await expect
      .poll(async () => (await (await readRun(page, active.id)).json()).phase, {
        timeout: 340_000,
        intervals: [2000],
      })
      .toBe('CancelUnknown');
    expect(Date.now() - requestedAt).toBeGreaterThanOrEqual(300_000);
    await expect(
      page.getByText('Observation paused. Refresh to continue.', { exact: true }),
    ).toBeVisible();
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await expect(
      page.locator('.run-meta').getByText('Stop unconfirmed', { exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText('This is not a confirmed cancellation.', { exact: false }),
    ).toBeVisible();
    await attachEvidence('unknown-stop.png', {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
    const run = (await (await readRun(page, active.id)).json()) as RunDetail;
    unknownEvidence = readTrustedCancellationEvidence(run, target);
    await attachEvidence('unknown-run.json', {
      body: JSON.stringify({ run, target, evidence: unknownEvidence }, null, 2),
      contentType: 'application/json',
    });
  } finally {
    if (unknownEvidence) {
      releaseCancellationHold(target);
    } else {
      test.info().annotations.push({
        type: 'unverified-stop-hold-retained',
        description: JSON.stringify(target),
      });
    }
  }
  await expectCancellationResourcesGone(target);
  await expect
    .poll(async () => (await (await readRun(page, active.id)).json()).phase)
    .toBe('Canceled');
  const canceled = (await (await readRun(page, active.id)).json()) as RunDetail;
  const confirmedEvidence = readTrustedCancellationEvidence(canceled, target);
  expect(confirmedEvidence.rawProof).toEqual(unknownEvidence.rawProof);
  if (unknownEvidence.attempt_phase === 'Failed') expect(canceled.attempts[0].phase).toBe('Failed');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await outcome(page, active.id, 'Canceled', 1);
  await attachEvidence('confirmed-stop.json', {
    body: JSON.stringify({ run: canceled, evidence: confirmedEvidence }, null, 2),
    contentType: 'application/json',
  });
});

test('Installed Console restores a password session, rejects invalid login and invalidates logout', async ({
  page,
}) => {
  await page.goto('/console/connection');
  await page.getByLabel('Username', { exact: true }).fill(credentials.username);
  await page.getByLabel('Password', { exact: true }).fill('invalid-password-not-a-real-secret');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Sign in failed.');
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
  await signInAccount(page, credentials.key);
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await expect(page.getByRole('link', { name: 'console-success', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('link', { name: 'console-success', exact: true })).toBeVisible();
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  expect((await (await page.request.get('/api/v1/auth/session')).json()).authenticated).toBe(false);
  await expect(page.getByLabel('API key', { exact: true })).toHaveCount(0);
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    credentials.password,
  );
});
