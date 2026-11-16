import { signInAccount } from './account-support';
import { test, expect } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import type { Workflow, WorkflowRun, WorkflowRunDetail } from '../src/api/m4';
import type { RunDetail } from '../src/api/contracts';
import {
  expectCancellationResourcesGone,
  holdCancellationJob,
  readTrustedCancellationEvidence,
  releaseCancellationHold,
} from './cancellation-evidence';
const c = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));
const headers = { Authorization: `Bearer ${c.key}` };
test('native Workflow confirms cancellation after its timed-out stop has trusted cleanup', async ({
  page,
}) => {
  test.setTimeout(440_000);
  expect(process.env.KUBECONFIG).toBeTruthy();
  expect(execFileSync('kubectl', ['config', 'current-context'], { encoding: 'utf8' }).trim()).toBe(
    'kind-orbitjob-console-e2e',
  );
  async function get<T>(path: string): Promise<T> {
    const r = await page.request.get(`/api/v1/${path}`, { headers });
    expect(r.status()).toBe(200);
    return r.json();
  }
  await page.goto('/console/connection');
  await signInAccount(page, c.key);
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Workflows', exact: true })
    .click();
  const definition = (await get<{ items: Workflow[] }>('workflows?limit=100')).items.find(
    (w) => w.name === 'console-m4-cancel',
  )!;
  await page.getByRole('link', { name: definition.name, exact: true }).click();
  await page.getByRole('button', { name: `Trigger ${definition.name}` }).click();
  const trigger = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname.endsWith(`/workflows/${definition.id}/runs`) &&
      r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Confirm trigger' }).click();
  const response = await trigger;
  expect(response.status()).toBe(201);
  const receipt = await response.json();
  let run!: WorkflowRun;
  let detail!: WorkflowRunDetail;
  await expect
    .poll(
      async () => {
        run = (await get<{ items: WorkflowRun[] }>(`workflows/${definition.id}/runs`)).items.find(
          (r) => r.occurrence_key === receipt.occurrence_key,
        )!;
        if (!run) return '';
        detail = await get<WorkflowRunDetail>(`workflows/${definition.id}/runs/${run.id}`);
        return detail.steps[0]?.phase;
      },
      { timeout: 60_000, intervals: [1000] },
    )
    .toBe('Running');
  const step = await get<RunDetail>(`instances/${detail.steps[0].id}`);
  const hold = `console.orbitjob.io/m4-stop-${run.id}`;
  const target = holdCancellationJob(step, hold);
  let unknownEvidence: ReturnType<typeof readTrustedCancellationEvidence> | undefined;
  try {
    await page.getByRole('tab', { name: 'Runs', exact: true }).click();
    await page.getByRole('button', { name: 'Refresh', exact: true }).last().click();
    await page.getByRole('link', { name: `#${run.id}`, exact: true }).click();
    await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
    const requestedAt = Date.now();
    await page.getByRole('button', { name: 'Confirm cancel request' }).click();
    await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
    await expect
      .poll(
        async () =>
          (await get<WorkflowRunDetail>(`workflows/${definition.id}/runs/${run.id}`)).phase,
        { timeout: 340_000, intervals: [2000] },
      )
      .toBe('CancelUnknown');
    expect(Date.now() - requestedAt).toBeGreaterThanOrEqual(300_000);
    await expect(page.getByText('Observation paused', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await expect(
      page.locator('.run-meta').getByText('Stop unconfirmed', { exact: true }),
    ).toBeVisible();
    await expect(page.getByText('A stop has not been confirmed.', { exact: false })).toBeVisible();
    const path = new URL(page.url()).pathname;
    await page.reload();
    await signInAccount(page, c.key);
    await expect(page).toHaveURL(new RegExp(path + '$'));
    await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeDisabled();
    const unknown = await get<WorkflowRunDetail>(`workflows/${definition.id}/runs/${run.id}`);
    expect(unknown.steps).toHaveLength(1);
    expect(String(unknown.steps[0].id)).toBe(target.runID);
    expect(unknown.steps[0].phase).toBe('CancelUnknown');
    unknownEvidence = readTrustedCancellationEvidence(
      await get<RunDetail>(`instances/${target.runID}`),
      target,
    );
    writeFileSync(
      join(dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!), 'unknown-evidence.json'),
      JSON.stringify({ receipt, workflow: unknown, target, evidence: unknownEvidence }, null, 2),
    );
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
    .poll(async () => (await get<RunDetail>(`instances/${target.runID}`)).phase)
    .toBe('Canceled');
  await expect
    .poll(
      async () => (await get<WorkflowRunDetail>(`workflows/${definition.id}/runs/${run.id}`)).phase,
    )
    .toBe('Canceled');
  const canceled = await get<WorkflowRunDetail>(`workflows/${definition.id}/runs/${run.id}`);
  expect(canceled.steps).toHaveLength(1);
  expect(String(canceled.steps[0].id)).toBe(target.runID);
  expect(canceled.steps[0].phase).toBe('Canceled');
  const confirmedEvidence = readTrustedCancellationEvidence(
    await get<RunDetail>(`instances/${target.runID}`),
    target,
  );
  expect(confirmedEvidence.rawProof).toEqual(unknownEvidence.rawProof);
  if (unknownEvidence.attempt_phase === 'Failed')
    expect(confirmedEvidence.attempt_phase).toBe('Failed');
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.locator('.run-meta')).toContainText('Canceled');
  writeFileSync(
    join(dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!), 'confirmed-stop-evidence.json'),
    JSON.stringify({ receipt, workflow: canceled, target, evidence: confirmedEvidence }, null, 2),
  );
});
