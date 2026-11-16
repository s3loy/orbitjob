import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const [job, check, result, sli, slo, budget, alert] = await Promise.all([
    demo.job('17'),
    demo.check('1'),
    demo.checkResult('700'),
    demo.sli('11'),
    demo.slo('21'),
    demo.budget('21'),
    demo.alert('31'),
  ]);
  demo.dispose();
  const state = { path: '', method: 'GET', reads: [] as string[], writes: [] as string[] };
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const path = new URL(request.url()).pathname;
    (request.method() === 'GET' ? state.reads : state.writes).push(path);
    if (path === '/api/v1/identity') return route.fulfill({ json: fixtureIdentity });
    if (path === state.path && request.method() === state.method)
      return route.fulfill({
        status: 401,
        json: { error: { code: 'UNAUTHORIZED', message: 'Fixture credential rejected' } },
      });
    const bodies: Record<string, unknown> = {
      '/api/v1/jobs': { items: [job] },
      '/api/v1/checks': { items: [check] },
      '/api/v1/checks/1': check,
      '/api/v1/check-runs': { items: [result] },
      '/api/v1/check-runs/700': result,
      '/api/v1/slis': { items: [sli] },
      '/api/v1/slis/11': sli,
      '/api/v1/slos': { items: [slo] },
      '/api/v1/slos/21': slo,
      '/api/v1/slos/21/budget': budget,
      '/api/v1/slos/21/budgets': { items: [budget] },
      '/api/v1/slo-alerts': { items: [alert] },
      '/api/v1/slo-alerts/31': alert,
    };
    return route.fulfill({ json: bodies[path] ?? { items: [] } });
  });
  return state;
}

async function connect(page: Page, path: string) {
  await page.goto(`/console${path}`);
  await signIn(page, 'monitoring_recovery_fixture_not_a_real_key');
}
async function nav(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

for (const entry of [
  { path: '/checks', heading: 'Checks', protected: '.monitoring-table' },
  { path: '/slis', heading: 'Indicators', protected: '.monitoring-table' },
  { path: '/slos', heading: 'Service objectives', protected: '.monitoring-table' },
  { path: '/slo-alerts', heading: 'Alerts', protected: '.monitoring-table' },
  { path: '/checks/1', heading: 'api-health', protected: '.page-heading .phase-label' },
  { path: '/check-results/700', heading: 'Result #700', protected: '.page-heading .phase-label' },
  { path: '/slis/11', heading: 'Payments API readiness', protected: '.monitoring-details' },
  {
    path: '/slos/21',
    heading: 'Payments API availability',
    protected: '.page-heading .phase-label',
  },
  { path: '/slo-alerts/31', heading: 'Alert #31', protected: '.page-heading .phase-label' },
])
  test(`a sibling-page 401 hides cached ${entry.path} without another read`, async ({ page }) => {
    const state = await fixture(page);
    await connect(page, entry.path);
    await expect(page.getByRole('heading', { name: entry.heading, exact: true })).toBeVisible();
    await expect(page.locator(entry.protected)).toBeVisible();
    state.path = '/api/v1/jobs';
    await nav(page, 'Jobs');
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    const before = state.reads.filter(
      (path) => path === `/api/v1${entry.path.replace('/check-results/', '/check-runs/')}`,
    ).length;
    await page.goBack();
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(page.locator(entry.protected)).toHaveCount(0);
    expect(
      state.reads.filter(
        (path) => path === `/api/v1${entry.path.replace('/check-results/', '/check-runs/')}`,
      ),
    ).toHaveLength(before);
  });

test('a monitoring read 401 invalidates an already cached Jobs page', async ({ page }) => {
  const state = await fixture(page);
  await connect(page, '/jobs');
  await expect(page.locator('.jobs-table')).toBeVisible();
  state.path = '/api/v1/checks';
  await nav(page, 'Checks');
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  const before = state.reads.filter((path) => path === '/api/v1/jobs').length;
  await page.goto('/console/jobs');
  await expect(page.locator('.jobs-table')).toHaveCount(0);
  expect(state.reads.filter((path) => path === '/api/v1/jobs')).toHaveLength(before);
});

test('a create 401 clears the Check draft and prevents a second submit', async ({ page }) => {
  const state = await fixture(page);
  await connect(page, '/checks');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Name', { exact: true }).fill('keep-after-create-denial');
  await dialog.getByLabel('URL', { exact: true }).fill('https://service.example/health');
  state.path = '/api/v1/checks';
  state.method = 'POST';
  await dialog.getByRole('button', { name: 'Create check', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.monitoring-table')).toHaveCount(0);
  expect(state.writes).toEqual(['/api/v1/checks']);
});

test('a preflight 401 clears the SLO draft and sends no create request', async ({ page }) => {
  const state = await fixture(page);
  await connect(page, '/slos?create=1&sli_id=11');
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Name', { exact: true }).fill('keep-after-preflight-denial');
  state.path = '/api/v1/slis/11';
  await dialog.getByRole('button', { name: 'Create SLO', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(dialog).toHaveCount(0);
  expect(state.writes).toEqual([]);
});

test('a Check action 401 invalidates the connection and hides protected metadata', async ({
  page,
}) => {
  const state = await fixture(page);
  await connect(page, '/checks/1');
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  state.path = '/api/v1/checks/1/pause';
  state.method = 'POST';
  await page.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.run-meta')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Pause', exact: true })).toHaveCount(0);
  expect(state.writes).toEqual(['/api/v1/checks/1/pause']);
});
