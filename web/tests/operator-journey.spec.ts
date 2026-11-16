import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function connect(page: Page, path: string) {
  await page.route('**/openapi.json', (route) => route.fulfill({ json: { paths: {} } }));
  await page.goto(path);
  await signIn(page, 'operator_journey_fixture');
}
test('mixed execution projections expose only their own actions and preserve Jobs return context', async ({
  page,
}) => {
  const fixture = demoClient();
  const base = await fixture.job(17);
  const fn = await fixture.functionDefinition('27');
  fixture.dispose();
  const rows = [
    { ...base, source_kind: 'kubernetes' },
    {
      ...base,
      id: 50,
      name: 'function-27',
      source_uid: 'function-27',
      source_kind: 'function',
      source_mode: 'function',
    },
    { ...base, id: 51, name: 'nightly-flow', source_kind: 'workflow', source_mode: 'workflow' },
    {
      ...base,
      id: 52,
      name: 'unrecognized-definition',
      source_kind: 'future',
      source_mode: 'future',
    },
  ];
  let triggers = 0;
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path.endsWith('/trigger')) {
      triggers++;
      return route.fulfill({ status: 500, json: {} });
    }
    if (path === '/api/v1/jobs') return route.fulfill({ json: { items: rows } });
    if (path === '/api/v1/jobs/50') return route.fulfill({ json: rows[1] });
    if (path === '/api/v1/jobs/17') return route.fulfill({ json: base });
    if (path === '/api/v1/functions/27') return route.fulfill({ json: fn });
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/jobs?search=billing&namespace=finance-prod&offset=20');
  await expect(page.getByRole('button', { name: /^Trigger / })).toHaveCount(1);
  await expect(page.getByRole('link', { name: 'Open Function', exact: true })).toHaveAttribute(
    'href',
    '/console/functions/27',
  );
  await expect(page.getByRole('link', { name: 'Open Workflow', exact: true })).toHaveAttribute(
    'href',
    '/console/workflows/51',
  );
  await page.getByRole('link', { name: base.name, exact: true }).click();
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await expect(page.getByLabel('Search jobs', { exact: true })).toHaveValue('billing');
  await expect(page.getByLabel('Namespace', { exact: true })).toHaveValue('finance-prod');
  await expect(page).toHaveURL(/offset=20/);
  await connect(page, '/console/jobs/50');
  await expect(page.getByRole('link', { name: 'Open Function', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Trigger / })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Edit definition', exact: true })).toHaveCount(0);
  expect(triggers).toBe(0);
});

test('SLI source type changes clear stale identities before a confirmed source is saved', async ({
  page,
}) => {
  const fixture = demoClient();
  const base = await fixture.job(17);
  const check = await fixture.check('1');
  const indicator = await fixture.sli('11');
  fixture.dispose();
  let submitted: Record<string, unknown> | undefined;
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/jobs')
      return route.fulfill({ json: { items: [{ ...base, source_kind: 'kubernetes' }] } });
    if (path === '/api/v1/checks') return route.fulfill({ json: { items: [check] } });
    if (path === '/api/v1/checks/1') return route.fulfill({ json: check });
    if (path === '/api/v1/slis' && route.request().method() === 'POST') {
      submitted = route.request().postDataJSON();
      return route.fulfill({ json: { id: 111 } });
    }
    if (path === '/api/v1/slis/111')
      return route.fulfill({ json: { ...indicator, ...submitted, id: 111 } });
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/slis?create=1&search=availability');
  await page.getByLabel('Name', { exact: true }).fill('confirmed-source');
  await page.locator('select[aria-label="Monitoring source"]').selectOption(base.source_uid);
  await page.getByLabel('Source type', { exact: true }).selectOption('checks');
  await expect(page.locator('select[aria-label="Monitoring source"]')).toHaveValue('');
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Choose a monitoring source');
  expect(submitted).toBeUndefined();
  await page.locator('select[aria-label="Monitoring source"]').selectOption('check-1');
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'confirmed-source', exact: true })).toBeVisible();
  expect(submitted?.source_config).toEqual({ source_uid: 'check-1' });
  await expect(page.getByRole('link', { name: 'api-health', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to SLIs', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByLabel('Search records')).toHaveValue('availability');
  await expect(page).not.toHaveURL(/create=1/);
});

test('a confirmed monitoring write retains its ID when its detail read is forbidden', async ({
  page,
}) => {
  const fixture = demoClient();
  const check = await fixture.check('1');
  fixture.dispose();
  let writes = 0;
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/checks/1') return route.fulfill({ json: check });
    if (path === '/api/v1/checks') return route.fulfill({ json: { items: [check] } });
    if (path === '/api/v1/slis' && route.request().method() === 'POST') {
      writes++;
      return route.fulfill({ json: { id: '9007199254740993' } });
    }
    if (path === '/api/v1/slis/9007199254740993')
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'SLI Get denied' } },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/slis?create=1&source_uid=check-1');
  await page.getByLabel('Name', { exact: true }).fill('saved-without-get');
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  const receipt = page.locator('main > section[aria-label="Monitoring creation receipts"]');
  await expect(receipt).toContainText('Created saved-without-get');
  await expect(receipt).toContainText('SLI #9007199254740993');
  await expect(page.getByRole('alert')).toContainText('SLI Get denied');
  await expect(
    page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }),
  ).toHaveAttribute('href', '/console/connection');
  expect(writes).toBe(1);
});

test('SLO creation consumes its intent and Back restores the objective list filters', async ({
  page,
}) => {
  const fixture = demoClient();
  const sli = await fixture.sli('11');
  const slo = await fixture.slo('21');
  const budget = await fixture.budget('21');
  fixture.dispose();
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/slis') return route.fulfill({ json: { items: [sli] } });
    if (path === '/api/v1/slis/11') return route.fulfill({ json: sli });
    if (path === '/api/v1/slos' && route.request().method() === 'POST')
      return route.fulfill({ json: { id: 121 } });
    if (path === '/api/v1/slos/121')
      return route.fulfill({ json: { ...slo, id: 121, name: 'intent-once' } });
    if (path === '/api/v1/slos/121/budget') return route.fulfill({ json: budget });
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/slos?create=1&sli_id=11&search=availability&offset=20');
  await page.getByLabel('Name', { exact: true }).fill('intent-once');
  await page.getByRole('button', { name: 'Create SLO', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'intent-once', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Objectives', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByLabel('Search records')).toHaveValue('availability');
  await expect(page).toHaveURL(/offset=20/);
  await expect(page).not.toHaveURL(/create=1/);
});

test('Alerts rejects an invalid credential without pretending the collection is empty and guides replacement', async ({
  page,
}) => {
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/slo-alerts')
      return route.fulfill({
        status: 401,
        json: { error: { code: 'UNAUTHORIZED', message: 'Credential rejected' } },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/slo-alerts');
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.sidebar')).toHaveCount(0);
  await expect(page.getByText('No records on this page', { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
});
