import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const job = await demo.job('17');
  const recorded = await demo.run('1042');
  demo.dispose();
  const run = {
    ...recorded,
    id: 77,
    occurrence_key: 'retained-trigger-occurrence',
    phase: 'Running',
    attempts: recorded.attempts.map((attempt) => ({ ...attempt, run_id: 77 })),
  };
  const state = {
    runFailure: 0,
    afterCancelFailure: 0,
    logFailure: 0,
    reads: 0,
    triggers: 0,
    cancels: 0,
  };
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
          name: 'retained-trigger',
          occurrence_key: run.occurrence_key,
          trigger: 'Manual',
          phase: 'Pending',
          created: true,
        },
      });
    }
    if (path === '/api/v1/instances/77/cancel') {
      state.cancels++;
      state.runFailure = state.afterCancelFailure;
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'retained-trigger',
          occurrence_key: run.occurrence_key,
          phase: 'CancelRequested',
        },
      });
    }
    if (path === '/api/v1/instances/77') {
      state.reads++;
      if (state.runFailure === -1) return route.abort('connectionreset');
      return state.runFailure
        ? route.fulfill({
            status: state.runFailure,
            json: { error: { code: 'READ_FAILED', message: 'Run read no longer allowed' } },
          })
        : route.fulfill({ json: run });
    }
    if (path.endsWith('/logs'))
      return state.logFailure
        ? route.fulfill({
            status: state.logFailure,
            json: { error: { code: 'LOG_READ_FAILED', message: 'Log read no longer allowed' } },
          })
        : route.fulfill({
            json: {
              state: 'ready',
              text: 'previously-authorized-log-text',
              truncated: false,
              tail_lines: 100,
              limit_bytes: 65536,
              attempt: 2,
              container: 'task',
              reason: '',
            },
          });
    if (path === '/api/v1/instances/summary')
      return route.fulfill({ json: { total: 1, phases: { Running: 1 } } });
    return route.fulfill({
      json: { items: path === '/api/v1/instances' ? [run] : [] },
    });
  });
  return { state, job };
}

async function connect(page: Page, path = '/console/runs/77') {
  await page.goto(path);
  await signIn(page, 'run_read_recovery_fixture');
}

for (const status of [401, 403, 404])
  test(`cached run and logs disappear after a ${status} read while accepted receipts remain`, async ({
    page,
  }) => {
    const { state, job } = await fixture(page);
    await connect(page, '/console/jobs/17');
    await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
    await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
    const receipt = page.getByRole('region', { name: `Trigger receipt for ${job.name}` });
    await receipt.getByRole('link', { name: 'View run', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Run #77', exact: true })).toBeVisible();
    await page.getByText('Container logs', { exact: true }).click();
    await page.getByRole('button', { name: 'Read logs', exact: true }).click();
    await expect(page.getByLabel('Container log snapshot')).toHaveValue(
      'previously-authorized-log-text',
    );
    state.afterCancelFailure = status;
    await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
    if (status === 401) {
      await expect(
        page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
      ).toBeVisible();
      await expect(receipt).toHaveCount(0);
      await expect(page.getByLabel('Container log snapshot')).toHaveCount(0);
      expect(state.triggers).toBe(1);
      expect(state.cancels).toBe(1);
      return;
    }
    await expect(
      page.getByRole('heading', {
        name: status === 404 ? 'Run no longer available' : 'Run unavailable',
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Run #77', exact: true })).toHaveCount(0);
    await expect(page.locator('.run-meta')).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Attempt timeline' })).toHaveCount(0);
    await expect(page.getByLabel('Container log snapshot')).toHaveCount(0);
    await expect(page.getByRole('button', { name: /Request cancel|Cancel requested/ })).toHaveCount(
      0,
    );
    await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
    const activity = page.locator('details.operation-activity');
    await activity.locator(':scope > summary').click();
    await expect(
      receipt.getByText(`Trigger accepted for ${job.name}`, { exact: true }),
    ).toBeVisible();
    expect(state.triggers).toBe(1);
    expect(state.cancels).toBe(1);

    if (status === 404) {
      state.runFailure = 0;
      await page.getByRole('button', { name: 'Try again', exact: true }).click();
      await expect(page.getByRole('heading', { name: 'Run #77', exact: true })).toBeVisible();
      await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
    }
  });

for (const status of [403, 404])
  test(`a separately denied ${status} log refresh hides the old snapshot without hiding the run`, async ({
    page,
  }) => {
    const { state } = await fixture(page);
    await connect(page);
    await page.getByText('Container logs', { exact: true }).click();
    await page.getByRole('button', { name: 'Read logs', exact: true }).click();
    await expect(page.getByLabel('Container log snapshot')).toHaveValue(
      'previously-authorized-log-text',
    );
    state.logFailure = status;
    await page.getByRole('button', { name: 'Read logs', exact: true }).click();
    await expect(
      page.locator('details[aria-label="Attempt logs"]').getByRole('alert'),
    ).toContainText('Log read no longer allowed');
    await expect(page.getByLabel('Container log snapshot')).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Run #77', exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeEnabled();
    state.logFailure = 0;
    await page.getByRole('button', { name: 'Read logs', exact: true }).click();
    await expect(page.getByLabel('Container log snapshot')).toHaveValue(
      'previously-authorized-log-text',
    );
  });

test('a log 401 hides a previously loaded run and snapshot for the rejected connection', async ({
  page,
}) => {
  const { state } = await fixture(page);
  await connect(page);
  await page.getByText('Container logs', { exact: true }).click();
  await page.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(page.getByLabel('Container log snapshot')).toHaveValue(
    'previously-authorized-log-text',
  );
  state.logFailure = 401;
  await page.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel('Container log snapshot')).toHaveCount(0);
  await expect(page.locator('.run-meta')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toHaveCount(0);
});

for (const failure of [-1, 503])
  test(`a ${failure === -1 ? 'network' : '503'} failure labels a retained snapshot and blocks cancellation until a successful refresh`, async ({
    page,
  }) => {
    const { state } = await fixture(page);
    await connect(page);
    await page.getByText('Container logs', { exact: true }).click();
    await page.getByRole('button', { name: 'Read logs', exact: true }).click();
    await expect(page.getByLabel('Container log snapshot')).toHaveValue(
      'previously-authorized-log-text',
    );
    await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
    state.runFailure = failure;
    await expect(page.getByText('Last observed snapshot.', { exact: false })).toBeVisible();
    await expect(
      page.getByRole('button', { name: 'Confirm cancel request', exact: true }),
    ).toBeDisabled();
    await page.getByRole('button', { name: 'Keep observing', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Run #77', exact: true })).toBeVisible();
    const identity = page.locator('details.run-identity');
    await identity.locator(':scope > summary').click();
    await expect(identity.locator('dt', { hasText: /^Last observed$/ })).toBeVisible();
    await expect(
      identity.locator('dt', { hasText: /^Last observed$/ }).locator('+ dd'),
    ).not.toHaveText('Not recorded');
    await expect(page.getByLabel('Container log snapshot')).toHaveValue(
      'previously-authorized-log-text',
    );
    await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeDisabled();
    state.runFailure = 0;
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await expect(page.getByText('Last observed snapshot.', { exact: false })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeEnabled();
    expect(state.cancels).toBe(0);
  });
