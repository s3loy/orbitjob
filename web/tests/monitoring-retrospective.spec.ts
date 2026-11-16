import { signIn } from './support';
import { expect, test } from './support';
import { demoClient } from '../src/api/demo';

test('Scheduling labels and every related-record return retain the original context', async ({
  page,
}) => {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  const nav = page.getByRole('navigation', { name: 'Main navigation' });
  await nav.getByRole('link', { name: 'Checks', exact: true }).click();
  await expect(page.getByRole('columnheader', { name: 'Scheduling', exact: true })).toBeVisible();
  await expect(page.getByRole('row', { name: /gateway-health/ })).toContainText('Not scheduled');
  await page.getByRole('link', { name: 'gateway-health', exact: true }).click();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(
    page.locator('.fact-pair').filter({ has: page.locator('dt', { hasText: /^Next scheduled$/ }) }),
  ).toHaveText('Next scheduledNot scheduled');
  await nav.getByRole('link', { name: 'Checks', exact: true }).click();
  await page.getByLabel('Search records').fill('api');
  await page.getByRole('link', { name: 'api-health', exact: true }).click();
  await page.getByRole('link', { name: 'Back to Checks', exact: true }).click();
  await expect(page.getByLabel('Search records')).toHaveValue('api');
  await page.getByRole('link', { name: 'api-health', exact: true }).click();
  await nav.getByRole('link', { name: 'Checks', exact: true }).click();
  await expect(page.getByLabel('Search records')).toHaveValue('api');

  await nav.getByRole('link', { name: 'SLOs', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Service objectives', exact: true }),
  ).toBeVisible();
  await page.getByLabel('Search records').fill('API');
  await page.getByRole('link', { name: 'Payments API availability', exact: true }).click();
  await page.getByRole('tab', { name: 'History', exact: true }).click();
  await page.getByRole('link', { name: 'SLI #11', exact: true }).first().click();
  await page.getByRole('link', { name: 'api-health', exact: true }).click();
  await page.getByRole('combobox', { name: 'Result status', exact: true }).click();
  await page.getByRole('option', { name: 'Failed', exact: true }).click();
  await page.getByRole('link', { name: '#701', exact: true }).click();
  // The related Check link has the same recovery behavior as explicit Back.
  await page.getByRole('link', { name: 'Check #1', exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Result status' })).toHaveText('Failed');
  await page.getByRole('link', { name: 'Back to SLI', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Payments API readiness', exact: true }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Back to Objective', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'History', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('link', { name: 'Back to Objectives', exact: true }).click();
  await expect(page.getByLabel('Search records')).toHaveValue('API');
});

test('Failed SLI preflight keeps a retryable draft; an uncertain POST blocks duplicate submission', async ({
  page,
}) => {
  const fixture = demoClient();
  let preflights = 0;
  let writes = 0;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/slis/11') {
      preflights++;
      if (preflights === 1)
        return route.fulfill({
          status: 503,
          json: { error: { code: 'INTERNAL', message: 'Preflight unavailable' } },
        });
      return route.fulfill({ json: await fixture.sli('11') });
    }
    if (path === '/api/v1/slos' && route.request().method() === 'POST') {
      writes++;
      return route.fulfill({
        status: 503,
        json: { error: { code: 'INTERNAL', message: 'Creation outcome unknown' } },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/');
  await signIn(page, 'otj_fixture_test_key');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'SLOs', exact: true })
    .click();
  await page.getByRole('button', { name: 'New SLO', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('retained-draft');
  await page.getByText('Enter indicator ID manually', { exact: true }).click();
  await page.getByLabel('SLI ID', { exact: true }).fill('11');
  const create = page.getByRole('button', { name: 'Create SLO', exact: true });
  await create.click();
  await expect(page.getByRole('alert')).toContainText('Preflight unavailable');
  await expect(create).toBeEnabled();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('retained-draft');
  await expect(page.getByText('Creation was not confirmed.', { exact: false })).toHaveCount(0);
  expect(writes).toBe(0);
  await create.click();
  await expect(page.getByRole('alert')).toContainText('Creation outcome unknown');
  await expect(create).toBeDisabled();
  await expect(page.getByText('Creation was not confirmed.', { exact: false })).toBeVisible();
  expect(writes).toBe(1);
});

test('Denied related definitions retain a return path and at-risk budgets use the warning state', async ({
  page,
}) => {
  const fixture = demoClient();
  let sourceReads = 0;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/slos') return route.fulfill({ json: { items: await fixture.slos() } });
    if (path === '/api/v1/slos/21') return route.fulfill({ json: await fixture.slo('21') });
    if (path === '/api/v1/slos/21/budget')
      return route.fulfill({ json: { ...(await fixture.budget('21')), status: 'at_risk' } });
    if (path === '/api/v1/slis/11') return route.fulfill({ json: await fixture.sli('11') });
    if (path === '/api/v1/checks/1') {
      if (++sourceReads === 1) return route.fulfill({ json: await fixture.check('1') });
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Source definition unavailable' } },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/');
  await signIn(page, 'otj_fixture_test_key');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'SLOs', exact: true })
    .click();
  await page.getByRole('link', { name: 'Payments API availability', exact: true }).click();
  await expect(
    page.getByRole('region', { name: 'Current error budget' }).locator('.phase-label'),
  ).toHaveClass(/warning/);
  await page.getByRole('link', { name: 'SLI #11', exact: true }).first().click();
  await page.getByRole('link', { name: 'api-health', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  expect(sourceReads).toBeGreaterThanOrEqual(2);
  await page.getByRole('link', { name: 'Back to SLI', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Payments API readiness', exact: true }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Back to Objective', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Payments API availability', exact: true }),
  ).toBeVisible();
});

test('Missing direct result links name their fallback collection accurately', async ({ page }) => {
  await page.goto('/console/check-results/9999');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await expect(page.getByRole('alert')).toBeVisible();
  await page.getByRole('link', { name: 'Back to Checks', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Checks', exact: true })).toBeVisible();
});
