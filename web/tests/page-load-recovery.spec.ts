import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const job = await demo.job('17');
  demo.dispose();
  const state = { triggers: 0, documents: 0 };
  page.on('request', (request) => {
    if (request.isNavigationRequest() && request.resourceType() === 'document') state.documents++;
  });
  await page.route('**/openapi.json', (route) => route.fulfill({ json: { paths: {} } }));
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/jobs/17/trigger') {
      state.triggers++;
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'retained-page-recovery-trigger',
          occurrence_key: 'retained-page-recovery-occurrence',
          trigger: 'Manual',
          phase: 'Pending',
          created: true,
        },
      });
    }
    if (path.startsWith('/api/v1/definitions/jobs/'))
      return route.fulfill({
        json: {
          kind: 'jobs',
          namespace: job.namespace,
          name: job.name,
          uid: job.source_uid,
          resource_version: '7',
          generation: 1,
          spec_json: '{"schedule":"","timeoutSeconds":120,"jobTemplate":{"image":"busybox:1.37"}}',
          manifest_digest: 'retained-live-digest',
          last_request_id: '',
          last_manifest_digest: '',
        },
      });
    return route.fulfill({
      json:
        path === '/api/v1/jobs/17'
          ? job
          : path === '/api/v1/jobs'
            ? { items: [{ ...job, source_kind: 'kubernetes' }] }
            : { items: [] },
    });
  });
  await page.goto('/console/jobs/17');
  await signIn(page, 'page_recovery_fixture_key');
  await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(page.getByLabel('Timeout in seconds', { exact: true })).toHaveValue('120');
  await page.getByLabel('Timeout in seconds', { exact: true }).fill('777');
  return { state, job };
}

async function navigate(page: Page, name: 'Checks' | 'Jobs') {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

test('an interrupted route module preserves navigation, receipts and drafts until explicit reload', async ({
  page,
}) => {
  const { state, job } = await fixture(page);
  let moduleRequests = 0;
  await page.route(/\/src\/pages\/Monitoring\.tsx(?:\?.*)?$/, (route) => {
    moduleRequests++;
    return moduleRequests === 1 ? route.abort('connectionreset') : route.fallback();
  });
  await navigate(page, 'Checks');
  const recovery = page.getByRole('region', { name: 'Page recovery', exact: true });
  await expect(recovery.getByRole('heading', { name: 'Page unavailable' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toBeVisible();
  await expect(
    page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }),
  ).toBeVisible();
  await expect(recovery).toContainText('Navigation and Activity remain available');
  await expect(recovery.getByRole('button', { name: /Retry/ })).toHaveCount(0);
  const activity = page.locator('details.operation-activity');
  await activity.locator(':scope > summary').click();
  await expect(
    activity.getByRole('region', { name: `Trigger receipt for ${job.name}`, exact: true }),
  ).toContainText(`Trigger accepted for ${job.name}`);
  await navigate(page, 'Jobs');
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await expect(recovery).toHaveCount(0);
  await page.getByRole('link', { name: job.name, exact: true }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(page.getByLabel('Timeout in seconds', { exact: true })).toHaveValue('777');
  await navigate(page, 'Checks');
  await expect(recovery).toBeVisible();
  await recovery.getByText('Reload options', { exact: true }).click();
  await expect(recovery).toContainText(
    'Reloading discards changes kept only in this tab and clears Activity',
  );
  await expect(recovery).toContainText('Your sign-in is restored');
  await expect(recovery).toContainText('record any pending or unknown request IDs and receipts');
  expect(state.documents).toBe(1);
  expect(state.triggers).toBe(1);
  expect(moduleRequests).toBe(1);
  await recovery.getByRole('button', { name: 'Reload and clear local state', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Checks', exact: true })).toBeVisible();
  await expect(page.locator('details.operation-activity')).toHaveCount(0);
  await signIn(page, 'page_recovery_fixture_key');
  await expect(page.getByRole('heading', { name: 'Checks', exact: true })).toBeVisible();
  expect(moduleRequests).toBeGreaterThan(1);
  expect(state.documents).toBe(2);
  expect(state.triggers).toBe(1);
});

test('a route render failure leaves the existing connection and Activity outside the boundary', async ({
  page,
}) => {
  const { state, job } = await fixture(page);
  await page.route(/\/src\/pages\/Monitoring\.tsx(?:\?.*)?$/, (route) =>
    route.fulfill({
      contentType: 'application/javascript',
      body: 'export function MonitoringList() { throw new Error("Fixture route render failure"); }',
    }),
  );
  await navigate(page, 'Checks');
  await expect(page.getByRole('region', { name: 'Page recovery', exact: true })).toBeVisible();
  const activity = page.locator('details.operation-activity');
  await activity.locator(':scope > summary').click();
  await expect(activity).toContainText(`Trigger accepted for ${job.name}`);
  await navigate(page, 'Jobs');
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await expect(
    page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }),
  ).toBeVisible();
  await page.getByRole('link', { name: job.name, exact: true }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(page.getByLabel('Timeout in seconds', { exact: true })).toHaveValue('777');
  expect(state.documents).toBe(1);
  expect(state.triggers).toBe(1);
});
