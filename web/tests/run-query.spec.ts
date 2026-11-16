import { signIn } from './support';
import { expect, test } from './support';

test('clearing an unapplied time field keeps the range open without requesting new results', async ({
  page,
}) => {
  let reads = 0;
  await page.route('**/api/v1/instances**', (route) => {
    reads++;
    return route.fulfill({
      json: new URL(route.request().url()).pathname.endsWith('/summary')
        ? { total: 0, phases: {} }
        : { items: [] },
    });
  });
  await page.goto('/console/runs');
  await signIn(page);
  await expect(page.getByText('0 matching runs', { exact: true })).toBeVisible();
  const before = reads;
  const range = page.locator('details.history-range');
  await expect(range).not.toHaveAttribute('open', '');
  await range.locator(':scope > summary').click();
  const from = page.getByLabel('Created time from', { exact: true });
  await from.fill('2026-10-01T00:00:00Z');
  await from.fill('');
  await expect(range).toHaveAttribute('open', '');
  await expect(from).toBeVisible();
  await expect(range.getByRole('button', { name: 'Apply range', exact: true })).toBeVisible();
  expect(reads).toBe(before);
});
test('run counts and pages preserve the same exact execution identity and time filters', async ({
  page,
}) => {
  const reads: URL[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname.includes('/instances')) reads.push(url);
    return route.fulfill({
      json: url.pathname.endsWith('/summary') ? { total: 0, phases: {} } : { items: [] },
    });
  });
  await page.goto(
    '/console/runs?source_uid=exact-source&occurrence_key=exact-occ&created_from=2026-10-01T00%3A00%3A00Z&created_before=2026-11-01T00%3A00%3A00Z',
  );
  await signIn(page, 'otj_fixture_query_only');
  await expect(page.getByText('0 matching runs', { exact: true })).toBeVisible();
  expect(reads).toHaveLength(2);
  for (const url of reads) {
    expect(url.searchParams.get('source_uid')).toBe('exact-source');
    expect(url.searchParams.get('occurrence_key')).toBe('exact-occ');
    expect(url.searchParams.get('created_from')).toBe('2026-10-01T00:00:00Z');
    expect(url.searchParams.get('created_before')).toBe('2026-11-01T00:00:00Z');
  }
  const range = page.locator('details.history-range');
  await expect(range).toHaveAttribute('open', '');
  await expect(page.getByLabel('Created time from', { exact: true })).toHaveValue(
    '2026-10-01T00:00:00Z',
  );
  await expect(page.getByLabel('Created time before', { exact: true })).toHaveValue(
    '2026-11-01T00:00:00Z',
  );
  await page.getByText('Source and request filters applied', { exact: true }).click();
  await expect(page.getByText('exact-source', { exact: true })).toBeVisible();
  await expect(page.getByText('exact-occ', { exact: true })).toBeVisible();
});
test('invalid run time URL cannot cause an unfiltered ledger or summary request', async ({
  page,
}) => {
  let reads = 0;
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    if (route.request().url().includes('/instances')) reads++;
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/runs?created_from=invalid');
  await signIn(page, 'otj_fixture_query_only');
  await expect(page.getByRole('alert')).toContainText('RFC3339');
  await expect(page.getByRole('button', { name: 'Refresh', exact: true })).toBeDisabled();
  expect(reads).toBe(0);
});
