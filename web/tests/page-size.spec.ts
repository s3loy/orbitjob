import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page, path: string) {
  const demo = demoClient();
  const job = { ...(await demo.job('17')), source_kind: 'kubernetes' };
  const run = await demo.run('1042');
  const fn = await demo.functionDefinition('27');
  const workflow = await demo.workflow('412');
  const workflowRun = await demo.workflowRun('412', '94');
  const invocation = (await demo.functionRuns('27'))[0];
  demo.dispose();
  const jobs = Array.from({ length: 65 }, (_, i) => ({
    ...job,
    id: 17 + i,
    name: `billing-page-${i + 1}`,
  }));
  const runs = Array.from({ length: 65 }, (_, i) => ({ ...run, id: 1042 + i }));
  const functions = Array.from({ length: 65 }, (_, i) => ({
    ...fn,
    id: 27 + i,
    name: `billing-function-${i + 1}`,
  }));
  const workflows = Array.from({ length: 65 }, (_, i) => ({
    ...workflow,
    id: 412 + i,
    name: `billing-workflow-${i + 1}`,
  }));
  const workflowRuns = Array.from({ length: 65 }, (_, i) => ({ ...workflowRun, id: 94 + i }));
  const invocations = Array.from({ length: 65 }, (_, i) => ({
    ...invocation,
    id: 80 + i,
    run_id: crypto.randomUUID(),
  }));
  const reads: URL[] = [];
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
    const url = new URL(route.request().url());
    const pathname = url.pathname;
    if (pathname === '/api/v1/identity') return route.fallback();
    reads.push(url);
    const limit = Number(url.searchParams.get('limit') ?? 20);
    const offset = Number(url.searchParams.get('offset') ?? 0);
    const rows =
      pathname === '/api/v1/jobs'
        ? jobs
        : pathname === '/api/v1/instances'
          ? runs
          : pathname === '/api/v1/functions'
            ? functions
            : pathname === '/api/v1/workflows'
              ? workflows
              : pathname === '/api/v1/workflows/412/runs'
                ? workflowRuns
                : pathname === '/api/v1/functions/27/runs'
                  ? invocations
                  : undefined;
    if (rows) return route.fulfill({ json: { items: rows.slice(offset, offset + limit) } });
    if (pathname === '/api/v1/instances/summary')
      return route.fulfill({ json: { total: 65, phases: { RetryWaiting: 65 } } });
    if (pathname === '/api/v1/jobs/17') return route.fulfill({ json: jobs[0] });
    if (pathname === '/api/v1/functions/27') return route.fulfill({ json: functions[0] });
    if (pathname === '/api/v1/workflows/412') return route.fulfill({ json: workflows[0] });
    const runID = pathname.match(/^\/api\/v1\/instances\/(\d+)$/)?.[1];
    if (runID) {
      const item = runs.find((row) => String(row.id) === runID)!;
      return route.fulfill({
        json: { ...item, attempts: item.attempts.map((a) => ({ ...a, run_id: item.id })) },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto(path);
  await signIn(page, 'page_size_fixture_not_a_real_key');
  return reads;
}

for (const resource of [
  { path: 'jobs', search: 'Search jobs', record: 'billing-page-1', parent: 'Jobs' },
  {
    path: 'functions',
    search: 'Search functions',
    record: 'billing-function-1',
    parent: 'Functions',
  },
  {
    path: 'workflows',
    search: 'Search workflows',
    record: 'billing-workflow-1',
    parent: 'Workflows',
  },
])
  test(`${resource.path} page size reaches the server and survives detail return`, async ({
    page,
  }) => {
    const reads = await fixture(page, `/console/${resource.path}?search=billing&limit=10&page=2`);
    await expect(page.locator('tbody tr')).toHaveCount(10);
    await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
    expect(
      reads
        .filter((url) => url.pathname === `/api/v1/${resource.path}`)
        .at(-1)
        ?.searchParams.get('offset'),
    ).toBe('10');
    await page.getByLabel('Rows per page', { exact: true }).selectOption('50');
    await expect(page.locator('tbody tr')).toHaveCount(50);
    await expect(page.getByText('Page 1', { exact: true })).toBeVisible();
    expect(new URL(page.url()).searchParams.get('search')).toBe('billing');
    expect(new URL(page.url()).searchParams.has('page')).toBe(false);
    const collection = page.url();
    await page.getByRole('link', { name: resource.record, exact: true }).click();
    if (resource.path === 'functions')
      await expect(page.getByRole('combobox', { name: 'History limit' })).toHaveText('20');
    await page.getByRole('link', { name: `Back to ${resource.parent}`, exact: true }).click();
    await expect(page).toHaveURL(collection);
    await expect(page.getByLabel('Rows per page', { exact: true })).toHaveValue('50');
    await page.getByRole('button', { name: 'Next page', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(15);
    await expect(page.getByText('Rows 51–65', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeDisabled();
    await page.getByLabel('Rows per page', { exact: true }).selectOption('20');
    await expect(page.locator('tbody tr')).toHaveCount(20);
    expect(new URL(page.url()).searchParams.has('limit')).toBe(false);
    expect(new URL(page.url()).searchParams.has('page')).toBe(false);
    await expect(page.getByLabel(resource.search, { exact: true })).toHaveValue('billing');
  });

test('Runs page size preserves exact time and execution filters while the summary stays global', async ({
  page,
}) => {
  const query = new URLSearchParams({
    phase: 'RetryWaiting',
    source_uid: 'page-size-source',
    occurrence_key: 'page-size-occurrence',
    created_from: '2026-10-01T00:00:00.000001Z',
    created_before: '2026-11-01T08:00:00+08:00',
  });
  const reads = await fixture(page, `/console/runs?${query}&page=2&limit=10`);
  await expect(page.locator('tbody tr')).toHaveCount(10);
  await page.getByLabel('Rows per page', { exact: true }).selectOption('100');
  await expect(page.locator('tbody tr')).toHaveCount(65);
  await expect(page.getByText('65 matching runs', { exact: true })).toBeVisible();
  const request = reads.filter((url) => url.pathname === '/api/v1/instances').at(-1)!;
  expect(request.searchParams.get('limit')).toBe('100');
  expect(request.searchParams.get('offset')).toBe('0');
  for (const [key, value] of query) expect(request.searchParams.get(key)).toBe(value);
  const collection = page.url();
  await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
  await page.getByRole('link', { name: 'Back to Runs', exact: true }).click();
  await expect(page).toHaveURL(collection);
  await expect(page.getByLabel('Rows per page', { exact: true })).toHaveValue('100');
});

for (const resource of ['jobs/17', 'workflows/412'])
  test(`${resource} history pages use the selected page size`, async ({ page }) => {
    const reads = await fixture(page, `/console/${resource}?tab=runs&limit=10&page=2`);
    await expect(page.locator('tbody tr')).toHaveCount(10);
    await page.getByLabel('Rows per page', { exact: true }).selectOption('50');
    await expect(page.locator('tbody tr')).toHaveCount(50);
    await page.getByRole('button', { name: 'Next page', exact: true }).click();
    await expect(page.locator('tbody tr')).toHaveCount(15);
    await expect(page.getByText('Rows 51–65', { exact: true })).toBeVisible();
    const endpoint = resource.startsWith('jobs')
      ? '/api/v1/instances'
      : '/api/v1/workflows/412/runs';
    const request = reads.filter((url) => url.pathname === endpoint).at(-1)!;
    expect(request.searchParams.get('limit')).toBe('50');
    expect(request.searchParams.get('offset')).toBe('50');
    expect(new URL(page.url()).searchParams.get('tab')).toBe('runs');
  });
