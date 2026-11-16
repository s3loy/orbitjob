import { signIn } from './support';
import { expect, test, fixtureIdentity } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

test('Monitoring collection size changes the server window and retains search', async ({
  page,
}) => {
  const demo = demoClient();
  const check = await demo.check('1');
  demo.dispose();
  // This deterministic dataset exercises page boundaries, not resource identity generation.
  const rows = Array.from({ length: 65 }, (_, i) => ({
    ...check,
    id: i + 1,
    name: `health-check-${i + 1}`,
  }));
  const seen: URL[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    seen.push(url);
    const offset = Number(url.searchParams.get('offset') ?? 0);
    const limit = Number(url.searchParams.get('limit') ?? 20);
    return route.fulfill({ json: { items: rows.slice(offset, offset + limit) } });
  });
  await page.goto('/console/checks?search=health&page=2');
  await signIn(page, 'monitoring_size_fixture_not_a_real_key');
  await page.getByRole('combobox', { name: 'Rows per page', exact: true }).selectOption('50');
  await expect(page.locator('.monitoring-table tbody tr')).toHaveCount(50);
  await expect(page).toHaveURL(/search=health&limit=50$/);
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.locator('.monitoring-table tbody tr')).toHaveCount(15);
  await expect(page.getByText('Rows 51–65', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Next page', exact: true })).toBeDisabled();
  expect(seen.at(-1)?.searchParams.get('limit')).toBe('50');
  expect(seen.at(-1)?.searchParams.get('offset')).toBe('50');
  expect(seen.at(-1)?.searchParams.get('search')).toBe('health');
});

test('SLO history and alerts honor selected size, resetting page but retaining size across tabs', async ({
  page,
}) => {
  const demo = demoClient();
  const [slo, budget, alert] = await Promise.all([
    demo.slo('21'),
    demo.budget('21'),
    demo.alert('31'),
  ]);
  demo.dispose();
  const seen: URL[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    seen.push(url);
    if (url.pathname === '/api/v1/slos/21') return route.fulfill({ json: slo });
    const offset = Number(url.searchParams.get('offset') ?? 0);
    const limit = Number(url.searchParams.get('limit') ?? 20);
    const items = Array.from({ length: 65 }, (_, i) =>
      url.pathname.includes('/budgets')
        ? {
            ...budget,
            window_start: new Date(Date.UTC(2026, 0, i + 1)).toISOString(),
            window_end: new Date(Date.UTC(2026, 0, i + 2)).toISOString(),
          }
        : { ...alert, id: i + 1 },
    );
    return route.fulfill({ json: { items: items.slice(offset, offset + limit) } });
  });
  await page.goto('/console/slos/21?tab=history&page=2');
  await page.getByLabel('Username', { exact: true }).fill('fixture-user');
  await page
    .getByLabel('Password', { exact: true })
    .fill('slo_history_size_fixture_not_a_real_key');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await page.getByRole('combobox', { name: 'Rows per page', exact: true }).selectOption('50');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('Rows 51–65', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Alerts', exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Rows per page', exact: true })).toHaveValue(
    '50',
  );
  await expect(page.getByText('Rows 1–50', { exact: true })).toBeVisible();
  const last = seen.at(-1)!;
  expect(last.pathname).toBe('/api/v1/slo-alerts');
  expect(last.searchParams.get('limit')).toBe('50');
  expect(last.searchParams.get('offset')).toBe('0');
  expect(last.searchParams.get('slo_id')).toBe('21');
});
