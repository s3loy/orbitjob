import { signIn } from './support';
import { expect, test, type Page } from './support';
import type { FunctionDefinition, FunctionRun } from '../src/api/m4';
const time = '2026-10-19T00:00:00Z';
const fn: FunctionDefinition = {
  id: 27,
  tenant_id: '00000000000000000000000001',
  resource_group_id: '',
  name: 'Server-selected function',
  description: 'API_% searchable description',
  status: 'active',
  image: 'example:v1',
  command: [],
  args: [],
  ready: true,
  timeout_seconds: 60,
  retry_limit: 0,
  history_success: 50,
  history_failed: 50,
  labels: {},
  version: 1,
  created_at: time,
  updated_at: time,
};
const outcomes: FunctionRun[] = Array.from({ length: 45 }, (_, i) => ({
  id: 1000 + i,
  run_id: `00000000-0000-4000-8000-${String(i).padStart(12, '0')}`,
  function_id: 27,
  status: 'success',
  triggered_at: new Date(Date.parse(time) + i * 60000).toISOString(),
  started_at: time,
  finished_at: time,
  duration_ms: 10,
  attempts: null,
  last_attempt: null,
  version: 1,
  created_at: '2026-10-21T00:00:00Z',
})).reverse();
async function live(page: Page, path = '/console/') {
  const seen: URL[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    seen.push(url);
    if (url.pathname === '/api/v1/functions') {
      const offset = Number(url.searchParams.get('offset') ?? 0);
      const rows = Array.from({ length: 40 }, (_, i) => ({
        ...fn,
        id: 27 + i,
        name: i ? 'Function ' + i : fn.name,
      }));
      return route.fulfill({ json: { items: rows.slice(offset, offset + 20) } });
    }
    if (url.pathname === '/api/v1/functions/27') return route.fulfill({ json: fn });
    if (url.pathname === '/api/v1/functions/27/runs') {
      const from = url.searchParams.get('created_from');
      const before = url.searchParams.get('created_before');
      const offset = Number(url.searchParams.get('offset') ?? 0);
      const limit = Number(url.searchParams.get('limit') ?? 20);
      const rows = outcomes.filter(
        (row) =>
          (!from || Date.parse(row.triggered_at) >= Date.parse(from)) &&
          (!before || Date.parse(row.triggered_at) < Date.parse(before)),
      );
      return route.fulfill({ json: { items: rows.slice(offset, offset + limit) } });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto(path);
  await signIn(page, 'otj_query_fixture_not_a_real_key');
  return seen;
}
function historyRequests(seen: URL[]) {
  return seen.filter((url) => url.pathname === '/api/v1/functions/27/runs');
}
test('Function search reaches the server, resets page, and keeps server-selected rows', async ({
  page,
}) => {
  const seen = await live(page);
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Functions', exact: true })
    .click();
  await expect(page.locator('tbody tr')).toHaveCount(20);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page).toHaveURL(/page=2/);
  await page.getByLabel('Search functions', { exact: true }).fill('API_%');
  await expect(
    page.getByRole('link', { name: 'Server-selected function', exact: true }),
  ).toBeVisible();
  expect(new URL(page.url()).searchParams.has('page')).toBe(false);
  await expect
    .poll(() =>
      seen
        .filter((url) => url.pathname === '/api/v1/functions')
        .at(-1)
        ?.searchParams.get('search'),
    )
    .toBe('API_%');
  const last = seen.filter((url) => url.pathname === '/api/v1/functions').at(-1)!;
  expect(last.searchParams.get('search')).toBe('API_%');
  expect(last.searchParams.get('offset')).toBe('0');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page).toHaveURL(/page=2/);
  expect(new URL(page.url()).searchParams.get('search')).toBe('API_%');
  await expect
    .poll(() =>
      seen
        .filter((url) => url.pathname === '/api/v1/functions')
        .at(-1)
        ?.searchParams.get('offset'),
    )
    .toBe('20');
  expect(
    seen
      .filter((url) => url.pathname === '/api/v1/functions')
      .at(-1)!
      .searchParams.get('search'),
  ).toBe('API_%');
});
test('Function history applies invocation-time range, retains it across pages, and restores browser Back', async ({
  page,
}) => {
  const seen = await live(page, '/console/functions/27');
  await expect(page.locator('tbody tr')).toHaveCount(20);
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page).toHaveURL(/offset=20/);
  const from = '2026-10-19T08:05:00+08:00';
  const before = '2026-10-19T00:45:00Z';
  const range = page.locator('details.history-range');
  await expect(range).not.toHaveAttribute('open', '');
  await range.locator(':scope > summary').click();
  await page.getByLabel('Invocation time from', { exact: true }).fill(from);
  await page.getByLabel('Invocation time before', { exact: true }).fill(before);
  await page.getByRole('button', { name: 'Apply range', exact: true }).click();
  await expect(page.getByLabel('Invocation time from', { exact: true })).toHaveValue(from);
  await expect(range).toHaveAttribute('open', '');
  await expect(range.locator(':scope > summary')).toHaveText('Invocation time range applied');
  await expect(page.locator('tbody tr')).toHaveCount(20);
  expect(new URL(page.url()).searchParams.has('offset')).toBe(false);
  await expect
    .poll(() => historyRequests(seen).at(-1)?.searchParams.get('created_from'))
    .toBe(from);
  let request = historyRequests(seen).at(-1)!;
  expect(request.searchParams.get('created_from')).toBe(from);
  expect(request.searchParams.get('created_before')).toBe(before);
  expect(request.searchParams.get('offset')).toBe('0');
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page).toHaveURL(/offset=20/);
  await expect.poll(() => historyRequests(seen).at(-1)?.searchParams.get('offset')).toBe('20');
  request = historyRequests(seen).at(-1)!;
  expect(request.searchParams.get('created_from')).toBe(from);
  expect(request.searchParams.get('offset')).toBe('20');
  await page.goBack();
  await expect(page.getByLabel('Invocation time from', { exact: true })).toHaveValue(from);
  expect(new URL(page.url()).searchParams.has('offset')).toBe(false);
  await page.getByRole('button', { name: 'Clear range', exact: true }).click();
  await expect(range).not.toHaveAttribute('open', '');
  await expect(page.getByLabel('Invocation time from', { exact: true })).toHaveValue('');
  expect(new URL(page.url()).searchParams.has('created_from')).toBe(false);
  await page.setViewportSize({ width: 320, height: 740 });
  await page.emulateMedia({ colorScheme: 'dark' });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({
    path: 'test-results/function-history-range-320dark.png',
    fullPage: true,
  });
});
test('invalid URL range shows a field error and does not send an unfiltered history request', async ({
  page,
}) => {
  const seen = await live(page, '/console/functions/27?created_from=2026-10-19T00%3A00Z');
  const invocations = page.getByRole('tabpanel', { name: 'Invocations', exact: true });
  await expect(invocations.getByRole('alert')).toContainText('with seconds');
  const controls = page.getByRole('region', { name: 'Execution controls', exact: true });
  await expect(controls.getByRole('alert')).toContainText(
    'The API response does not match the supported contract.',
  );
  await expect(controls.getByRole('switch')).toHaveCount(0);
  await expect(controls.getByRole('button', { name: 'Save controls', exact: true })).toHaveCount(0);
  expect(historyRequests(seen)).toHaveLength(0);
  await expect(
    page.getByRole('button', { name: 'Refresh invocations', exact: true }),
  ).toBeDisabled();
  await page.getByRole('button', { name: 'Clear range', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(20);
  expect(historyRequests(seen)).toHaveLength(1);
});
