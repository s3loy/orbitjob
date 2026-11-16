import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const job = await demo.job('17');
  const recorded = await demo.run('1042');
  const workflow = await demo.workflow('412');
  const workflowRun = await demo.workflowRun('412', '94');
  demo.dispose();
  const run = {
    ...recorded,
    id: 77,
    phase: 'ExecutionUnknown',
    attempts: recorded.attempts.map((attempt) => ({
      ...attempt,
      run_id: 77,
      phase: 'ExecutionUnknown',
      completed_at: null,
      owner_run_uid: 'original-run-uid',
      execution_unknown_since: recorded.updated_at,
      execution_unknown_reason: 'JobNotFound',
    })),
  };
  const state = { triggers: 0, cancels: 0 };
  await page.route('**/openapi.json', (route) =>
    route.fulfill({
      json: {
        paths: {
          '/api/v1/instances': {
            get: {
              parameters: [
                { name: 'source_uid', in: 'query' },
                { name: 'occurrence_key', in: 'query' },
              ],
            },
          },
        },
      },
    }),
  );
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/jobs/17') return route.fulfill({ json: job });
    if (path === '/api/v1/jobs/17/trigger') {
      state.triggers++;
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'waiting-admission',
          occurrence_key: 'waiting-admission-occurrence',
          trigger: 'Manual',
          phase: 'Pending',
          created: true,
        },
      });
    }
    if (path === '/api/v1/instances/77/cancel') {
      state.cancels++;
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'original-run',
          occurrence_key: run.occurrence_key,
          phase: 'CancelRequested',
        },
      });
    }
    if (path === '/api/v1/instances/77') return route.fulfill({ json: run });
    if (path === '/api/v1/workflows/412/runs/94')
      return route.fulfill({ json: { ...workflowRun, phase: 'ExecutionUnknown' } });
    if (path === '/api/v1/workflows/412') return route.fulfill({ json: workflow });
    if (path === '/api/v1/instances/summary')
      return route.fulfill({ json: { total: 0, phases: {} } });
    return route.fulfill({ json: { items: [] } });
  });
  return { job, state };
}

async function connect(page: Page, path: string) {
  await page.goto(path);
  await signIn(page, 'execution_uncertainty_fixture');
}

test('unknown execution retains evidence and can request cancel without claiming a stop', async ({
  page,
}) => {
  const { state } = await fixture(page);
  await connect(page, '/console/runs/77');
  await expect(page.getByRole('heading', { name: 'Run #77', exact: true })).toBeVisible();
  await expect(page.getByText('Execution outcome unknown.', { exact: false })).toBeVisible();
  await expect(page.getByText('JobNotFound', { exact: false })).toBeVisible();
  await expect(page.locator('.run-meta .phase-label')).toHaveText('Outcome unknown');
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
  await expect.poll(() => state.cancels).toBe(1);
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
  await expect(page.locator('.run-meta .phase-label')).toHaveText('Outcome unknown');
  expect(state.triggers).toBe(0);
});

test('unknown workflow remains open with cancellation available', async ({ page }) => {
  await fixture(page);
  await connect(page, '/console/workflows/412/runs/94');
  await expect(page.getByRole('heading', { name: 'Workflow run #94', exact: true })).toBeVisible();
  await expect(page.getByText('Execution outcome unknown', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeEnabled();
});

test('accepted admission wait stays pending without creating another trigger', async ({ page }) => {
  const { job, state } = await fixture(page);
  await connect(page, '/console/jobs/17');
  await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const receipt = page.getByRole('region', { name: `Trigger receipt for ${job.name}` });
  await expect(receipt.getByText('Trigger accepted for', { exact: false })).toBeVisible();
  await expect(
    receipt.getByText('This request is accepted and may be waiting for another run to finish.', {
      exact: false,
    }),
  ).toBeVisible();
  await receipt.getByRole('button', { name: 'Refresh observation', exact: true }).click();
  await expect(receipt.getByRole('link', { name: 'View run', exact: true })).toHaveCount(0);
  expect(state.triggers).toBe(1);
  expect(state.cancels).toBe(0);
});
