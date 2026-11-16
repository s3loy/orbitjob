import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';
const fixtureKey = 'otj_fixture_not_a_real_key';
// Live list responses declare source_kind; the demo's internal details also carry source_mode.
function scheduledProjection<T extends object>(job: T) {
  return { ...job, source_kind: 'kubernetes' };
}
async function demo(page: Page, path = '/console/') {
  await page.goto(path);
  await page.getByRole('button', { name: 'Explore sample data' }).click();
}
async function nav(page: Page, name: 'Jobs' | 'Runs' | 'Account') {
  const target =
    name === 'Account'
      ? page.locator('.sidebar-bottom').getByRole('link', { name })
      : page
          .getByRole('navigation', { name: 'Main navigation' })
          .getByRole('link', { name, exact: true });
  await target.click();
}
async function live(page: Page) {
  await page.goto('/console/');
  await signIn(page, fixtureKey);
}
async function expectDisconnected(page: Page) {
  await expect(page.getByLabel('Password', { exact: true })).toBeVisible();
  await expect(page.locator('.topbar')).toHaveCount(0);
  await expect(page.locator('.sidebar')).toHaveCount(0);
}

test('Console follows the approved Jobs and proportional Run design', async ({ page }) => {
  const errors: string[] = [];
  const writes: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('request', (request) => {
    if (request.method() !== 'GET') writes.push(request.url());
  });
  await demo(page);
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await expect(page.locator('.topbar, .breadcrumbs')).toHaveCount(0);
  await expect(page.locator('tbody tr')).toHaveCount(11);
  await expect(page.locator('.connection-state')).toHaveText('Sample data');
  await page.screenshot({ path: 'test-results/console-jobs.png', fullPage: true });
  await page.getByRole('link', { name: 'billing-reconciliation', exact: true }).click();
  await page.getByRole('tab', { name: 'Definition', exact: true }).click();
  await expect(page.getByText('300 seconds', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Attempt 2', exact: true })).toBeVisible();
  await expect(page.getByText('Recorded span: 1m 18s', { exact: true })).toBeVisible();
  await expect(page.getByText('30s gap between attempts 1 and 2.')).toBeVisible();
  await page.getByRole('button', { name: 'Attempt 1, Failed, 24s', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Attempt 1', exact: true })).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Attempt 1, Failed, 24s', exact: true }),
  ).toBeFocused();
  await page.screenshot({ path: 'test-results/console-run.png', fullPage: true });
  expect(writes).toEqual([]);
  expect(errors).toEqual([]);
});

test('Trigger receipt and exact correlation survive page navigation', async ({ page }) => {
  await demo(page);
  await page.getByRole('button', { name: 'Trigger warehouse-sync', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Keep reviewing', exact: true })).toBeFocused();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const receipt = page.getByRole('region', { name: 'Trigger receipt for warehouse-sync' });
  await expect(receipt.getByText('Trigger accepted for warehouse-sync')).toBeVisible();
  await expect(receipt.locator('.receipt-disclosure > summary')).toBeFocused();
  await nav(page, 'Runs');
  await expect(page).toHaveURL(/\/console\/runs$/);
  const activity = page.locator('details.operation-activity');
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator(':scope > summary').click();
  const disclosure = receipt.locator('.receipt-disclosure');
  if ((await disclosure.getAttribute('open')) === null)
    await disclosure.locator(':scope > summary').click();
  await expect(receipt).toBeVisible();
  await receipt.getByRole('link', { name: 'View this job’s runs' }).click();
  await expect(page.getByRole('heading', { name: 'warehouse-sync', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/console\/jobs\/\d+$/);
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator(':scope > summary').click();
  if ((await disclosure.getAttribute('open')) === null)
    await disclosure.locator(':scope > summary').click();
  await receipt.getByRole('link', { name: 'View run', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Run #1043', exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/console\/runs\/1043$/);
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator(':scope > summary').click();
  if ((await disclosure.getAttribute('open')) === null)
    await disclosure.locator(':scope > summary').click();
  await expect(page.locator('.run-meta').getByText('Succeeded', { exact: true })).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.getByRole('heading', { name: 'Attempt 2', exact: true })).toBeVisible();
  await expect(receipt).toBeVisible();
});

test('Paused definitions cannot be triggered and search covers authorized definitions', async ({
  page,
}) => {
  await demo(page);
  await expect(
    page.getByRole('button', { name: 'Trigger backup-verification', exact: true }),
  ).toBeDisabled();
  await page.getByLabel('Search jobs').fill('warehouse');
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.getByLabel('Search jobs').fill('not-a-job');
  await expect(page.getByRole('heading', { name: 'No matching jobs' })).toBeVisible();
  await page.getByLabel('Search jobs').fill('');
  await page.getByRole('textbox', { name: 'Namespace', exact: true }).fill('finance-prod');
  await expect(page.locator('tbody tr')).toHaveCount(6);
  await expect(page.getByRole('link', { name: 'billing-extract', exact: true })).toBeVisible();
});

test('Rapid job filter changes retain both fields and follow navigation history', async ({
  page,
}) => {
  await demo(page);
  await page.getByLabel('Search jobs').fill('not-a-job');
  await expect(page.getByRole('heading', { name: 'No matching jobs' })).toBeVisible();
  await page.evaluate(() => {
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
    for (const [label, value] of [
      ['Search jobs', ''],
      ['Namespace', 'finance-prod'],
    ]) {
      const input = document.querySelector<HTMLInputElement>(`input[aria-label="${label}"]`)!;
      setValue.call(input, value);
      input.dispatchEvent(new Event('input', { bubbles: true }));
    }
  });
  await expect(page.getByLabel('Search jobs')).toHaveValue('');
  await expect(page.getByRole('textbox', { name: 'Namespace', exact: true })).toHaveValue(
    'finance-prod',
  );
  await expect(page.locator('tbody tr')).toHaveCount(6);
  expect(new URL(page.url()).searchParams.get('search')).toBeNull();
  expect(new URL(page.url()).searchParams.get('namespace')).toBe('finance-prod');

  await nav(page, 'Runs');
  await page.goBack();
  await expect(page.getByRole('textbox', { name: 'Namespace', exact: true })).toHaveValue(
    'finance-prod',
  );
  await page.getByLabel('Search jobs').fill('billing');
  await expect(page.locator('tbody tr')).toHaveCount(4);
  expect(new URL(page.url()).searchParams.get('namespace')).toBe('finance-prod');
  await page.goForward();
  await expect(page.getByRole('heading', { name: 'Runs', exact: true })).toBeVisible();
});

test('Cancellation receipt is distinct from the later observed phase', async ({ page }) => {
  await demo(page, '/console/runs/1042');
  await expect(page.getByRole('heading', { name: 'Run #1042', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
  await expect(page.locator('.run-meta').getByText('Retry waiting', { exact: true })).toBeVisible();
  await expect(page.locator('.operation-notice[tabindex]')).toBeFocused();
  await expect(page.locator('.run-meta').getByText('Canceled', { exact: true })).toBeVisible({
    timeout: 10_000,
  });
  await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toHaveCount(0);
});

for (const width of [320, 390])
  test(`Console fits ${width}px, supports dark appearance and clears connection data`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto('/console/');
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/console\/connection$/);
    await page.keyboard.press('Tab');
    await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
    await page.getByRole('button', { name: 'Explore sample data' }).click();
    await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await nav(page, 'Runs');
    await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Attempts', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({ path: `test-results/console-mobile-${width}.png`, fullPage: true });
    await page
      .locator('.sidebar-bottom')
      .getByRole('link', { name: 'Workspace', exact: true })
      .click();
    await page.getByRole('button', { name: 'Leave sample data' }).click();
    await expectDisconnected(page);
    expect(await page.evaluate(() => [localStorage.length, sessionStorage.length])).toEqual([0, 0]);
  });

for (const status of [401, 403, 429, 500])
  test(`Live ${status} stays recoverable without Demo fallback or credential persistence`, async ({
    page,
  }) => {
    let calls = 0;
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error' && message.text().includes('cannot'))
        consoleErrors.push(message.text());
    });
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      calls++;
      return route.fulfill({
        status,
        contentType: 'application/json',
        headers: status === 429 ? { 'Retry-After': '180' } : {},
        body: JSON.stringify({
          error: { code: 'FIXTURE_ERROR', message: 'Fixture request denied' },
        }),
      });
    });
    await live(page);
    await expect(page.getByRole('alert').first()).toBeVisible();
    await expectDisconnected(page);
    await expect(page.locator('.connection-state')).toHaveCount(0);
    if (status === 429) {
      await expect(page.getByRole('button', { name: 'Wait before signing in' })).toBeDisabled();
      expect(calls).toBe(1);
    }
    expect(consoleErrors).toEqual([]);
    expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
      fixtureKey,
    );
    await page.reload();
    await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
    await expectDisconnected(page);
  });

test('HTTP 200 with ignored filters never links an unrelated occurrence', async ({ page }) => {
  const api = demoClient();
  const jobs = (await api.jobs()).map(scheduledProjection);
  const unrelated = (await api.runs())[0];
  await page.route('**/openapi.json', (route) =>
    route.fulfill({
      json: {
        paths: {
          '/api/v1/instances': {
            get: {
              parameters: ['source_uid', 'occurrence_key'].map((name) => ({ name, in: 'query' })),
            },
          },
        },
      },
    }),
  );
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    if (route.request().url().includes('/trigger'))
      return route.fulfill({
        json: {
          namespace: 'data-pipelines',
          name: 'accepted-run',
          occurrence_key: 'expected-occurrence',
          phase: '',
          trigger: 'Manual',
          created: true,
        },
      });
    return route.fulfill({
      json: { items: route.request().url().includes('/jobs?') ? jobs : [unrelated] },
    });
  });
  await live(page);
  await page.getByRole('button', { name: 'Trigger warehouse-sync', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger' }).click();
  await expect(page.getByText('Automatic linking is disabled.', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: 'View run', exact: true })).toHaveCount(0);
  api.dispose();
});

test('A failed trigger retains its intent after closing and navigating', async ({ page }) => {
  const api = demoClient();
  const jobs = (await api.jobs()).map(scheduledProjection);
  const keys: string[] = [];
  await page.route('**/openapi.json', (route) => route.fulfill({ json: { paths: {} } }));
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = route.request().url();
    if (new URL(url).pathname.endsWith('/trigger')) {
      keys.push(route.request().headers()['x-orbitjob-idempotency-key']);
      return keys.length === 1
        ? route.fulfill({
            status: 503,
            json: { error: { code: 'UNAVAILABLE', message: 'Trigger outcome not confirmed' } },
          })
        : route.fulfill({
            json: {
              namespace: 'finance-prod',
              name: 'accepted-run',
              occurrence_key: keys[0],
              phase: '',
              trigger: 'Manual',
              created: false,
            },
          });
    }
    return route.fulfill({ json: { items: url.includes('/jobs?') ? jobs : [] } });
  });
  await live(page);
  await page.getByRole('button', { name: 'Trigger billing-reconciliation', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger' }).click();
  await expect(page.getByRole('button', { name: 'Retry same trigger' })).toBeVisible();
  await page.keyboard.press('Escape');
  await nav(page, 'Runs');
  await expect(page).toHaveURL(/\/console\/runs$/);
  await nav(page, 'Jobs');
  await expect(page).toHaveURL(/\/console\/jobs$/);
  await page.getByRole('button', { name: 'Trigger billing-reconciliation', exact: true }).click();
  await page.getByRole('button', { name: 'Retry same trigger' }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const activity = page.locator('details.operation-activity');
  if ((await activity.getAttribute('open')) === null)
    await activity.locator(':scope > summary').click();
  const receipt = activity.getByRole('region', {
    name: 'Trigger receipt for billing-reconciliation',
  });
  const disclosure = receipt.locator('.receipt-disclosure');
  if ((await disclosure.getAttribute('open')) === null)
    await disclosure.locator(':scope > summary').click();
  await expect(page.getByText('Trigger accepted for billing-reconciliation')).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[1]).toBe(keys[0]);
  api.dispose();
});

test('Disconnect prevents a late mutation result from recreating its receipt', async ({ page }) => {
  const api = demoClient();
  const jobs = (await api.jobs()).map(scheduledProjection);
  let release!: () => void;
  let started!: () => void;
  let finished!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const observed = new Promise<void>((resolve) => {
    started = resolve;
  });
  const settled = new Promise<void>((resolve) => {
    finished = resolve;
  });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    if (new URL(route.request().url()).pathname.endsWith('/trigger')) {
      started();
      await gate;
      await route
        .fulfill({
          json: {
            namespace: 'finance-prod',
            name: 'late-run',
            occurrence_key: 'late',
            phase: '',
            trigger: 'Manual',
            created: true,
          },
        })
        .catch(() => {});
      finished();
      return;
    }
    await route.fulfill({ json: { items: jobs } });
  });
  await live(page);
  await page.getByRole('button', { name: 'Trigger billing-reconciliation', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger' }).click();
  await observed;
  await page.keyboard.press('Escape');
  await nav(page, 'Account');
  await page.getByRole('button', { name: 'Sign out' }).click();
  release();
  await settled;
  await expectDisconnected(page);
  await expect(page.getByRole('region', { name: /Trigger receipt/ })).toHaveCount(0);
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
  api.dispose();
});

test('Cancel restarts bounded observation after the initial window has paused', async ({
  page,
}) => {
  const api = demoClient();
  const run = await api.run(1042);
  const job = scheduledProjection(await api.job(run.revision_id));
  let cancelled = false;
  let reads = 0;
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/cancel')) {
      cancelled = true;
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'fixture-run',
          occurrence_key: run.occurrence_key,
          phase: run.phase,
        },
      });
    }
    if (path.includes('/instances/')) {
      reads++;
      return route.fulfill({
        json: { ...run, phase: cancelled ? 'CancelUnknown' : 'RetryWaiting' },
      });
    }
    if (path.includes('/jobs/')) return route.fulfill({ json: job });
    return route.fulfill({ json: { items: path.includes('/instances') ? [run] : [job] } });
  });
  await page.clock.install();
  await live(page);
  await nav(page, 'Runs');
  await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Run #1042', exact: true })).toBeVisible();
  await expect(page.locator('.run-meta').getByText('Retry waiting', { exact: true })).toBeVisible();
  await page.clock.fastForward(121_000);
  await expect(page.getByText('Observation paused. Refresh to continue.')).toBeVisible();
  const pausedReads = reads;
  await page.context().setOffline(true);
  await page.context().setOffline(false);
  await page.clock.runFor(3000);
  expect(reads).toBe(pausedReads);
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
  await expect(
    page.locator('.run-meta').getByText('Stop unconfirmed', { exact: true }),
  ).toBeVisible();
  const afterCancel = reads;
  await page.clock.fastForward(4000);
  await expect.poll(() => reads).toBeGreaterThan(afterCancel);
  await expect(page.getByText('The stop could not be confirmed.', { exact: false })).toBeVisible();
  await nav(page, 'Runs');
  await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Cancel requested', exact: true })).toBeDisabled();
  api.dispose();
});

test('Preparing a later trigger retains every previously accepted receipt', async ({ page }) => {
  await demo(page);
  const trigger = page.getByRole('button', { name: 'Trigger warehouse-sync', exact: true });
  const receipts = page.getByRole('region', {
    name: 'Trigger receipt for warehouse-sync',
    exact: true,
    includeHidden: true,
  });
  await trigger.click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await expect(receipts).toHaveCount(1);
  await trigger.click();
  await page.getByRole('button', { name: 'Prepare another trigger', exact: true }).click();
  await page.getByRole('button', { name: 'Keep reviewing', exact: true }).click();
  await expect(receipts).toHaveCount(1);
  await trigger.click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await expect(receipts).toHaveCount(2);
  const earlier = page.locator('.trigger-receipt-list > [data-history="true"]');
  const latest = page.locator('.trigger-receipt-list > [data-history="false"]');
  await expect(earlier.locator('.receipt-disclosure')).not.toHaveAttribute('open');
  await expect(latest.locator('.receipt-disclosure')).toHaveAttribute('open');
  await page.getByRole('button', { name: 'Show earlier receipts (1)', exact: true }).click();
  await earlier.locator('.receipt-disclosure > summary').click();
  await expect(earlier.getByRole('link', { name: 'View this job’s runs' })).toBeVisible();
  await nav(page, 'Runs');
  await expect(receipts).toHaveCount(2);
});

test('Try again starts a fresh observation after an initial error exceeds the budget', async ({
  page,
}) => {
  const api = demoClient();
  const run = await api.run(1042);
  const job = scheduledProjection(await api.job(run.revision_id));
  let available = false;
  let reads = 0;
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/instances/summary')
      return route.fulfill({ json: { total: 1, phases: { RetryWaiting: 1 } } });
    if (path.includes('/instances/')) {
      reads++;
      return available
        ? route.fulfill({ json: run })
        : route.fulfill({
            status: 500,
            json: { error: { code: 'UNAVAILABLE', message: 'Run read temporarily unavailable' } },
          });
    }
    if (path.includes('/jobs/')) return route.fulfill({ json: job });
    return route.fulfill({ json: { items: path.includes('/instances') ? [run] : [job] } });
  });
  await page.clock.install();
  await live(page);
  await nav(page, 'Runs');
  await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
  await expect(page).toHaveURL(/\/runs\/1042$/);
  await expect(page.getByRole('link', { name: 'Back to Runs', exact: true })).toBeVisible();
  await expect(
    page.getByRole('alert').filter({ hasText: 'Run read temporarily unavailable' }),
  ).toBeVisible();
  await page.clock.fastForward(121_000);
  available = true;
  const previous = reads;
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Run #1042', exact: true })).toBeVisible();
  expect(reads).toBeGreaterThan(previous);
  api.dispose();
});

test('Jobs retain server search and pagination through details and browser history', async ({
  page,
}) => {
  const api = demoClient();
  const sample = scheduledProjection(await api.job(17));
  const jobs = Array.from({ length: 41 }, (_, index) => ({
    ...sample,
    id: index + 1,
    name: `fixture-job-${index + 1}`,
    namespace: 'finance-prod',
  }));
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    const id = url.pathname.match(/\/jobs\/(\d+)$/)?.[1];
    if (id) return route.fulfill({ json: jobs.find((job) => String(job.id) === id) });
    const offset = Number(url.searchParams.get('offset') ?? 0);
    const search = url.searchParams.get('search') ?? '';
    return route.fulfill({
      json: {
        items: jobs
          .filter((j) => `${j.name} ${j.namespace}`.includes(search))
          .slice(offset, offset + 20),
      },
    });
  });
  await live(page);
  await page.getByLabel('Search jobs').fill('fixture-job-');
  await expect(page).toHaveURL(/search=fixture-job-/);
  await page.getByRole('link', { name: 'fixture-job-1', exact: true }).click();
  await page.getByRole('heading', { name: 'fixture-job-1', exact: true }).waitFor();
  await page.goBack();
  await expect(page.getByLabel('Search jobs')).toHaveValue('fixture-job-');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'fixture-job-21', exact: true }).click();
  await page.getByRole('heading', { name: 'fixture-job-21', exact: true }).waitFor();
  await page.goBack();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Search jobs')).toHaveValue('fixture-job-');
  await page.getByLabel('Search jobs').fill('fixture-job-41');
  await expect(page).not.toHaveURL(/page=2|offset=20/);
  await expect(page.getByRole('link', { name: 'fixture-job-41', exact: true })).toBeVisible();
  api.dispose();
});

test('Runs restore page and phase through browser back and forward', async ({ page }) => {
  await demo(page, '/console/runs');
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'View run 1022', exact: true }).click();
  await page.getByRole('heading', { name: 'Run #1022', exact: true }).waitFor();
  await page.goBack();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Run phase' }).click();
  await page.getByRole('option', { name: 'Failed', exact: true }).click();
  await expect(page.getByText('Page 1', { exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await expect(page.getByRole('combobox', { name: 'Run phase' })).toHaveText('All phases');
  await page.goForward();
  await expect(page.getByRole('combobox', { name: 'Run phase' })).toHaveText('Failed');
  await expect(page.getByText('Page 1', { exact: true })).toBeVisible();
});

test('Job tabs and scoped Run pagination survive navigation', async ({ page }) => {
  const api = demoClient();
  const job = scheduledProjection(await api.job(17));
  const sample = await api.run(1042);
  const runs = Array.from({ length: 21 }, (_, i) => ({
    ...sample,
    id: 1042 - i,
    source_uid: job.source_uid,
    revision_id: job.id,
  }));
  await page.route('**/openapi.json', (route) =>
    route.fulfill({
      json: {
        paths: {
          '/api/v1/instances': {
            get: {
              parameters: ['source_uid', 'occurrence_key'].map((name) => ({ name, in: 'query' })),
            },
          },
        },
      },
    }),
  );
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname.includes('/jobs/')) return route.fulfill({ json: job });
    const id = url.pathname.match(/\/instances\/(\d+)$/)?.[1];
    if (id) return route.fulfill({ json: runs.find((run) => String(run.id) === id) });
    const offset = Number(url.searchParams.get('offset') ?? 0);
    return route.fulfill({
      json: { items: url.pathname.includes('/jobs') ? [job] : runs.slice(offset, offset + 20) },
    });
  });
  await live(page);
  await page.getByRole('link', { name: job.name, exact: true }).click();
  await page.getByRole('tab', { name: 'Definition', exact: true }).click();
  await nav(page, 'Jobs');
  await page.goBack();
  await expect(page.getByRole('tab', { name: 'Definition', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'View run 1022', exact: true }).click();
  await page.getByRole('heading', { name: 'Run #1022', exact: true }).waitFor();
  await page.goBack();
  await expect(page.getByText('Page 2', { exact: true })).toBeVisible();
  api.dispose();
});

test('A successful retry receives a fresh acceptance observation budget', async ({ page }) => {
  const api = demoClient();
  const job = scheduledProjection(await api.job(17));
  const run = await api.run(1042);
  let triggers = 0;
  let occurrence = '';
  await page.route('**/openapi.json', (route) =>
    route.fulfill({
      json: {
        paths: {
          '/api/v1/instances': {
            get: {
              parameters: ['source_uid', 'occurrence_key'].map((name) => ({ name, in: 'query' })),
            },
          },
        },
      },
    }),
  );
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname.endsWith('/trigger')) {
      triggers++;
      occurrence = route.request().headers()['x-orbitjob-idempotency-key'];
      return triggers === 1
        ? route.fulfill({
            status: 503,
            json: { error: { code: 'UNAVAILABLE', message: 'Trigger not confirmed' } },
          })
        : route.fulfill({
            json: {
              namespace: job.namespace,
              name: 'accepted-after-wait',
              occurrence_key: occurrence,
              phase: '',
              trigger: 'Manual',
              created: true,
            },
          });
    }
    return route.fulfill({
      json: {
        items: url.pathname.includes('/jobs') ? [job] : [{ ...run, occurrence_key: occurrence }],
      },
    });
  });
  await page.clock.install();
  await live(page);
  await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Retry same trigger' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.clock.fastForward(121_000);
  await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Retry same trigger' }).click();
  await expect(page.getByRole('link', { name: 'View run', exact: true })).toBeVisible();
  await expect(
    page.getByText('Observation paused. No matching Run record has been confirmed.'),
  ).toHaveCount(0);
  expect(triggers).toBe(2);
  api.dispose();
});

test('A prior failed trigger cannot collapse a newer accepted receipt', async ({ page }) => {
  const api = demoClient();
  const jobs = (await api.jobs()).map(scheduledProjection);
  await page.route('**/openapi.json', (route) => route.fulfill({ json: { paths: {} } }));
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/17/trigger'))
      return route.fulfill({
        status: 503,
        json: { error: { code: 'UNAVAILABLE', message: 'Fixture failure' } },
      });
    if (path.endsWith('/trigger'))
      return route.fulfill({
        json: {
          namespace: jobs[1].namespace,
          name: 'newer-accepted',
          occurrence_key: 'later-intent',
          phase: '',
          trigger: 'Manual',
          created: true,
        },
      });
    return route.fulfill({ json: { items: jobs } });
  });
  await live(page);
  await page.getByRole('button', { name: 'Trigger billing-reconciliation', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Retry same trigger' })).toBeVisible();
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Trigger warehouse-sync', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const accepted = page.getByRole('region', {
    name: 'Trigger receipt for warehouse-sync',
    exact: true,
  });
  await expect(accepted.getByRole('link', { name: 'View this job’s runs' })).toBeVisible();
  await expect(accepted.locator('.receipt-disclosure')).toHaveAttribute('open');
  await expect(
    page
      .getByRole('region', { name: 'Trigger receipt for billing-reconciliation' })
      .locator('.receipt-disclosure'),
  ).toHaveAttribute('open');
  api.dispose();
});

for (const status of [401, 500])
  test(`A stopped ${status} observation is described accurately`, async ({ page }) => {
    const api = demoClient();
    const run = await api.run(1042);
    const job = scheduledProjection(await api.job(17));
    let reads = 0;
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/instances/summary')
        return route.fulfill({ json: { total: 1, phases: { RetryWaiting: 1 } } });
      if (path.includes('/logs/archive'))
        return route.fulfill({
          status: 404,
          json: {
            error: {
              code: 'NOT_FOUND',
              message: 'Archive not registered in this snapshot fixture',
            },
          },
        });
      if (path.includes('/instances/')) {
        reads++;
        return reads === 1
          ? route.fulfill({ json: run })
          : route.fulfill({
              status,
              json: { error: { code: 'FIXTURE_ERROR', message: 'Observation denied' } },
            });
      }
      if (path.includes('/jobs/')) return route.fulfill({ json: job });
      return route.fulfill({ json: { items: path.includes('/jobs') ? [job] : [run] } });
    });
    await page.clock.install();
    await live(page);
    await nav(page, 'Runs');
    await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
    await expect(page.getByText('Checking for updates')).toBeVisible();
    await page.clock.runFor(2500);
    if (status === 401) {
      await expect(page.getByRole('alert')).toContainText('Your session has ended.');
      await expect(page.getByRole('region', { name: 'Attempt timeline' })).toHaveCount(0);
      await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toHaveCount(
        0,
      );
      await expect(
        page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
      ).toBeVisible();
    } else {
      await expect(page.getByText('Observation stopped. Refresh to retry.')).toBeVisible();
      await expect(
        page.getByRole('status').filter({ hasText: 'Last observed snapshot.' }),
      ).toHaveText(
        'Last observed snapshot. The latest read failed; these details do not confirm the current execution state. Refresh successfully before requesting cancellation.',
      );
      await expect(page.getByRole('region', { name: 'Attempt timeline' })).toBeVisible();
      await expect(
        page.getByRole('button', { name: 'Request cancel', exact: true }),
      ).toBeDisabled();
    }
    const stopped = reads;
    await page.clock.runFor(6000);
    expect(reads).toBe(stopped);
    await expect(page.getByText('Checking for updates')).toHaveCount(0);
    api.dispose();
  });

test('Narrow Runs keep the owning Job visible and empty namespace URLs stay usable', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 844 });
  await demo(page, '/console/jobs?namespace=');
  await expect(page.locator('tbody tr')).toHaveCount(11);
  await expect(page.getByRole('textbox', { name: 'Namespace', exact: true })).toHaveValue('');
  await nav(page, 'Runs');
  await expect(
    page
      .locator('tbody tr')
      .first()
      .getByText('billing-reconciliation (finance-prod)', { exact: true }),
  ).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('Closing a previous accepted trigger restores focus when its receipt is hidden', async ({
  page,
}) => {
  await demo(page);
  for (const job of ['warehouse-sync', 'daily-digest']) {
    await page.getByRole('button', { name: `Trigger ${job}`, exact: true }).click();
    await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
  const trigger = page.getByRole('button', { name: 'Trigger warehouse-sync', exact: true });
  await trigger.click();
  await expect(page.getByRole('heading', { name: 'Trigger already accepted' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused();
  await page.getByRole('button', { name: 'Show earlier receipts (1)', exact: true }).click();
  const names = await page
    .locator('.trigger-receipt-list > section')
    .evaluateAll((elements) => elements.map((e) => e.getAttribute('aria-label')));
  expect(names).toEqual(['Trigger receipt for daily-digest', 'Trigger receipt for warehouse-sync']);
});
