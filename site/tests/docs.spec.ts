import { expect, test } from '@playwright/test';

const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
const path = (route = '') => `${base}/${route}`;

test('documentation entry leads to installation and architecture without a demo route', async ({ page }, testInfo) => {
  await page.goto(path());
  await expect(page.getByRole('heading', { name: 'OrbitJob documentation', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeEnabled();
  await expect(page.locator('a[href*="/demo/"]')).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath('documentation-desktop.png') });
  await page.getByRole('link', { name: 'Install OrbitJob', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/docs/setup/prerequisites/`));
  await expect(page.getByRole('heading', { level: 1, name: 'Installation prerequisites' })).toBeVisible();
  await page.goto(path('docs/concepts/architecture/'));
  await expect(page.getByRole('heading', { level: 1, name: 'Architecture' })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('architecture-desktop.png') });
});

test('complete API index leads through request, responses and a linked schema', async ({ page }, testInfo) => {
  await page.goto(path('docs/reference/http/'));
  await expect(page.getByRole('heading', { level: 1, name: 'API reference', exact: true })).toBeVisible();
  await page.locator('main').getByRole('link', { name: 'POST /api/v1/jobs/{id}/trigger', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Trigger a manual run', exact: true })).toBeVisible();
  const content = page.locator('.sl-markdown-content');
  await expect(content).toContainText('job:Trigger');
  await expect(content).toContainText('X-OrbitJob-Idempotency-Key');
  await expect(content.getByRole('heading', { name: 'Request', exact: true })).toBeVisible();
  await expect(content.getByRole('heading', { name: 'Responses', exact: true })).toBeVisible();
  await expect(content.getByRole('heading', { name: /401.*Unauthorized/ })).toHaveCount(1);
  await page.screenshot({ path: testInfo.outputPath('api-operation-desktop.png') });
  await content.getByRole('link', { name: 'TriggerResult', exact: true }).first().click();
  await expect(page).toHaveURL(new RegExp(`${base}/docs/reference/http/schemas/triggerresult/`));
  await expect(page.getByRole('heading', { level: 1, name: 'TriggerResult', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'occurrence_key', exact: true })).toBeVisible();
  await expect(content.getByRole('heading', { name: 'Complete schema', exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('api-schema-desktop.png') });
  await content.getByRole('link', { name: 'POST /api/v1/jobs/{id}/trigger', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Trigger a manual run', exact: true })).toBeVisible();
});

test('request template copies an installation-local command with credential placeholders', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto(path('docs/reference/http/operations/triggerjob/'));
  const example = page.locator('.expressive-code').filter({ hasText: 'curl --request POST' });
  await example.getByRole('button', { name: /copy/i }).click();
  const clipboard = await page.evaluate(() => navigator.clipboard.readText());
  expect(clipboard).toContain('curl --request POST');
  expect(clipboard).toContain('$ORBITJOB_API/api/v1/jobs/$ID/trigger');
  expect(clipboard).toContain('Authorization: Bearer $ORBITJOB_API_KEY');
});

test('keyboard search finds a complete API operation and opens its documentation', async ({ page }, testInfo) => {
  await page.goto(path());
  await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeEnabled();
  await page.keyboard.press('ControlOrMeta+k');
  const dialog = page.getByRole('dialog', { name: 'Search', exact: true });
  await expect(dialog).toBeVisible();
  const input = dialog.getByRole('textbox', { name: 'Search', exact: true });
  await expect(input).toBeFocused();
  await input.fill('Trigger a manual run');
  const result = dialog.locator(`a[href*="/docs/reference/http/operations/triggerjob/"]`).first();
  await expect(result).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('api-search-desktop.png') });
  await result.click();
  await expect(page).toHaveURL(new RegExp(`${base}/docs/reference/http/operations/triggerjob/`));
  await expect(page.getByRole('heading', { level: 1, name: 'Trigger a manual run', exact: true })).toBeVisible();
});

test('320px navigation and theme remain usable with keyboard focus', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto(path());
  await expect(page.getByRole('heading', { name: 'OrbitJob documentation', exact: true })).toBeVisible();
  await page.keyboard.press('Tab');
  const skip = page.getByRole('link', { name: 'Skip to content', exact: true });
  await expect(skip).toBeFocused();
  await expect(skip).toBeInViewport();
  await page.keyboard.press('Enter');
  const menu = page.getByRole('button', { name: /Menu/ });
  await menu.click();
  const sidebar = page.locator('#starlight__sidebar');
  await expect(sidebar).toBeVisible();
  const theme = sidebar.getByRole('combobox', { name: /theme/i });
  await theme.selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.screenshot({ path: testInfo.outputPath('mobile-navigation-dark.png') });
  await theme.selectOption('light');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await sidebar.locator('summary').filter({ hasText: /^Reference$/ }).click();
  await sidebar.getByRole('link', { name: 'HTTP API', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'API reference', exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('api-index-mobile320.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});

test('static API documentation and spec remain available without JavaScript', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false });
  const page = await context.newPage();
  try {
    await page.goto(`http://127.0.0.1:18433${path('docs/reference/http/operations/triggerjob/')}`);
    await expect(page.getByRole('heading', { level: 1, name: 'Trigger a manual run', exact: true })).toBeVisible();
    await expect(page.locator('.sl-markdown-content')).toContainText('Authorization: Bearer $ORBITJOB_API_KEY');
    await page.goto(`http://127.0.0.1:18433${path('api/')}`);
    await page.getByRole('link', { name: 'Searchable API documentation', exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: 'API reference', exact: true })).toBeVisible();
    const spec = await context.request.get(`http://127.0.0.1:18433${path('openapi.json')}`);
    expect(spec.ok()).toBe(true);
    expect((await spec.json()).paths['/api/v1/jobs/{id}/trigger'].post.operationId).toBe('triggerJob');
  } finally {
    await context.close();
  }
});
