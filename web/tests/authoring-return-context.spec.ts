import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { demoClient } from '../src/api/demo';

async function demo(page: Page, path: string) {
  await page.goto(path);
  await page.getByRole('button', { name: 'Explore sample data' }).click();
}

test('a filtered Job creation opens the retained Job editor with its original collection', async ({
  page,
}) => {
  await demo(page, '/console/jobs?search=billing');
  await page.getByRole('link', { name: 'billing-reconciliation', exact: true }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Reload live definition', exact: true }),
  ).toBeEnabled();
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).click();
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  const retained = page.getByRole('link', { name: 'Open retained editor', exact: true });
  await expect(retained).toHaveAttribute('href', '/console/definitions/jobs');
  await retained.click();
  await expect(page.getByRole('heading', { name: 'Job definition', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await expect(page).toHaveURL(/\/jobs\?search=billing$/);
});

test('Function create Cancel preserves the filtered collection', async ({ page }) => {
  await demo(page, '/console/functions?search=rebuild');
  await page.getByRole('link', { name: 'Create a function', exact: true }).click();
  await page.getByRole('link', { name: 'Cancel', exact: true }).click();
  await expect(page).toHaveURL(/\/functions\?search=rebuild$/);
  await expect(page.getByLabel('Search functions', { exact: true })).toHaveValue('rebuild');
});

test('Function edit Cancel returns to Configuration without losing the original collection', async ({
  page,
}) => {
  await demo(page, '/console/functions?search=rebuild');
  await page.getByRole('link', { name: 'rebuild-search-index', exact: true }).click();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  const parent = page.url();
  await page.getByRole('link', { name: 'Edit function', exact: true }).click();
  await expect(page.getByRole('link', { name: 'Back to Function', exact: true })).toHaveAttribute(
    'href',
    new URL(parent).pathname + new URL(parent).search,
  );
  await page.getByRole('link', { name: 'Cancel', exact: true }).click();
  await expect(page).toHaveURL(parent);
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('link', { name: 'Back to Functions', exact: true }).click();
  await expect(page).toHaveURL(/\/functions\?search=rebuild$/);
});

test.describe('Job history return context', () => {
  test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

  test('tab changes and history pagination keep the filtered ancestor through a Run round trip', async ({
    page,
  }) => {
    const sample = demoClient();
    const job = await sample.job('17');
    const run = await sample.run('1042');
    sample.dispose();
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
      const path = url.pathname;
      if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
      if (path === '/api/v1/jobs') return route.fulfill({ json: { items: [job] } });
      if (path === '/api/v1/jobs/17') return route.fulfill({ json: job });
      if (path === '/api/v1/instances') {
        const offset = Number(url.searchParams.get('offset') ?? 0);
        return route.fulfill({
          json: {
            items: Array.from({ length: 20 }, (_, i) => ({ ...run, id: 1042 + offset + i })),
          },
        });
      }
      if (path === '/api/v1/instances/1062')
        return route.fulfill({
          json: {
            ...run,
            id: 1062,
            attempts: run.attempts.map((attempt) => ({ ...attempt, run_id: 1062 })),
          },
        });
      return route.fulfill({ json: { items: [] } });
    });
    await page.goto('/console/jobs?search=billing');
    await signIn(page, 'ux_history_fixture_not_a_real_key');
    await page.getByRole('link', { name: job.name, exact: true }).click();
    await page.getByRole('tab', { name: 'Definition', exact: true }).click();
    await page.getByRole('tab', { name: 'Runs', exact: true }).click();
    await page.getByRole('button', { name: 'Next page', exact: true }).click();
    await expect(page).toHaveURL(/page=2/);
    const history = page.url();
    await page.getByRole('link', { name: 'View run 1062', exact: true }).click();
    await page.getByRole('link', { name: 'Back to Job', exact: true }).click();
    await expect(page).toHaveURL(history);
    await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
    await expect(page).toHaveURL(/\/jobs\?search=billing$/);
  });
});
