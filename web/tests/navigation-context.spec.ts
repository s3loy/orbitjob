import { expect, test, type Page } from './support';

async function demo(page: Page, path = '/console/') {
  await page.goto(path);
  await page.getByRole('button', { name: 'Explore sample data' }).click();
}

async function navigate(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

for (const resource of [
  {
    nav: 'Jobs',
    search: 'Search jobs',
    value: 'billing',
    create: 'Create a job',
    path: 'manifests/job',
  },
  {
    nav: 'Workflows',
    search: 'Search workflows',
    value: 'billing',
    create: 'Create a workflow',
    path: 'manifests/workflow',
  },
  {
    nav: 'Functions',
    search: 'Search functions',
    value: 'rebuild',
    create: 'Create a function',
    path: 'functions/new',
  },
])
  test(`${resource.nav} creation has a working Back link and retains list search`, async ({
    page,
  }) => {
    await demo(page);
    await navigate(page, resource.nav);
    await page.getByLabel(resource.search, { exact: true }).fill(resource.value);
    await expect(page).toHaveURL(new RegExp(`search=${resource.value}`));
    const collection = page.url();
    await page.getByRole('link', { name: resource.create, exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/console/${resource.path}$`));
    const parent = page.getByRole('link', { name: `Back to ${resource.nav}`, exact: true });
    await expect(parent).toHaveAttribute(
      'href',
      new URL(collection).pathname + new URL(collection).search,
    );
    await expect(
      page
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name: resource.nav, exact: true }),
    ).toHaveAttribute('aria-current', 'page');
    await parent.click();
    await expect(page).toHaveURL(collection);
    await expect(page.getByLabel(resource.search, { exact: true })).toHaveValue(resource.value);
    await expect(page.getByRole('heading', { name: 'Page not found' })).toHaveCount(0);
  });

for (const resource of [
  { nav: 'Workflows', search: 'Search workflows', value: 'billing', record: 'billing-close' },
  {
    nav: 'Functions',
    search: 'Search functions',
    value: 'rebuild',
    record: 'rebuild-search-index',
  },
])
  test(`${resource.nav} detail Back link and sidebar retain search`, async ({ page }) => {
    await demo(page);
    await navigate(page, resource.nav);
    await page.getByLabel(resource.search, { exact: true }).fill(resource.value);
    await expect(page).toHaveURL(new RegExp(`search=${resource.value}`));
    const collection = page.url();
    await page.getByRole('link', { name: resource.record, exact: true }).click();
    await expect(page.getByRole('heading', { name: resource.record, exact: true })).toBeVisible();
    const href = new URL(collection).pathname + new URL(collection).search;
    await expect(
      page
        .getByRole('navigation', { name: 'Main navigation' })
        .getByRole('link', { name: resource.nav, exact: true }),
    ).toHaveAttribute('href', href);
    const back = page.getByRole('link', { name: `Back to ${resource.nav}`, exact: true });
    await expect(back).toHaveAttribute('href', href);
    await back.click();
    await expect(page).toHaveURL(collection);
    await expect(page.getByLabel(resource.search, { exact: true })).toHaveValue(resource.value);
  });

for (const control of ['Back', 'sidebar'])
  test(`Run ${control} restores phase and date filters`, async ({ page }) => {
    await page.clock.setFixedTime(new Date('2026-10-13T06:20:00Z'));
    const query = new URLSearchParams({
      phase: 'Failed',
      created_from: '2026-10-01T00:00:00Z',
      created_before: '2026-11-01T00:00:00Z',
    });
    await demo(page, `/console/runs?${query}`);
    await expect(page).toHaveURL(`/console/runs?${query}`);
    const collection = page.url();
    await page.getByRole('link', { name: 'View run 1040', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Run #1040', exact: true })).toBeVisible();
    const back =
      control === 'Back'
        ? page.getByRole('link', { name: 'Back to Runs', exact: true })
        : page
            .getByRole('navigation', { name: 'Main navigation' })
            .getByRole('link', { name: 'Runs', exact: true });
    await expect(back).toHaveAttribute('href', `/console/runs?${query}`);
    await back.click();
    await expect(page).toHaveURL(collection);
    await expect(page.getByRole('combobox', { name: 'Run phase' })).toHaveText('Failed');
    await expect(page.getByLabel('Created time from', { exact: true })).toHaveValue(
      '2026-10-01T00:00:00Z',
    );
  });

test('a Job run returns to the originating filtered Job history', async ({ page }) => {
  await demo(page);
  await page.getByLabel('Search jobs', { exact: true }).fill('billing');
  await page.getByRole('link', { name: 'billing-reconciliation', exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  const history = page.url();
  await page.getByRole('link', { name: 'View run 1042', exact: true }).click();
  const back = page.getByRole('link', { name: 'Back to Job', exact: true });
  await expect(back).toHaveAttribute('href', new URL(history).pathname + new URL(history).search);
  await back.click();
  await expect(page).toHaveURL(history);
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await expect(page.getByLabel('Search jobs', { exact: true })).toHaveValue('billing');
});

test('Job definition Back link returns to the original filtered collection', async ({ page }) => {
  await demo(page);
  await page.getByLabel('Search jobs', { exact: true }).fill('billing');
  const collection = page.url();
  await page.getByRole('link', { name: 'billing-reconciliation', exact: true }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Job definition', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await expect(page).toHaveURL(collection);
});

test('Function edit Back link and sidebar keep the parent tab and filtered collection', async ({
  page,
}) => {
  await demo(page);
  await navigate(page, 'Functions');
  await page.getByLabel('Search functions', { exact: true }).fill('rebuild');
  const collection = page.url();
  await page.getByRole('link', { name: 'rebuild-search-index', exact: true }).click();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  const parent = page.url();
  await page.getByRole('link', { name: 'Edit function', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Edit rebuild-search-index', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Back to Function', exact: true })).toHaveAttribute(
    'href',
    new URL(parent).pathname + new URL(parent).search,
  );
  await navigate(page, 'Functions');
  await expect(page).toHaveURL(collection);
});

for (const detail of [
  {
    path: '/console/workflows/412/runs/94',
    parent: '/console/workflows/412?tab=runs',
    back: 'Back to Workflow runs',
    heading: /^Workflow run #94$/,
  },
  {
    path: '/console/functions/27/runs/019a0371-d232-7000-8000-000000000001',
    parent: '/console/functions/27',
    back: 'Back to Invocations',
    heading: /^Invocation /,
  },
])
  test(`${detail.path} has a link to its real parent`, async ({ page }) => {
    await demo(page, detail.path);
    await expect(page.getByRole('heading', { level: 1, name: detail.heading })).toBeVisible();
    const back = page.getByRole('link', { name: detail.back, exact: true });
    const parentURL = new URL(detail.parent, page.url()).href;
    await expect(back).toHaveAttribute('href', detail.parent);
    await back.click();
    await expect(page).toHaveURL(parentURL);
  });

test('Alert Back link returns to Alerts', async ({ page }) => {
  await demo(page, '/console/slo-alerts/9');
  const back = page.getByRole('link', { name: 'Back to Alerts', exact: true });
  await expect(back).toHaveAttribute('href', '/console/slo-alerts');
  await back.click();
  await expect(page).toHaveURL(/\/console\/slo-alerts$/);
});

for (const width of [320, 390])
  test(`content navigation exposes the active item at ${width}px and SLIs keep SLO context`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await demo(page, '/console/overview');
    await page.getByRole('link', { name: 'Manage functions', exact: false }).click();
    await expect(page.getByRole('heading', { name: 'Functions', exact: true })).toBeVisible();
    const nav = page.getByRole('navigation', { name: 'Main navigation' });
    const current = nav.locator('[aria-current="page"]');
    await expect(current).toHaveText('Functions');
    await expect
      .poll(async () => {
        const item = await current.boundingBox();
        const viewport = await nav.boundingBox();
        return Boolean(
          item &&
          viewport &&
          item.x >= viewport.x - 1 &&
          item.x + item.width <= viewport.x + viewport.width + 1,
        );
      })
      .toBe(true);
    await navigate(page, 'SLOs');
    await page.getByRole('link', { name: 'SLIs', exact: true }).click();
    await expect(nav.getByRole('link', { name: 'SLOs', exact: true })).toHaveAttribute(
      'aria-current',
      'page',
    );
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });
