import { signIn } from './support';
import { expect, test, type Page } from './support';
async function demo(page: Page, section = 'Checks') {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: section, exact: true })
    .click();
}

test('Checks retain context, separate recorded evidence and support versioned changes', async ({
  page,
}) => {
  await demo(page);
  await page.getByLabel('Search records').fill('api');
  await page.getByRole('link', { name: 'api-health', exact: true }).click();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByText('https://api-health.example/health', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Results', exact: true }).click();
  await page.getByRole('link', { name: '#700', exact: true }).click();
  await expect(page.getByText('No response was recorded.', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: /View run/ })).toHaveCount(0);
  await page.getByRole('link', { name: 'Back to Check results' }).click();
  await page.getByRole('link', { name: 'Back to Checks' }).click();
  await expect(page.getByLabel('Search records')).toHaveValue('api');
});

test('M3 create forms preserve supplied configuration and require typed deletion', async ({
  page,
}) => {
  await demo(page);
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('new-head-check');
  await page.getByLabel('URL', { exact: true }).fill('https://service.example/ready');
  await page.getByRole('combobox', { name: 'Method', exact: true }).click();
  await page.getByRole('option', { name: 'HEAD', exact: true }).click();
  await page.getByLabel('Expected status', { exact: true }).fill('204');
  const retry = page.getByLabel('Retry limit', { exact: true });
  await retry.fill('11');
  await expect(retry).toHaveAttribute('max', '10');
  expect(await retry.evaluate((input: HTMLInputElement) => input.validity.rangeOverflow)).toBe(
    true,
  );
  await retry.fill('0');
  expect(await retry.evaluate((input: HTMLInputElement) => input.checkValidity())).toBe(true);
  await page.getByRole('button', { name: 'Create check', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'new-head-check', exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByText('HEAD', { exact: true })).toBeVisible();
  await expect(page.getByText('204', { exact: true })).toBeVisible();
  await expect(page.locator('dt', { hasText: /^Retry limit$/ }).locator('+ dd')).toHaveText('0');
  await page.getByText('Manage Check', { exact: true }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Confirm delete' })).toBeDisabled();
  await page.getByLabel('Definition name', { exact: true }).fill('new-head-check');
  await page.getByRole('button', { name: 'Confirm delete' }).click();
  await expect(page.getByRole('heading', { name: 'Checks', exact: true })).toBeVisible();
});

test('SLOs show real count semantics, zero allowance, absent data and historical windows', async ({
  page,
}) => {
  await demo(page, 'SLOs');
  await expect(page.getByRole('combobox', { name: 'Status', exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'Payments API availability', exact: true }).click();
  await expect(
    page
      .getByRole('region', { name: 'Current error budget' })
      .getByText('-23 events', { exact: true }),
  ).toBeVisible();
  await page.getByRole('tab', { name: 'History', exact: true }).click();
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByRole('tab', { name: 'Alerts', exact: true }).click();
  await page.getByRole('link', { name: 'fast_burn', exact: true }).click();
  await expect(page.getByText('15×', { exact: true })).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'SLOs', exact: true })
    .click();
  await page.getByRole('link', { name: 'Checkout readiness', exact: true }).click();
  await page.getByRole('link', { name: 'SLI #12', exact: true }).first().click();
  await expect(page.getByRole('heading', { name: 'Checkout successful probes' })).toBeVisible();
  await page.getByRole('link', { name: 'checkout-ready', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'checkout-ready', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to SLI', exact: true }).click();
  await page.getByRole('link', { name: 'Back to Objective', exact: true }).click();
  await page.getByRole('link', { name: 'Back to Objectives', exact: true }).click();
  await page.getByRole('link', { name: 'Checkout release gate', exact: true }).click();
  await expect(page.getByText('Zero allowance', { exact: true })).toBeVisible();
  await expect(page.getByText('Not comparable', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Objectives' }).click();
  await page.getByRole('link', { name: 'Gateway cutover readiness', exact: true }).click();
  await expect(page.getByText('No budget recorded', { exact: true })).toBeVisible();
});

test('SLI form rejects unsupported evaluation criteria while preserving draft', async ({
  page,
}) => {
  await demo(page, 'SLOs');
  await page.getByRole('link', { name: 'SLIs', exact: true }).click();
  await page.getByRole('button', { name: 'New SLI' }).click();
  await page.getByLabel('Name', { exact: true }).fill('latency');
  await page.getByText('Enter source ID manually', { exact: true }).click();
  await page.getByLabel('Source UID', { exact: true }).fill('check-1');
  await page.getByText('Custom criteria', { exact: true }).click();
  await page
    .getByLabel('Good-event criteria (JSON)')
    .fill('{"duration_ms":{"op":"banana","value":10}}');
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('supported operator');
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('latency');
  for (const invalid of ['{"status":["success"]}', '{"duration_ms":{"op":["<="],"value":1000}}']) {
    await page.getByLabel('Good-event criteria (JSON)').fill(invalid);
    await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page.getByRole('dialog')).toBeVisible();
  }
  await page.getByLabel('Good-event criteria (JSON)').fill('{"status":"success"}');
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'latency', exact: true })).toBeVisible();
});

test('M3 320px dark navigation and form fit', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 780 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await demo(page, 'SLOs');
  for (const name of ['Jobs', 'Runs', 'Checks', 'SLOs'])
    await expect(
      page
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name, exact: true }),
    ).toBeVisible();
  await page.getByRole('button', { name: 'New SLO', exact: true }).click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.getByLabel('Rolling window (hours)')).toHaveAttribute('min', '1');
  await expect(page.getByLabel('Target (%)')).toHaveAttribute('step', '0.01');
  await page.screenshot({ path: 'test-results/m3-mobile-dark.png', fullPage: true });
  await page.keyboard.press('Escape');
  await expect(page.getByRole('button', { name: 'New SLO', exact: true })).toBeFocused();
});

test('Version conflicts require refresh and deliberate retry; denied results retain configuration', async ({
  page,
}) => {
  let version = 1,
    status = 'active',
    writes = 0;
  const definition = {
    id: 1,
    tenant_id: 'tenant',
    name: 'conflict-check',
    check_type: 'http_health',
    execution_policy_version: 0,
    schedule_type: 'interval',
    interval_sec: 60,
    status,
    version,
    created_at: '2026-10-19T00:00:00Z',
    updated_at: '2026-10-19T00:00:00Z',
    check_config: { url: 'https://example.com' },
    labels: {},
    assertion_rules: [],
    timezone: 'UTC',
    timeout_sec: 10,
    retry_limit: 2,
    priority: 0,
  };
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/pause')) {
      writes++;
      const submitted = route.request().postDataJSON().version;
      if (writes === 1) {
        version = 2;
        return route.fulfill({
          status: 409,
          json: { error: { code: 'CONFLICT', message: 'Changed elsewhere' } },
        });
      }
      expect(submitted).toBe(2);
      version = 3;
      status = 'paused';
      return route.fulfill({ json: { id: 1, status, version } });
    }
    if (path === '/api/v1/checks/1')
      return route.fulfill({ json: { ...definition, status, version } });
    if (path === '/api/v1/checks')
      return route.fulfill({ json: { items: [{ ...definition, status, version }] } });
    if (path === '/api/v1/check-runs')
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Missing checkrun:List grant' } },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/');
  await signIn(page, 'otj_fixture_test_key');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Checks', exact: true })
    .click();
  await page.getByRole('link', { name: 'conflict-check', exact: true }).click();
  await expect(
    page.getByRole('tabpanel', { name: 'Results', exact: true }).getByRole('alert'),
  ).toContainText('Permission required');
  const controls = page.getByRole('region', { name: 'Execution controls', exact: true });
  await expect(controls.getByRole('alert')).toContainText(
    'The API response does not match the supported contract.',
  );
  await expect(controls.getByRole('switch')).toHaveCount(0);
  await expect(controls.getByRole('button', { name: 'Save controls', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByText('https://example.com', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Pause', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Confirm pause', exact: true })).toBeDisabled();
  expect(writes).toBe(1);
  await page.getByRole('button', { name: 'Refresh and review' }).click();
  await expect(page.getByRole('dialog')).toContainText('Review version 2');
  await page.getByRole('button', { name: 'Confirm pause', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Resume', exact: true })).toBeVisible();
  expect(writes).toBe(2);
});

test('SLO preflight locks submission and navigation cannot create after unmount', async ({
  page,
}) => {
  let writes = 0;
  let release!: () => void;
  const waiting = new Promise<void>((r) => {
    release = r;
  });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/slis/11') {
      await waiting;
      return route.fulfill({
        json: {
          id: 11,
          name: 'source',
          sli_type: 'availability',
          source_type: 'job_run',
          source_config: { source_uid: 'check-1' },
          aggregation: 'ratio',
          good_event_criteria: { status: 'success' },
          version: 1,
          created_at: '2026-10-19T00:00:00Z',
          updated_at: '2026-10-19T00:00:00Z',
        },
      });
    }
    if (path === '/api/v1/slos' && route.request().method() === 'POST') {
      writes++;
      return route.fulfill({ status: 201, json: { id: 22 } });
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
  await page.getByLabel('Name', { exact: true }).fill('single-intent');
  await page.getByText('Enter indicator ID manually', { exact: true }).click();
  await page.getByLabel('SLI ID', { exact: true }).fill('11');
  await page.getByRole('button', { name: 'Create SLO', exact: true }).dblclick();
  await expect(page.getByRole('button', { name: 'Creating…', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Cancel', exact: true })).toBeDisabled();
  await page.goBack();
  release();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Checks', exact: true })
    .click();
  await expect(page.getByRole('heading', { name: 'No records on this page' })).toBeVisible();
  expect(writes).toBe(0);
});

test('Deleting on Results returns to the collection before deleted detail can refetch', async ({
  page,
}) => {
  const fixture = await (await import('../src/api/demo')).demoClient().check('1');
  let deleted = false,
    evidenceReads = 0;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/checks/1') {
      if (route.request().method() === 'DELETE') {
        deleted = true;
        return route.fulfill({ json: { deleted: true } });
      }
      return route.fulfill(
        deleted
          ? { status: 404, json: { error: { code: 'NOT_FOUND', message: 'Check missing' } } }
          : { json: fixture },
      );
    }
    if (path === '/api/v1/checks')
      return route.fulfill({ json: { items: deleted ? [] : [fixture] } });
    if (path === '/api/v1/check-runs') {
      evidenceReads++;
      if (deleted) await new Promise((r) => setTimeout(r, 800));
      return route.fulfill({ json: { items: [] } });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/');
  await signIn(page, 'otj_fixture_test_key');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Checks', exact: true })
    .click();
  await page.getByRole('link', { name: fixture.name, exact: true }).click();
  await expect(page.getByRole('heading', { name: 'No recorded results' })).toBeVisible();
  await page.getByText('Manage Check', { exact: true }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.getByLabel('Definition name', { exact: true }).fill(fixture.name);
  await page.getByRole('button', { name: 'Confirm delete', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Checks', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'No records on this page' })).toBeVisible();
  expect(evidenceReads).toBe(1);
});

test('M3 deep links resume after connection and empty pages keep Previous available', async ({
  page,
}) => {
  await page.goto('/console/checks?page=2');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await expect(page.getByRole('heading', { name: 'Checks', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'No records on this page' })).toBeVisible();
  await page.getByRole('button', { name: 'Previous page', exact: true }).click();
  await expect(page.getByRole('link', { name: 'api-health', exact: true })).toBeVisible();
});
