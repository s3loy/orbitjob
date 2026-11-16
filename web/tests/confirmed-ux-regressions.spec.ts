import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function connect(page: Page, path: string) {
  await page.goto(`/console${path}`);
  await signIn(page, 'ux_regression_fixture_not_a_real_key');
}

test('alert page search preserves the loaded server window and makes no extra request', async ({
  page,
}) => {
  const demo = demoClient();
  const alert = await demo.alert('31');
  demo.dispose();
  const reads: URL[] = [];
  const rows = Array.from({ length: 45 }, (_, i) => ({ ...alert, id: i + 1 }));
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    if (url.pathname === '/api/v1/slo-alerts') {
      reads.push(url);
      const offset = Number(url.searchParams.get('offset') ?? 0);
      const limit = Number(url.searchParams.get('limit') ?? 20);
      return route.fulfill({ json: { items: rows.slice(offset, offset + limit) } });
    }
    return route.fulfill({ status: 404, json: { error: { code: 'NOT_FOUND' } } });
  });
  await connect(page, '/slo-alerts?page=2');
  await expect(page.locator('.monitoring-table tbody tr')).toHaveCount(20);
  const count = reads.length;
  await page.getByLabel('Filter alerts on this page').fill('21');
  await expect(page.getByRole('link', { name: 'Alert #21', exact: true })).toBeVisible();
  await expect(page.locator('.monitoring-table tbody tr')).toHaveCount(1);
  await expect(page.getByText('1 of 20 match on this page', { exact: true })).toBeVisible();
  expect(new URL(page.url()).searchParams.get('page')).toBe('2');
  expect(reads).toHaveLength(count);
  await page.getByLabel('Filter alerts on this page').fill('missing');
  await expect(page.getByRole('heading', { name: 'No matches on this alert page' })).toBeVisible();
  await page.getByLabel('Filter alerts on this page').fill('');
  await expect(page.locator('.monitoring-table tbody tr')).toHaveCount(20);
  expect(reads).toHaveLength(count);
  await page.getByRole('combobox', { name: 'Status', exact: true }).click();
  await page.getByRole('option', { name: 'Resolved', exact: true }).click();
  await expect.poll(() => reads.at(-1)?.searchParams.get('status')).toBe('resolved');
  expect(reads.at(-1)?.searchParams.get('offset')).toBe('0');
  expect(reads.at(-1)?.searchParams.has('search')).toBe(false);
  expect(new URL(page.url()).searchParams.has('page')).toBe(false);
});

for (const responseStatus of [200, 401, 403]) {
  test(`Administration keeps rows stable during refresh and handles ${responseStatus}`, async ({
    page,
  }) => {
    let refreshing = false;
    let requests = 0;
    let release!: () => void;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const workspace = {
      id: fixtureIdentity.tenant_id,
      name: 'Refresh workspace',
      slug: 'refresh-workspace',
      status: 'active',
    };
    await page.route('**/api/v1/**', async (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const url = new URL(route.request().url());
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
        if (refreshing) {
          requests++;
          await gate;
          if (responseStatus !== 200)
            return route.fulfill({
              status: responseStatus,
              json: {
                error: {
                  code: responseStatus === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN',
                  message: 'Access denied',
                },
              },
            });
        }
        return route.fulfill({ json: { items: [workspace], has_more: false } });
      }
      return route.fulfill({ json: { items: [] } });
    });
    try {
      await connect(page, '/administration?panel=tenants');
      const table = page.locator('.admin-record-table');
      const form = page.locator('.administration-create');
      await expect(table).toContainText(workspace.name);
      await expect(form).toBeVisible();
      const position = () =>
        form.evaluate((element) => element.getBoundingClientRect().top + window.scrollY);
      const before = await position();
      refreshing = true;
      await page.getByRole('button', { name: 'Refresh', exact: true }).click();
      await expect.poll(() => requests).toBeGreaterThan(0);
      await expect(table).toContainText(workspace.name);
      await expect(table.locator('..')).toHaveAttribute('aria-busy', 'true');
      await expect(page.getByRole('button', { name: 'Refreshing…', exact: true })).toBeDisabled();
      await expect(
        table.getByRole('button', { name: 'Manage access', exact: true }),
      ).toBeDisabled();
      expect(Math.abs((await position()) - before)).toBeLessThanOrEqual(1);
      release();
      if (responseStatus === 200) {
        await expect(table.locator('..')).toHaveAttribute('aria-busy', 'false');
        await expect(
          table.getByRole('button', { name: 'Manage access', exact: true }),
        ).toBeEnabled();
      } else {
        await expect(table).toHaveCount(0);
        if (responseStatus === 401)
          await expect(page.getByRole('heading', { name: 'Sign in to OrbitJob' })).toBeVisible();
        else await expect(page.getByRole('alert').first()).toContainText('Permission required');
      }
    } finally {
      release();
    }
  });
}
