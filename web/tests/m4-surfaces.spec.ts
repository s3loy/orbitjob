import { signIn } from './support';
import { ulid } from 'ulid';
import { expect, test, type Page } from './support';
import type {
  FunctionDefinition,
  FunctionRun,
  WorkflowDetail,
  WorkflowRunDetail,
} from '../src/api/m4';

const timestamp = '2026-10-19T01:20:30Z';
const workflow: WorkflowDetail = {
  time_zone: 'UTC',
  concurrency_policy: 'Forbid',
  id: 412,
  name: 'billing-close',
  namespace: 'orbitjob',
  source_uid: 'workflow-source',
  generation: 4,
  schedule: '0 2 * * *',
  suspend: false,
  fail_policy: 'fail_fast',
  timeout_seconds: 900,
  task_count: 4,
  created_at: timestamp,
  tasks: [
    { name: 'extract', job_ref: 'billing-extract', depends_on: [] },
    { name: 'validate', job_ref: 'billing-validate', depends_on: ['extract'] },
    { name: 'reconcile', job_ref: 'billing-reconcile', depends_on: ['extract'] },
    {
      name: 'publish',
      job_ref: 'billing-publish',
      depends_on: ['validate', 'reconcile'],
      condition: {
        type: 'any',
        rules: [
          { task: 'validate', field: 'attempts', operator: 'ge', number: 0 },
          { task: 'reconcile', field: 'duration_ms', operator: 'lt', number: 30000 },
        ],
      },
    },
  ],
};
const workflowRun: WorkflowRunDetail = {
  id: 94,
  workflow_source_uid: 'workflow-source',
  revision_id: 398,
  occurrence_key: 'recorded-workflow-key',
  trigger: 'manual',
  actor: 'operator',
  phase: 'Failed',
  created_at: timestamp,
  updated_at: timestamp,
  steps: [
    {
      id: 1032,
      source_uid: 'shared-source',
      occurrence_key: 'step-key',
      phase: 'Failed',
      attempt: 2,
      created_at: timestamp,
      updated_at: timestamp,
    },
  ],
};
const fn: FunctionDefinition = {
  ready: true,
  id: 27,
  tenant_id: ulid(),
  resource_group_id: ulid(),
  name: 'rebuild-search-index',
  description: 'Rebuild the search index.',
  status: 'active',
  image: 'registry.example/indexer:stable',
  command: ['/app/indexer'],
  args: ['--catalog=default'],
  timeout_seconds: 120,
  retry_limit: 2,
  history_success: 20,
  history_failed: 50,
  labels: {},
  version: 3,
  created_at: timestamp,
  updated_at: timestamp,
};
const outcome: FunctionRun = {
  id: 701,
  run_id: '026c15a0-b08f-4060-9858-c6e0724c9ac5',
  function_id: 27,
  status: 'canceled',
  triggered_at: timestamp,
  started_at: null,
  finished_at: timestamp,
  duration_ms: null,
  attempts: null,
  last_attempt: null,
  version: 1,
  created_at: timestamp,
};

async function live(
  page: Page,
  options: {
    historyDenied?: boolean;
    workflowDenied?: boolean;
    missing?: boolean;
    running?: boolean;
    definitionDenied?: boolean;
  } = {},
) {
  const seen: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    const path = url.pathname;
    seen.push(path + url.search);
    const deny = (status: number, message: string) =>
      route.fulfill({
        status,
        json: { error: { code: status === 403 ? 'FORBIDDEN' : 'NOT_FOUND', message } },
      });
    if (options.workflowDenied && path.startsWith('/api/v1/workflows'))
      return deny(403, 'Workflow access denied');
    if (path === '/api/v1/workflows') return route.fulfill({ json: { items: [workflow] } });
    if (path === '/api/v1/workflows/412')
      return options.definitionDenied
        ? deny(403, 'Definition requires workflow:Get')
        : options.missing
          ? deny(404, 'Revision not found')
          : route.fulfill({ json: workflow });
    if (path === '/api/v1/workflows/412/runs')
      return route.fulfill({ json: { items: [workflowRun] } });
    if (path === '/api/v1/workflows/412/runs/94')
      return route.fulfill({
        json: options.running ? { ...workflowRun, phase: 'Running' } : workflowRun,
      });
    if (path === '/api/v1/instances/1032') return deny(403, 'This step requires instance:Get');
    if (path === '/api/v1/functions') return route.fulfill({ json: { items: [fn] } });
    if (path === '/api/v1/functions/27')
      return options.definitionDenied
        ? deny(403, 'Definition requires function:Get')
        : route.fulfill({ json: fn });
    if (path === '/api/v1/functions/27/runs')
      return options.historyDenied
        ? deny(403, 'Function history denied')
        : route.fulfill({ json: { items: [outcome] } });
    if (path === `/api/v1/functions/27/runs/${outcome.run_id}`)
      return route.fulfill({ json: outcome });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/');
  await signIn(page, 'otj_m4_fixture_not_a_real_key');
  return seen;
}
async function nav(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

test('Current workflow tasks retain numeric zero and any-rule meaning, without execution coloring', async ({
  page,
}) => {
  await live(page);
  await nav(page, 'Workflows');
  await page.getByLabel('Search workflows').fill('billing');
  await page.getByRole('link', { name: 'billing-close', exact: true }).click();
  await expect(
    page.getByText('Missed automatic starts are skipped.', { exact: false }),
  ).toBeVisible();
  await page.getByRole('button', { name: /publish billing-publish/ }).click();
  const selected = page.getByRole('region', { name: 'Selected task' });
  await expect(selected.getByText('validate.attempts >= 0', { exact: true })).toBeVisible();
  await expect(selected.getByText('At least one rule must pass', { exact: true })).toBeVisible();
  await expect(
    page.getByRole('group', { name: 'Current definition tasks' }).locator('.phase-label'),
  ).toHaveCount(0);
  await page.screenshot({ path: 'test-results/m4-current-definition-desktop.png', fullPage: true });
  await page.getByRole('link', { name: 'Back to Workflows', exact: true }).click();
  await expect(page.getByLabel('Search workflows')).toHaveValue('billing');
});

test('Historical workflow steps preserve pinned revision and recover from independent step denial', async ({
  page,
}) => {
  await live(page);
  await nav(page, 'Workflows');
  await page.getByRole('link', { name: 'billing-close', exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: '#94', exact: true }).click();
  await expect(page.getByText('This run uses a different revision', { exact: true })).toBeVisible();
  await expect(
    page.getByText('This run is pinned to revision #398. The current definition is revision', {
      exact: false,
    }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: '#412', exact: true })).toHaveAttribute(
    'href',
    '/console/workflows/412',
  );
  await expect(page.getByRole('group', { name: 'Current definition tasks' })).toHaveCount(0);
  await expect(
    page.getByText('A saved task graph is unavailable for this run.', { exact: false }),
  ).toBeVisible();
  await page.screenshot({ path: 'test-results/m4-historical-run-desktop.png', fullPage: true });
  await page.getByRole('link', { name: '#1032', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await page.getByRole('link', { name: 'Back to Workflow run', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Workflow run #94', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Workflow runs', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Runs', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('link', { name: '#94', exact: true }).click();
  await page.getByText('Run identity and observation', { exact: true }).click();
  await page.getByRole('link', { name: 'Revision #412', exact: true }).click();
  await page.getByRole('link', { name: 'Back to Workflow run', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Workflow run #94', exact: true })).toBeVisible();
});

for (const kind of ['function', 'workflow'] as const) {
  test(`${kind} List history remains usable when its independent definition Get is denied`, async ({
    page,
  }) => {
    const seen = await live(page, { definitionDenied: true });
    await nav(page, kind === 'function' ? 'Functions' : 'Workflows');
    await page
      .getByRole('link', { name: kind === 'function' ? fn.name : workflow.name, exact: true })
      .click();
    if (kind === 'workflow') await page.getByRole('tab', { name: 'Runs', exact: true }).click();
    await expect(
      kind === 'function'
        ? page.getByTitle(outcome.run_id, { exact: true })
        : page.getByRole('link', { name: '#94', exact: true }),
    ).toBeVisible();
    expect(
      seen.some((p) =>
        p.startsWith(
          kind === 'function' ? '/api/v1/functions/27/runs?' : '/api/v1/workflows/412/runs?',
        ),
      ),
    ).toBe(true);
    await expect(
      page.getByRole('button', {
        name: kind === 'function' ? `Invoke ${fn.name}` : `Trigger ${workflow.name}`,
        exact: true,
      }),
    ).toHaveCount(0);
    await page
      .getByRole('tab', { name: kind === 'function' ? 'Configuration' : 'Definition', exact: true })
      .click();
    await expect(
      page.getByRole('heading', {
        name: kind === 'function' ? 'Configuration unavailable' : 'Definition unavailable',
        exact: true,
      }),
    ).toBeVisible();
  });
}

test('Function history uses UUID detail routing, terminal null timing and limit-only loading', async ({
  page,
}) => {
  const seen = await live(page);
  await nav(page, 'Functions');
  await page.getByRole('link', { name: 'rebuild-search-index', exact: true }).click();
  await page.getByRole('combobox', { name: 'History limit', exact: true }).click();
  await page.getByRole('option', { name: '50', exact: true }).click();
  await expect
    .poll(() => seen.filter((p) => p.includes('/functions/27/runs?limit=50')).length)
    .toBe(1);
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toHaveCount(0);
  const invocation = page.getByTitle(outcome.run_id, { exact: true });
  await expect(invocation).toHaveAttribute('href', `/console/functions/27/runs/${outcome.run_id}`);
  await invocation.click();
  await expect(page).toHaveURL(new RegExp(`/functions/27/runs/${outcome.run_id}$`));
  await page.getByText('Invocation identity and recorded fields', { exact: true }).click();
  await expect(page.locator('.record-json')).toContainText(`"run_id": "${outcome.run_id}"`);
  await expect(
    page
      .locator('.fact-pair')
      .filter({ has: page.locator('dt', { hasText: /^Latest attempt started \(UTC\)$/ }) }),
  ).toHaveText('Latest attempt started (UTC)Not recorded');
  await expect(
    page
      .locator('.fact-pair')
      .filter({ has: page.locator('dt', { hasText: /^Latest attempt duration$/ }) }),
  ).toHaveText('Latest attempt durationNot recorded');
  expect(seen.filter((p) => p.startsWith('/api/v1/instances'))).toEqual([]);
  await page.getByRole('link', { name: 'Back to Invocations', exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'History limit', exact: true })).toHaveText('50');
});

test('Function history denial leaves definition and configuration independently usable', async ({
  page,
}) => {
  await live(page, { historyDenied: true });
  await nav(page, 'Functions');
  await page.getByRole('link', { name: 'rebuild-search-index', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'rebuild-search-index', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('tabpanel', { name: 'Invocations', exact: true }).getByRole('alert'),
  ).toContainText('Function history denied');
  const controls = page.getByRole('region', { name: 'Execution controls', exact: true });
  await expect(controls.getByRole('alert')).toContainText(
    'The API response does not match the supported contract.',
  );
  await expect(controls.getByRole('switch')).toHaveCount(0);
  await expect(controls.getByRole('button', { name: 'Save controls', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByText(fn.image, { exact: true })).toBeVisible();
  await page.getByText('Version and history retention', { exact: true }).click();
  await expect(page.getByText('Successful invocations to retain', { exact: true })).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Invoke rebuild-search-index', exact: true }),
  ).toBeEnabled();
});

test('Workflow account denial explains permissions, and removed revisions return to collection', async ({
  page,
}) => {
  await live(page, { workflowDenied: true });
  await nav(page, 'Workflows');
  await expect(
    page.getByText(
      'Your account does not have access to this workflow. Ask a workspace administrator to review your permissions.',
      {
        exact: false,
      },
    ),
  ).toBeVisible();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await expect(page.getByRole('link', { name: 'Review connection', exact: true })).toHaveCount(0);
  await live(page, { missing: true });
  await nav(page, 'Workflows');
  await page.getByRole('link', { name: 'billing-close', exact: true }).click();
  await expect(
    page.getByText('The revision may no longer be active', { exact: false }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Return to Workflows', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Workflows', exact: true })).toBeVisible();
});

test('Workflow observation stops after its bounded window and manual refresh resumes it', async ({
  page,
}) => {
  await page.clock.install();
  const seen = await live(page, { running: true });
  await nav(page, 'Workflows');
  await page.getByRole('link', { name: 'billing-close', exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: '#94', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Workflow run #94', exact: true })).toBeVisible();
  const runRequests = () =>
    seen
      .map((path) => new URL(path, 'http://console.invalid'))
      .filter((url) => url.pathname === '/api/v1/workflows/412/runs/94');
  await expect.poll(() => runRequests().length).toBeGreaterThan(0);
  await page.clock.fastForward(120001);
  await expect(page.getByText('Observation paused', { exact: true })).toBeVisible();
  const count = runRequests().length;
  await page.clock.fastForward(60000);
  expect(runRequests()).toHaveLength(count);
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect.poll(() => runRequests().length).toBeGreaterThan(count);
  expect(runRequests().map((url) => url.searchParams.get('tenant_id'))).toEqual(
    Array(runRequests().length).fill('00000000000000000000000001'),
  );
  await expect(page.getByText('Observation paused', { exact: true })).toHaveCount(0);
});

test('History rate limiting gates refresh, invoke and limit changes until Retry-After ends', async ({
  page,
}) => {
  await page.clock.install();
  await live(page);
  await page.route('**/api/v1/functions/27/runs?limit=*', async (route) =>
    route.fulfill({
      status: 429,
      headers: { 'Retry-After': '3' },
      json: { error: { code: 'RATE_LIMITED', message: 'History rate limited' } },
    }),
  );
  await nav(page, 'Functions');
  await page.getByRole('link', { name: 'rebuild-search-index', exact: true }).click();
  await expect(
    page.getByRole('tabpanel', { name: 'Invocations', exact: true }).getByRole('alert'),
  ).toContainText('Too many requests');
  const controls = page.getByRole('region', { name: 'Execution controls', exact: true });
  await expect(controls.getByRole('alert')).toContainText(
    'The API response does not match the supported contract.',
  );
  await expect(controls.getByRole('switch')).toHaveCount(0);
  await expect(controls.getByRole('button', { name: 'Save controls', exact: true })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'History limit', exact: true })).toBeDisabled();
  await expect(
    page.getByRole('button', { name: 'Invoke rebuild-search-index', exact: true }),
  ).toBeDisabled();
  const refreshButtons = page.getByRole('button', { name: /^Refresh (definition|invocations)$/ });
  await expect(refreshButtons).toHaveCount(2);
  for (const button of await refreshButtons.all()) {
    await expect(button).toBeDisabled();
  }
  await page.clock.fastForward(3100);
  await expect(page.getByRole('combobox', { name: 'History limit', exact: true })).toBeEnabled();
  await expect(
    page.getByRole('button', { name: 'Invoke rebuild-search-index', exact: true }),
  ).toBeEnabled();
});

for (const [width, colorScheme] of [
  [320, 'dark'],
  [390, 'light'],
] as const) {
  test.describe(`${width}px ${colorScheme} M4 layout`, () => {
    test.use({ viewport: { width, height: 844 }, colorScheme, hasTouch: true, isMobile: true });
    test('keeps six navigation targets, task dependencies and outcome timing readable', async ({
      page,
    }) => {
      await live(page);
      const navigation = page.getByRole('navigation', { name: 'Main navigation' });
      for (const name of ['Jobs', 'Runs', 'Checks', 'SLOs', 'Workflows', 'Functions']) {
        const link = navigation.getByRole('link', { name, exact: true });
        await expect(link).toBeVisible();
        const box = await link.boundingBox();
        expect(box?.height).toBeGreaterThanOrEqual(44);
      }
      await nav(page, 'Workflows');
      await page.getByRole('link', { name: 'billing-close', exact: true }).click();
      for (const tab of await page.getByRole('tab').all()) {
        expect((await tab.boundingBox())?.height).toBeGreaterThanOrEqual(44);
      }
      await expect(
        page.getByRole('button', { name: /publish billing-publish After validate, reconcile/ }),
      ).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
      await page.screenshot({
        path: `test-results/m4-workflow-definition-${width}-${colorScheme}.png`,
        fullPage: true,
      });
      await nav(page, 'Functions');
      await page.getByRole('link', { name: 'rebuild-search-index', exact: true }).click();
      for (const tab of await page.getByRole('tab').all()) {
        expect((await tab.boundingBox())?.height).toBeGreaterThanOrEqual(44);
      }
      await expect(page.locator('[data-label="Duration"]')).toHaveText('Not recorded');
      const records = await page.locator('.m4-records table').boundingBox();
      expect(records?.height).toBeGreaterThan(120);
      await expect(page.getByTitle(outcome.run_id, { exact: true })).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
      await page.screenshot({
        path: `test-results/m4-function-history-${width}-${colorScheme}.png`,
        fullPage: true,
      });
    });
  });
}
