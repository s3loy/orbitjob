import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page, status: number) {
  const demo = demoClient();
  const job = await demo.job('17');
  demo.dispose();
  let release!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  const reads: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    reads.push(path);
    if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    if (path === '/api/v1/jobs/17') {
      await held;
      return status === 200
        ? route.fulfill({ json: job })
        : route.fulfill({
            status,
            json: { error: { code: 'SOURCE_FAILED', message: 'Fixture source read rejected' } },
          });
    }
    return route.fulfill({ json: { items: path === '/api/v1/jobs' ? [job] : [] } });
  });
  await page.goto('/console/functions/new');
  await signIn(page, 'image_intent_fixture_not_a_real_key');
  await page.getByRole('button', { name: 'Browse images', exact: true }).click();
  const requested = page.waitForRequest(
    (request) => new URL(request.url()).pathname === '/api/v1/jobs/17',
  );
  await page.getByRole('button', { name: `Reuse image from ${job.name}`, exact: true }).click();
  await requested;
  return { release, reads };
}

for (const status of [200, 503])
  test(`a late image source ${status} cannot overwrite newer manual input or feedback`, async ({
    page,
  }) => {
    const state = await fixture(page, status);
    await page
      .getByLabel('Container image', { exact: true })
      .fill('registry.example/manual-newer:v2');
    const response = page.waitForResponse(
      (result) => new URL(result.url()).pathname === '/api/v1/jobs/17',
    );
    state.release();
    await response;
    await expect(page.getByLabel('Container image', { exact: true })).toHaveValue(
      'registry.example/manual-newer:v2',
    );
    await expect(page.getByText('Image copied from', { exact: false })).toHaveCount(0);
    await expect(
      page.getByText('Image sources could not be loaded.', { exact: false }),
    ).toHaveCount(0);
    await expect(page.getByRole('button', { name: /Reading image/ })).toHaveCount(0);
  });

test('an image source 401 invalidates other protected pages', async ({ page }) => {
  const state = await fixture(page, 401);
  state.release();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.jobs-table')).toHaveCount(0);
  await expect(page.locator('.sidebar')).toHaveCount(0);
});
