import { signIn } from './support';
import { expect, test, fixtureIdentity } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

test('installed entry verifies credentials and has no simulated workspace', async ({ page }) => {
  const reads: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    reads.push(new URL(route.request().url()).pathname);
    return route.fulfill({
      status: 401,
      json: { error: { code: 'UNAUTHORIZED', message: 'This credential is invalid or revoked.' } },
    });
  });
  await page.route('**/api/v1/auth/login', (route) =>
    route.fulfill({
      status: 401,
      json: { error: { code: 'unauthorized', message: 'Invalid credentials' } },
    }),
  );
  await page.goto('/console/');
  await expect(page.getByRole('heading', { name: 'Sign in to OrbitJob' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Explore sample data' })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'OrbitJob', exact: true })).toBeVisible();
  await expect(page.getByLabel('Current workspace')).toHaveCount(0);
  await expect(page.locator('.sidebar, .topbar')).toHaveCount(0);
  await expect(page.getByRole('navigation', { name: 'Main navigation' })).toHaveCount(0);
  await signIn(page, 'otj_invalid_fixture_key');
  await expect(page.getByRole('alert')).toContainText('Sign in failed.');
  await expect(page).toHaveURL(/\/connection$/);
  expect(reads).toEqual([]);
  expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
});

test('limited access gives useful next steps and an honest overview', async ({ page }) => {
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity')
      return route.fulfill({
        json: { ...fixtureIdentity, group_id: '00000000000000000000000003' },
      });
    return route.fulfill({
      status: 403,
      json: { error: { code: 'FORBIDDEN', message: 'This action needs an additional grant.' } },
    });
  });
  await page.goto('/console/');
  await signIn(page, 'otj_limited_fixture_key');
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(2);
  await expect(page.getByRole('alert').first()).toContainText('Permission required');
  await expect(page.locator('.outcome-strip')).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Shortcuts' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Manage functions', exact: false })).toBeVisible();
});

test('Job form reaches save and exact version readiness without JSON editing', async ({ page }) => {
  const fixture = demoClient();
  const job = await fixture.job(17);
  fixture.dispose();
  let saved = false;
  let writes = 0;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path.startsWith('/api/v1/definitions/jobs/')) {
      const input = route.request().postDataJSON();
      if (!input.dry_run) {
        saved = true;
        writes++;
      }
      return route.fulfill({
        json: {
          kind: 'jobs',
          name: 'nightly-report',
          namespace: 'finance-prod',
          uid: 'saved-job-uid',
          resource_version: '8',
          generation: 1,
          spec_json: input.spec_json,
          manifest_digest: 'digest',
          last_request_id: input.dry_run ? '' : input.request_id,
          last_manifest_digest: input.dry_run ? '' : 'digest',
          request_id: input.request_id,
          state: input.dry_run ? 'validated' : 'accepted',
        },
      });
    }
    if (path === '/api/v1/jobs')
      return route.fulfill({
        json: {
          items: saved ? [{ ...job, name: 'nightly-report', source_uid: 'saved-job-uid' }] : [],
        },
      });
    return route.fulfill({
      json: path.endsWith('/summary') ? { total: 0, phases: {} } : { items: [] },
    });
  });
  await page.goto('/console/manifests/job');
  await signIn(page, 'otj_create_job_fixture_key');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('nightly-report');
  await page.getByRole('textbox', { name: 'Namespace', exact: true }).fill('finance-prod');
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(page.getByText('nightly-report', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Definition spec JSON')).not.toBeVisible();
  await page.getByRole('button', { name: 'Check against cluster' }).click();
  await page.getByRole('button', { name: 'Save definition' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByText('Saved version is ready', { exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open job', exact: true })).toHaveAttribute(
    'href',
    '/console/jobs/17',
  );
  expect(writes).toBe(1);
});

test('Function create waits for the reviewed revision and sends its version on invoke', async ({
  page,
}) => {
  const fixture = demoClient();
  const base = await fixture.functionDefinition('27');
  fixture.dispose();
  let current = {
    ...base,
    id: 99,
    active_revision_id: null as number | string | null,
    name: 'daily-report',
    resource_group_id: '',
    version: 1,
    ready: false,
    active_revision_version: null as number | null,
  };
  const versions: string[] = [];
  const keys: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/functions' && route.request().method() === 'POST') {
      keys.push(route.request().headers()['x-orbitjob-idempotency-key']);
      current = { ...current, ...route.request().postDataJSON() };
      return route.fulfill({ status: 201, json: current });
    }
    if (url.pathname === '/api/v1/functions/99') return route.fulfill({ json: current });
    if (url.pathname === '/api/v1/functions/99/invoke') {
      versions.push(url.searchParams.get('expected_version') || '');
      return route.fulfill({
        json: {
          namespace: 'finance-prod',
          name: 'function-run-99',
          occurrence_key: 'function-created',
          trigger: 'Function',
          phase: 'Pending',
          created: true,
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/functions/new');
  await signIn(page, 'otj_create_function_fixture_key');
  await page.getByLabel('Function name').fill('daily-report');
  await page.getByLabel('Container image').fill('busybox:1.37');
  await page.getByRole('button', { name: 'Review function' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(
    page.getByRole('heading', { name: 'Saved. Getting ready', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Invoke daily-report' })).toHaveCount(0);
  current = { ...current, ready: true, active_revision_id: 1, active_revision_version: 1 };
  await page.getByRole('button', { name: 'Check readiness again' }).click();
  await page.getByRole('button', { name: 'Invoke daily-report' }).click();
  await page.getByRole('button', { name: 'Confirm invoke', exact: true }).click();
  await expect.poll(() => versions.length).toBe(1);
  expect(versions).toEqual(['1']);
  expect(keys[0]).toBeTruthy();
});
