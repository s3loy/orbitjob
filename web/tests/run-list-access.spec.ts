import { signIn } from './support';
import { expect, test, fixtureIdentity } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

for (const denied of ['/api/v1/instances', '/api/v1/instances/summary'])
  test(`${denied} 401 remains rejected when the other request succeeds later`, async ({ page }) => {
    const demo = demoClient();
    const run = await demo.run('1042');
    demo.dispose();
    let refresh = false;
    let release!: () => void;
    const delayed = new Promise<void>((resolve) => {
      release = resolve;
    });
    const other = denied.endsWith('/summary') ? '/api/v1/instances' : '/api/v1/instances/summary';
    await page.route('**/api/v1/**', async (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
      if (refresh && path === denied)
        return route.fulfill({
          status: 401,
          json: { error: { code: 'UNAUTHORIZED', message: 'Fixture credential rejected' } },
        });
      if (refresh && path === other) await delayed;
      return route.fulfill({
        json: path.endsWith('/summary')
          ? { total: 1, phases: { RetryWaiting: 1 } }
          : { items: [run] },
      });
    });
    await page.goto('/console/runs');
    await signIn(page, 'ux_race_fixture_not_a_real_key');
    await expect(page.getByRole('link', { name: 'View run 1042', exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Filtered run summary' })).toContainText(
      '1 matching runs',
    );
    refresh = true;
    const requested = page.waitForRequest((request) => new URL(request.url()).pathname === other);
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await requested;
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    release();
    await expect(page.locator('.runs-table')).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Filtered run summary' })).toHaveCount(0);
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
  });

test('a Runs permission denial keeps separately authorized pages usable', async ({ page }) => {
  const demo = demoClient();
  const job = await demo.job('17');
  demo.dispose();
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    if (path === '/api/v1/instances')
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Fixture run permission denied' } },
      });
    return route.fulfill({
      json: path.endsWith('/summary') ? { total: 1, phases: { Running: 1 } } : { items: [job] },
    });
  });
  await page.goto('/console/runs');
  await signIn(page, 'ux_denial_fixture_not_a_real_key');
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await expect(page.getByRole('link', { name: 'Reconnect', exact: true })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Filtered run summary' })).toContainText(
    '1 matching runs',
  );
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await expect(page.locator('.jobs-table')).toBeVisible();
  await expect(page.getByRole('link', { name: job.name, exact: true })).toBeVisible();
});
