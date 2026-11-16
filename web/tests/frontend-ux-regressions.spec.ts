import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { demoClient } from '../src/api/demo';

async function navigate(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

test.describe('Runs credential recovery', () => {
  test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

  async function fixture(page: Page) {
    const demo = demoClient();
    const job = await demo.job('17');
    const run = await demo.run('1042');
    demo.dispose();
    const state = { failure: '', reads: [] as string[] };
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      state.reads.push(path);
      if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
      if (path === state.failure)
        return route.fulfill({
          status: 401,
          json: { error: { code: 'UNAUTHORIZED', message: 'Fixture credential expired' } },
        });
      return route.fulfill({
        json:
          path === '/api/v1/instances/summary'
            ? { total: 1, phases: { RetryWaiting: 1 } }
            : { items: path === '/api/v1/instances' ? [run] : [job] },
      });
    });
    await page.goto('/console/runs');
    await signIn(page, 'ux_fixture_not_a_real_key');
    await expect(page.getByRole('link', { name: 'View run 1042', exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Filtered run summary' })).toContainText(
      '1 matching runs',
    );
    return state;
  }

  async function rejected(page: Page) {
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(page.locator('.runs-table')).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Filtered run summary' })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Refresh', exact: true })).toHaveCount(0);
  }

  test('a sibling-page 401 hides cached Runs and stops later reads', async ({ page }) => {
    const state = await fixture(page);
    await navigate(page, 'Jobs');
    await expect(page.locator('.jobs-table')).toBeVisible();
    state.failure = '/api/v1/jobs';
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await rejected(page);
    const reads = [...state.reads];
    await page.goto('/console/runs');
    await rejected(page);
    expect(state.reads).toEqual(reads);
  });

  for (const endpoint of ['/api/v1/instances', '/api/v1/instances/summary'])
    test(`${endpoint} 401 invalidates the connection and cannot revive cached rows`, async ({
      page,
    }) => {
      const state = await fixture(page);
      await navigate(page, 'Jobs');
      await expect(page.locator('.jobs-table')).toBeVisible();
      await navigate(page, 'Runs');
      state.failure = endpoint;
      await page.getByRole('button', { name: 'Refresh', exact: true }).click();
      await rejected(page);
      const reads = [...state.reads];
      state.failure = '';
      await page.goto('/console/jobs');
      await expect(page.locator('.jobs-table')).toHaveCount(0);
      await rejected(page);
      await page.goto('/console/runs');
      await rejected(page);
      expect(state.reads).toEqual(reads);
    });
});
