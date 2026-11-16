import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';
import type { FunctionDefinition, FunctionRun, WorkflowRun } from '../src/api/m4';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });
const now = new Date('2026-10-25T12:00:00Z');

async function fixture(page: Page) {
  const demo = demoClient();
  const fn: FunctionDefinition = {
    ...(await demo.functionDefinition('27')),
    id: 99,
    name: 'readiness-check',
    version: 1,
    active_revision_version: 1,
    ready: true,
  };
  const workflow = await demo.workflow('412');
  const functionRun = {
    ...(await demo.functionRuns('27'))[0],
    function_id: 99,
    occurrence_key: 'reviewed-version',
  };
  const run = {
    ...(await demo.workflowRun('412', '94')),
    phase: 'Running',
    scheduled_for: '2026-10-25T12:00:00Z',
  };
  demo.dispose();
  const state = {
    fn,
    workflow,
    run,
    functionRun,
    functionHistory: [] as FunctionRun[],
    workflowHistory: [] as WorkflowRun[],
    denyHistoryOnce: false,
    historyGet: 200,
    functionGet: 200,
    functionsList: 200,
    workflowsList: 200,
    runGet: 200,
    preview: 200,
    reads: [] as string[],
    writes: [] as string[],
    versions: [] as string[],
    upcoming: ['2026-10-25T12:01:00Z', '2026-10-25T12:02:00Z'],
  };
  await page.clock.install({ time: now });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (request.method() === 'GET') state.reads.push(path);
    else state.writes.push(path);
    const response = (status: number, value: unknown) =>
      status === 200
        ? route.fulfill({ json: value })
        : route.fulfill({
            status,
            json: {
              error: {
                code:
                  status === 401 ? 'UNAUTHORIZED' : status === 403 ? 'FORBIDDEN' : 'UNAVAILABLE',
                message: `Controlled read ${status}`,
              },
            },
          });
    if (path === '/api/v1/functions' && request.method() === 'POST') {
      state.fn = {
        ...state.fn,
        ...request.postDataJSON(),
        ready: false,
        active_revision_version: null,
      };
      return route.fulfill({ status: 201, json: state.fn });
    }
    if (path === '/api/v1/functions') return response(state.functionsList, { items: [state.fn] });
    if (path === '/api/v1/functions/99') return response(state.functionGet, state.fn);
    if (path === '/api/v1/functions/99/runs') {
      if (state.denyHistoryOnce) {
        state.denyHistoryOnce = false;
        return response(403, {});
      }
      return response(state.historyGet, { items: state.functionHistory });
    }
    if (path === '/api/v1/functions/99/invoke') {
      state.versions.push(url.searchParams.get('expected_version') || '');
      return route.fulfill({
        json: {
          namespace: 'finance-prod',
          name: 'function-99',
          occurrence_key: 'reviewed-version',
          trigger: 'Function',
          phase: 'Pending',
          created: true,
        },
      });
    }
    if (path === '/api/v1/workflows')
      return response(state.workflowsList, { items: [state.workflow] });
    if (path === '/api/v1/workflows/412') return route.fulfill({ json: state.workflow });
    if (path === '/api/v1/workflows/412/runs') {
      if (request.method() === 'GET')
        return response(state.historyGet, { items: state.workflowHistory });
      return route.fulfill({
        json: {
          namespace: state.workflow.namespace,
          name: state.workflow.name,
          occurrence_key: state.run.occurrence_key,
          trigger: 'Manual',
          phase: 'Pending',
          created: true,
        },
      });
    }
    if (path === '/api/v1/workflows/412/runs/94') return response(state.runGet, state.run);
    if (path === '/api/v1/workflows/412/runs/94/cancel')
      return route.fulfill({
        json: {
          namespace: state.workflow.namespace,
          name: state.workflow.name,
          occurrence_key: state.run.occurrence_key,
          phase: 'CancelRequested',
        },
      });
    if (path === '/api/v1/workflows/schedule-preview')
      return response(state.preview, { time_zone: 'Asia/Shanghai', upcoming: state.upcoming });
    return route.fulfill({ json: { items: [] } });
  });
  return state;
}

async function connect(page: Page, path: string) {
  await page.goto(path);
  await signIn(page, 'otj_readiness_fixture_not_real');
}

test('List403 still allows a Function create, while Get403 stops readiness reads and retains the save', async ({
  page,
}) => {
  const state = await fixture(page);
  state.functionsList = 403;
  state.functionGet = 403;
  await connect(page, '/console/functions');
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await page.getByRole('link', { name: 'Create a function', exact: true }).click();
  await page.getByLabel('Function name').fill('readiness-check');
  await page.getByLabel('Container image').fill('busybox:1.37');
  await page.getByRole('button', { name: 'Review function', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Saved. Readiness unavailable', exact: true }),
  ).toBeVisible();
  const readiness = page.getByRole('region', { name: 'Function version readiness' });
  const versions = readiness.locator('details').filter({ hasText: 'Version details' });
  await expect(versions).not.toHaveAttribute('open');
  await versions.locator('summary').click();
  await expect(versions.getByText('Saved version', { exact: true })).toBeVisible();
  await expect(versions).toContainText('Unavailable');
  const reads = state.reads.filter((path) => path === '/api/v1/functions/99').length;
  await page.clock.fastForward(15000);
  expect(state.reads.filter((path) => path === '/api/v1/functions/99')).toHaveLength(reads);
  expect(state.writes).toEqual(['/api/v1/functions']);
});

test('a failed readiness refresh cannot present an old ready version as current', async ({
  page,
}) => {
  const state = await fixture(page);
  await connect(page, '/console/functions/99/edit');
  await page.getByRole('button', { name: 'Review function', exact: true }).click();
  await page.route(/\/api\/v1\/functions\/99(?:\?.*)?$/, async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback();
    state.fn = {
      ...state.fn,
      ...route.request().postDataJSON(),
      version: 2,
      active_revision_version: 2,
    };
    return route.fulfill({ json: state.fn });
  });
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Saved version is ready', exact: true }),
  ).toBeVisible();
  state.functionGet = 403;
  await page.getByRole('button', { name: 'Check readiness again', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Saved. Readiness unavailable', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Invoke readiness-check', exact: true }),
  ).toHaveCount(0);
  const versions = page
    .getByRole('region', { name: 'Function version readiness' })
    .locator('details')
    .filter({ hasText: 'Version details' });
  await versions.locator('summary').click();
  await expect(versions.getByText('Last checked (UTC)', { exact: true })).toBeVisible();
  await expect(versions).toContainText('Unavailable');
  state.functionGet = 200;
  await page.getByRole('button', { name: 'Check readiness again', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Invoke readiness-check', exact: true }),
  ).toBeEnabled();
});

test('Function details explain preparation, observe the reviewed version and invoke it exactly', async ({
  page,
}) => {
  const state = await fixture(page);
  state.fn.ready = false;
  await connect(page, '/console/functions/99');
  await expect(page.getByRole('region', { name: 'Function execution readiness' })).toContainText(
    'Preparing this version',
  );
  await expect(
    page.getByRole('button', { name: 'Invoke readiness-check', exact: true }),
  ).toBeDisabled();
  state.fn.ready = true;
  await page.clock.runFor(2100);
  await expect(page.getByRole('region', { name: 'Function execution readiness' })).toContainText(
    'Ready to invoke',
  );
  await page.getByRole('button', { name: 'Invoke readiness-check', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm invoke', exact: true }).click();
  await expect.poll(() => state.versions).toEqual(['1']);
});

test('Function detail readiness observation stops at its time boundary and offers refresh', async ({
  page,
}) => {
  const state = await fixture(page);
  state.fn.ready = false;
  await connect(page, '/console/functions/99');
  await expect(page.getByRole('region', { name: 'Function execution readiness' })).toContainText(
    'Preparing this version',
  );
  await page.clock.fastForward(120001);
  await expect(page.getByRole('region', { name: 'Function execution readiness' })).toContainText(
    'Readiness check paused',
  );
  const reads = state.reads.length;
  await page.clock.fastForward(10000);
  expect(state.reads).toHaveLength(reads);
  state.fn.ready = true;
  await page.getByRole('button', { name: 'Refresh definition', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Invoke readiness-check', exact: true }),
  ).toBeEnabled();
});

for (const collection of ['functions', 'workflows'] as const)
  test(`${collection} 401 hides cached counts and creation actions across later pages`, async ({
    page,
  }) => {
    const state = await fixture(page);
    const title = collection === 'functions' ? 'Functions' : 'Workflows';
    const other = collection === 'functions' ? 'Workflows' : 'Functions';
    const create = collection === 'functions' ? 'Create a function' : 'Create a workflow';
    await connect(page, `/console/${collection}`);
    await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible();
    await expect(page.getByText('Rows 1–1', { exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: create, exact: true })).toBeVisible();
    if (collection === 'functions') state.functionsList = 401;
    else state.workflowsList = 401;
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('Your session has ended.');
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(page.getByRole('button', { name: create, exact: true })).toHaveCount(0);
    await expect(page.getByRole('link', { name: create, exact: true })).toHaveCount(0);
    await expect(page.locator('.paging')).toHaveCount(0);
    const reads = state.reads.length;
    await page.goto(`/console/${other.toLowerCase()}`);
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    expect(state.reads).toHaveLength(reads);
  });

for (const status of [401, 403, 404, 503])
  test(`Workflow main-run read ${status} never leaves an actionable cached execution`, async ({
    page,
  }) => {
    const state = await fixture(page);
    await connect(page, '/console/workflows/412/runs/94');
    await expect(page.getByRole('region', { name: 'Pinned workflow history' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeEnabled();
    await page.getByText('Run identity and observation', { exact: true }).click();
    await expect(page.getByText('Planned trigger (UTC)', { exact: false })).toBeVisible();
    state.runGet = status;
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    if (status === 401) {
      await expect(
        page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
      ).toBeVisible();
      await expect(page.getByRole('region', { name: 'Pinned workflow history' })).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toHaveCount(
        0,
      );
      const reads = state.reads.length;
      await page.clock.fastForward(10000);
      expect(state.reads).toHaveLength(reads);
      expect(state.writes).toEqual([]);
      return;
    }
    await expect(page.getByRole('alert')).toContainText(`Controlled read ${status}`);
    if (status === 503) {
      await expect(
        page.getByText('Last observed workflow execution', { exact: true }),
      ).toBeVisible();
      await expect(page.getByRole('region', { name: 'Pinned workflow history' })).toBeVisible();
      await expect(
        page.getByRole('button', { name: 'Request cancel', exact: true }),
      ).toBeDisabled();
    } else {
      await expect(page.getByRole('region', { name: 'Pinned workflow history' })).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toHaveCount(
        0,
      );
    }
    const reads = state.reads.length;
    await page.clock.fastForward(10000);
    expect(state.reads).toHaveLength(reads);
    expect(state.writes).toEqual([]);
  });

test('Workflow previews expire at their first time boundary and a failed recalculation clears prior times', async ({
  page,
}) => {
  const state = await fixture(page);
  await connect(page, '/console/manifests/workflow');
  await page.getByRole('combobox', { name: 'Trigger mode', exact: true }).click();
  await page.getByRole('option', { name: 'Scheduled and manual', exact: true }).click();
  await page.getByRole('textbox', { name: 'Configured schedule', exact: true }).fill('* * * * *');
  await page.getByRole('textbox', { name: 'Time zone', exact: true }).fill('Asia/Shanghai');
  const preview = page.getByRole('region', { name: 'Schedule preview', exact: true });
  await preview.getByRole('button', { name: 'Preview next starts', exact: true }).click();
  await expect(preview.locator('.schedule-times time')).toHaveCount(2);
  const details = preview.locator('details').filter({ hasText: 'Preview details' });
  await expect(details).not.toHaveAttribute('open');
  await details.locator('summary').click();
  await expect(details.getByText('Calculated (UTC)', { exact: false })).toBeVisible();
  await expect(preview).toContainText('Times shown in Asia/Shanghai');
  await page.clock.fastForward(60001);
  await expect(
    preview.getByRole('heading', { name: 'Schedule preview expired', exact: true }),
  ).toBeVisible();
  await expect(preview.locator('.schedule-times time')).toHaveCount(0);
  state.preview = 503;
  await preview.getByRole('button', { name: 'Preview next starts', exact: true }).click();
  await expect(preview.getByRole('alert')).toContainText('Controlled read 503');
  await expect(preview.locator('.schedule-times time')).toHaveCount(0);
  state.preview = 200;
  state.upcoming = ['2026-10-25T12:03:00Z'];
  await preview.getByRole('button', { name: 'Preview next starts', exact: true }).click();
  await expect(preview.locator('.schedule-times time')).toHaveCount(1);
  expect(state.writes).toEqual(Array(3).fill('/api/v1/workflows/schedule-preview'));
});

for (const kind of ['function', 'workflow'] as const)
  test(`a matching terminal ${kind} receipt refreshes its previously empty history once`, async ({
    page,
  }) => {
    const state = await fixture(page);
    const isFunction = kind === 'function';
    const historyPath = isFunction ? '/api/v1/functions/99/runs' : '/api/v1/workflows/412/runs';
    await connect(page, isFunction ? '/console/functions/99' : '/console/workflows/412?tab=runs');
    await expect(
      page.getByText(isFunction ? 'No recorded invocations' : 'No recorded workflow runs', {
        exact: true,
      }),
    ).toBeVisible();
    await page
      .getByRole('button', {
        name: isFunction ? 'Invoke readiness-check' : `Trigger ${state.workflow.name}`,
        exact: true,
      })
      .click();
    await page
      .getByRole('button', { name: isFunction ? 'Confirm invoke' : 'Confirm trigger', exact: true })
      .click();
    await expect(page.getByRole('region', { name: 'Observed execution' })).toContainText(
      'Waiting for the execution record',
    );
    if (isFunction) state.functionHistory = [state.functionRun];
    else state.workflowHistory = [{ ...state.run, phase: 'Succeeded' }];
    await page.clock.runFor(2100);
    await expect(page.getByRole('region', { name: 'Observed execution' })).toContainText(
      'Succeeded',
    );
    const executionPath = isFunction
      ? `/console/functions/99/runs/${state.functionRun.run_id}`
      : '/console/workflows/412/runs/94';
    await expect(page.locator('.m4-records a.record-link')).toHaveAttribute('href', executionPath);
    const historyReads = state.reads.filter((path) => path === historyPath).length;
    await page.clock.fastForward(10000);
    expect(state.reads.filter((path) => path === historyPath)).toHaveLength(historyReads);
    expect(state.writes).toEqual([
      isFunction ? '/api/v1/functions/99/invoke' : '/api/v1/workflows/412/runs',
    ]);
  });

test('terminal receipt observation does not automatically retry an already denied history page', async ({
  page,
}) => {
  const state = await fixture(page);
  state.denyHistoryOnce = true;
  await connect(page, '/console/functions/99');
  const history = page.getByRole('tabpanel', { name: 'Invocations', exact: true });
  await expect(history.getByRole('alert')).toContainText('Permission required');
  state.functionHistory = [state.functionRun];
  await page.getByRole('button', { name: 'Invoke readiness-check', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm invoke', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Observed execution' })).toContainText('Succeeded');
  await expect(history.getByRole('alert')).toContainText('Permission required');
  await page.clock.fastForward(10000);
  expect(state.reads.filter((path) => path === '/api/v1/functions/99/runs')).toHaveLength(2);
  expect(state.writes).toEqual(['/api/v1/functions/99/invoke']);
});

test('a Workflow cancellation receipt remains visible when the next execution read is denied', async ({
  page,
}) => {
  const state = await fixture(page);
  await connect(page, '/console/workflows/412/runs/94');
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  state.runGet = 403;
  await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Controlled read 403');
  await expect(
    page.getByText('Cancel request accepted. The observed workflow phase determines the outcome.', {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByRole('region', { name: 'Pinned workflow history' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toHaveCount(0);
  const reads = state.reads.length;
  await page.clock.fastForward(10000);
  expect(state.reads).toHaveLength(reads);
  expect(state.writes).toEqual(['/api/v1/workflows/412/runs/94/cancel']);
});

for (const status of [403, 404, 503])
  test(`a receipt observation read ${status} preserves acceptance with honest observed-state display`, async ({
    page,
  }) => {
    const state = await fixture(page);
    state.workflowHistory = [state.run];
    await connect(page, '/console/workflows/412?tab=runs');
    await page.getByRole('button', { name: `Trigger ${state.workflow.name}`, exact: true }).click();
    await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
    const receipt = page.getByRole('region', {
      name: `Workflow trigger receipt for ${state.workflow.name}`,
      exact: true,
    });
    const observed = receipt.getByRole('region', { name: 'Observed execution', exact: true });
    await expect(
      observed.getByRole('link', { name: 'Open this execution', exact: true }),
    ).toBeVisible();
    state.historyGet = status;
    await page.clock.runFor(2100);
    await expect(observed.getByRole('alert')).toContainText(`Controlled read ${status}`);
    await expect(receipt.locator('summary').first()).toContainText('Workflow trigger accepted');
    if (status === 503) {
      await expect(observed).toContainText('previously read snapshot is not confirmed current');
      await expect(
        observed.getByRole('link', { name: 'Open this execution', exact: true }),
      ).toBeVisible();
    } else {
      await expect(
        observed.getByRole('link', { name: 'Open this execution', exact: true }),
      ).toHaveCount(0);
      await expect(observed.locator('.phase-label')).toHaveCount(0);
      await expect(observed).toContainText(
        'The request is still accepted, but its outcome is not confirmed.',
      );
    }
    const reads = state.reads.length;
    await page.clock.fastForward(10000);
    expect(state.reads).toHaveLength(reads);
    expect(state.writes).toEqual(['/api/v1/workflows/412/runs']);
  });
