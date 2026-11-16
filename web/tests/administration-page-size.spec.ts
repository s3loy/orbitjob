import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { ulid } from 'ulid';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page, path = '/console/administration?panel=tenants&limit=10&page=2') {
  const rows = Array.from({ length: 60 }, (_, i) => ({
    id: ulid(),
    name: `Finance ${String(i + 1).padStart(2, '0')}`,
    slug: `finance-${i + 1}`,
    status: 'active',
  }));
  const reads: URL[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    reads.push(url);
    if (url.pathname === '/api/v1/identity') {
      const probes = JSON.parse(url.searchParams.get('probes') || '[]');
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          capabilities: probes.map((probe: object) => ({ ...probe, allowed: true })),
        },
      });
    }
    if (url.pathname === '/api/v1/workspaces') {
      const limit = Number(url.searchParams.get('limit') ?? 20);
      const offset = Number(url.searchParams.get('offset') ?? 0);
      const search = (url.searchParams.get('search') ?? '').toLowerCase();
      const filtered = rows.filter((row) =>
        `${row.name} ${row.slug}`.toLowerCase().includes(search),
      );
      return route.fulfill({
        json: {
          items: filtered.slice(offset, offset + limit),
          has_more: filtered.length > offset + limit,
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto(path);
  await signIn(page, 'admin_page_size_fixture_not_real');
  await expect(page.locator('.admin-record-table tbody tr')).toHaveCount(10);
  return { rows, reads };
}

test('Workspaces page size uses the server limit, preserves search and trusts has_more on a full final page', async ({
  page,
}) => {
  const { reads } = await fixture(page);
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await page.getByLabel('Search workspaces', { exact: true }).fill('Finance');
  await expect(page.getByText('Page 1', { exact: true })).toBeVisible();
  await page.getByLabel('Rows per page', { exact: true }).selectOption('50');
  await expect(page.locator('.admin-record-table tbody tr')).toHaveCount(50);
  expect(new URL(page.url()).searchParams.get('search')).toBe('Finance');
  expect(new URL(page.url()).searchParams.get('panel')).toBe('tenants');
  expect(new URL(page.url()).searchParams.has('page')).toBe(false);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('.admin-record-table tbody tr')).toHaveCount(10);
  await expect(page.getByText('Rows 51–60', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeDisabled();
  const last = reads.filter((url) => url.pathname === '/api/v1/workspaces').at(-1)!;
  expect(last.searchParams.get('limit')).toBe('50');
  expect(last.searchParams.get('offset')).toBe('50');
  await page.getByLabel('Rows per page', { exact: true }).selectOption('10');
  for (let i = 0; i < 5; i++) {
    await page.getByRole('button', { name: 'Next page', exact: true }).click();
    await expect(page.getByText(`Page ${i + 2}`, { exact: true })).toBeVisible();
  }
  await expect(page.locator('.admin-record-table tbody tr')).toHaveCount(10);
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeDisabled();
});

test('panel and workspace switches reset the page without adding fake pagination to complete catalogs', async ({
  page,
}) => {
  const { rows } = await fixture(page);
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  expect(new URL(page.url()).searchParams.has('page')).toBe(false);
  expect(new URL(page.url()).searchParams.get('limit')).toBe('10');
  await expect(page.getByLabel('Rows per page', { exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Resource groups', exact: true }).click();
  await expect(page.getByLabel('Rows per page', { exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Workspaces', exact: true }).click();
  await expect(page.locator('.admin-record-table tbody tr')).toHaveCount(10);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await page
    .getByRole('row')
    .filter({ hasText: rows[10].name })
    .getByRole('button', { name: 'Manage access', exact: true })
    .click();
  await expect(page.getByRole('tab', { name: 'API keys', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  expect(new URL(page.url()).searchParams.has('page')).toBe(false);
  expect(new URL(page.url()).searchParams.get('limit')).toBe('10');
  await expect(page.getByLabel('Rows per page', { exact: true })).toHaveCount(0);
});
