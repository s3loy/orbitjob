import { expect, test } from '@playwright/test';

const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
const path = (route = '') => `${base}/${route}`;

test('rich operation directory filters by permission and method and recovers from no results', async ({ page }, testInfo) => {
  await page.goto(path('api/'));
  await page.locator('.operation-index > summary').click();
  const search = page.getByRole('searchbox', { name: 'Find an operation' });
  await search.fill('job:Trigger');
  await page.getByRole('combobox', { name: 'HTTP method', exact: true }).selectOption('post');
  const visible = page.locator('[data-operation]:visible');
  await expect(visible).toHaveCount(1);
  await expect(visible).toContainText('/api/v1/jobs/{id}/trigger');
  await expect(visible).toContainText('Permission: job:Trigger');
  await page.screenshot({ path: testInfo.outputPath('rich-filter-desktop.png') });
  await search.fill('no-such-operation');
  await expect(page.getByRole('status').filter({ hasText: 'No matching operations.' })).toBeVisible();
  await expect(visible).toHaveCount(0);
  await search.fill('');
  await page.getByRole('combobox', { name: 'HTTP method', exact: true }).selectOption('');
  expect(await visible.count()).toBeGreaterThan(60);
});

test('exact operation deep link opens local schemas without credentials, execution controls or external traffic', async ({ page }) => {
  const external: string[] = [];
  const requests: { url: string; method: string }[] = [];
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  page.on('request', (request) => requests.push({ url: request.url(), method: request.method() }));
  await page.route('**/*', async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== 'http://127.0.0.1:18433') {
      external.push(url.href);
      await route.abort();
    } else await route.continue();
  });
  await page.goto(`${path('api/')}#tag/jobs/triggerJob`);
  await expect(page.locator('#api-load-status')).toBeHidden();
  const operation = page.locator('[id$="/tag/jobs/triggerJob"]').first();
  await expect(operation).toBeVisible();
  await expect(operation).toBeInViewport();
  await expect(page.locator('#api-reference')).toContainText('triggerJob');
  await expect(page.locator('#api-reference')).toContainText('job:Trigger');
  await expect(page.getByRole('button', { name: /^(?:Test Request|Send Request|Authorize|Open API Client|Ask AI)(?:\s|$)/i })).toHaveCount(0);
  await expect(page.locator('input[type="password"], input[placeholder*="Bearer"], input[placeholder*="token" i]')).toHaveCount(0);
  expect(external).toEqual([]);
  expect(requests.filter((request) => request.method !== 'GET')).toEqual([]);
  expect(requests.filter((request) => new URL(request.url).pathname.startsWith('/api/v1/'))).toEqual([]);
  expect(errors).toEqual([]);
});

for (const sourceLocale of ['', '/zh-cn']) {
  test(`rich reference language switching retains initial, sidebar, directory and search selections ${sourceLocale || 'English'}`, async ({ page }) => {
    const targetLocale = sourceLocale ? '' : '/zh-cn';
    const source = `${base}${sourceLocale}/api/?source=language-regression`;
    const target = `${base}${targetLocale}/api/?source=language-regression`;
    const targetLanguage = sourceLocale ? 'English' : '简体中文';
    const sourceLanguage = sourceLocale ? '简体中文' : 'English';
    let releaseSchema!: () => void;
    const schemaGate = new Promise<void>((resolve) => { releaseSchema = resolve; });
    const schemaRequested = page.waitForRequest((request) => new URL(request.url()).pathname === `${base}${sourceLocale}/openapi.json`);
    await page.route('**/openapi.json', async (route) => {
      await schemaGate;
      await route.continue();
    }, { times: 1 });
    try {
      await page.goto(`${source}#tag/jobs/triggerJob`, { waitUntil: 'domcontentloaded' });
      await schemaRequested;
      await expect(page.locator('#api-load-status')).toBeVisible();
      await expect(page.getByRole('link', { name: targetLanguage, exact: true })).toHaveAttribute('href', new URL(`${target}#tag/jobs/triggerJob`, page.url()).href);
    } finally {
      releaseSchema();
    }
    await expect(page.locator('[id$="/tag/jobs/triggerJob"]').first()).toBeInViewport();

    const languageLink = page.getByRole('link', { name: targetLanguage, exact: true });
    await languageLink.scrollIntoViewIfNeeded();
    // Model the viewer replacing the fragment while the reader returns to the header.
    await page.evaluate(() => { history.replaceState(history.state, '', '#description/introduction'); });
    await expect(page).toHaveURL(`${source}#description/introduction`);
    await languageLink.click();
    await expect(page).toHaveURL(`${target}#tag/jobs/triggerJob`);
    await expect(page.locator('[id$="/tag/jobs/triggerJob"]').first()).toBeInViewport();

    await page.locator('#api-reference aside a[href="#tag/jobs/listJobs"]').click();
    await expect(page.locator('[id$="/tag/jobs/listJobs"]').first()).toBeInViewport();
    await expect(page.getByRole('link', { name: sourceLanguage, exact: true })).toHaveAttribute('href', new URL(`${source}#tag/jobs/listJobs`, page.url()).href);
    await page.locator('.operation-index > summary').click();
    await page.locator('.operation-index a[href="#tag/jobs/getJob"]').click();
    const selectedOperation = page.locator('[id$="/tag/jobs/getJob"]').first();
    await expect(selectedOperation).toBeInViewport();
    await page.locator('.operation-index > summary').click();
    await selectedOperation.scrollIntoViewIfNeeded();
    await expect(selectedOperation).toBeInViewport();
    const returnLink = page.getByRole('link', { name: sourceLanguage, exact: true });
    await returnLink.scrollIntoViewIfNeeded();
    await page.evaluate(() => { history.replaceState(history.state, '', '#description/introduction'); });
    await expect(page).toHaveURL(`${target}#description/introduction`);
    await returnLink.click();
    await expect(page).toHaveURL(`${source}#tag/jobs/getJob`);
    await expect(page.locator('[id$="/tag/jobs/getJob"]').first()).toBeInViewport();

    await page.locator('#api-reference').getByRole('button', { name: sourceLocale ? /^打开搜索/ : /^Open Search/ }).click();
    const dialog = page.getByRole('dialog');
    const query = dialog.getByRole('combobox');
    await query.fill('triggerJob');
    const result = dialog.getByRole('option').filter({ hasText: '/api/v1/jobs/{id}/trigger' });
    await expect(result).toBeVisible();
    if (sourceLocale) {
      await expect(dialog.getByRole('option').first()).toContainText('/api/v1/jobs/{id}/trigger');
      await query.press('ArrowDown');
      await query.press('Enter');
    } else await result.click();
    await expect(dialog).toBeHidden();
    await expect(page.locator('[id$="/tag/jobs/triggerJob"]').first()).toBeInViewport();
    await expect(languageLink).toHaveAttribute('href', new URL(`${target}#tag/jobs/triggerJob`, page.url()).href);
    await languageLink.scrollIntoViewIfNeeded();
    await page.evaluate(() => { history.replaceState(history.state, '', '#description/introduction'); });
    await expect(page).toHaveURL(`${source}#description/introduction`);
    await languageLink.click();
    await expect(page).toHaveURL(`${target}#tag/jobs/triggerJob`);
    await expect(page.locator('[id$="/tag/jobs/triggerJob"]').first()).toBeInViewport();
  });
}

test('rich reference language links retain anchor and history navigation without the Navigation API', async ({ page }) => {
  await page.addInitScript(() => { Object.defineProperty(window, 'navigation', { value: undefined }); });
  const source = path('api/?source=history-regression');
  let releaseSchema!: () => void;
  const schemaGate = new Promise<void>((resolve) => { releaseSchema = resolve; });
  await page.route('**/openapi.json', async (route) => {
    await schemaGate;
    await route.continue();
  });
  try {
    await page.goto(`${source}#tag/jobs/triggerJob`, { waitUntil: 'domcontentloaded' });
    const languageLink = page.getByRole('link', { name: '简体中文', exact: true });
    const target = `${base}/zh-cn/api/?source=history-regression`;
    await expect(languageLink).toHaveAttribute('href', new URL(`${target}#tag/jobs/triggerJob`, page.url()).href);
    await page.goto(`${source}#tag/jobs/getJob`, { waitUntil: 'domcontentloaded' });
    await expect(languageLink).toHaveAttribute('href', new URL(`${target}#tag/jobs/getJob`, page.url()).href);
    await page.goBack();
    await expect(languageLink).toHaveAttribute('href', new URL(`${target}#tag/jobs/triggerJob`, page.url()).href);
    await page.goForward();
    await expect(languageLink).toHaveAttribute('href', new URL(`${target}#tag/jobs/getJob`, page.url()).href);
    await page.locator('.operation-index > summary').click();
    await page.locator('.operation-index a[href="#tag/jobs/listJobs"]').click();
    await expect(languageLink).toHaveAttribute('href', new URL(`${target}#tag/jobs/listJobs`, page.url()).href);
  } finally {
    releaseSchema();
  }
});

test('a local schema load failure offers working static documentation recovery', async ({ page }) => {
  await page.route('**/openapi.json', (route) => route.fulfill({ status: 503, body: 'Temporarily unavailable' }));
  await page.goto(path('api/'));
  await expect(page.getByRole('alert')).toContainText('The schema viewer could not load.');
  await expect(page.getByRole('link', { name: 'Download OpenAPI JSON', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Searchable API documentation', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/docs/reference/http/$`));
  await expect(page.getByRole('heading', { level: 1, name: 'API reference', exact: true })).toBeVisible();
});

test('download gives the complete reusable contract', async ({ page }) => {
  await page.goto(path('api/'));
  const downloadPromise = page.waitForEvent('download');
  await page.getByRole('link', { name: 'Download OpenAPI JSON', exact: true }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe('orbitjob-openapi.json');
  expect(await download.failure()).toBeNull();
  const response = await page.request.get(path('openapi.json'));
  const spec = await response.json();
  expect(spec.paths['/api/v1/jobs/{id}/trigger'].post.operationId).toBe('triggerJob');
  expect(spec.components.schemas.TriggerResult.required).toContain('occurrence_key');
});

test('rich reference remains navigable at 320px with a long operation path', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto(path('api/'));
  await expect(page.getByRole('heading', { name: 'HTTP API reference', exact: true })).toBeVisible();
  await page.locator('.operation-index > summary').click();
  await page.getByRole('searchbox', { name: 'Find an operation' }).fill('/attempts/{attempt}/logs');
  await expect(page.locator('[data-operation]:visible')).toHaveCount(2);
  await page.locator('[data-operation]:visible').first().scrollIntoViewIfNeeded();
  await page.screenshot({ path: testInfo.outputPath('rich-reference-mobile320.png') });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await expect(page.locator('[data-operation]:visible').first().getByRole('link', { name: /^Documentation/ })).toBeVisible();
});

for (const locale of ['', '/zh-cn']) {
  test(`account references describe authenticated writes and actual request constraints ${locale || 'English'}`, async ({ page }, testInfo) => {
    const zh = Boolean(locale);
    await page.goto(`${base}${locale}/api/`);
    await page.locator('.operation-index > summary').click();
    const invitation = page.locator('[data-operation]').filter({ has: page.locator('a[href="#tag/authentication/inviteConsoleMember"]') });
    await expect(invitation).toContainText(zh ? '需要认证' : 'Authentication required');
    await expect(invitation).not.toContainText(zh ? '元数据接口' : 'Authenticated metadata');
    await invitation.scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath('account-access-label.png') });
    await invitation.getByRole('link', { name: zh ? /^接口文档/ : /^Documentation/ }).click();
    const content = page.locator('.sl-markdown-content');
    await expect(content).toContainText(zh ? '24 小时' : '24 hours');
    await expect(content).toContainText(zh ? '15 分钟' : '15 minutes');
    await expect(content).not.toContainText(zh ? '元数据查询遵循' : 'Authenticated metadata access');
    await page.goto(`${base}${locale}/docs/reference/http/schemas/resetpasswordrequest/`);
    for (const field of ['token', 'password']) {
      const row = page.locator('.sl-markdown-content tr').filter({ has: page.getByRole('cell', { name: field, exact: true }) });
      await expect(row.getByRole('cell', { name: zh ? '是' : 'Yes', exact: true })).toHaveCount(1);
    }
    const requiredHeader = page.getByRole('columnheader', { name: zh ? '必填' : 'Required', exact: true });
    expect(await requiredHeader.evaluate((header) => {
      const range = document.createRange();
      range.selectNodeContents(header);
      return range.getClientRects().length;
    })).toBe(1);
    await page.screenshot({ path: testInfo.outputPath('account-request-schema.png') });
    await page.setViewportSize({ width: 320, height: 740 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    const schemaTable = page.locator('.sl-markdown-content table').first();
    const description = schemaTable.locator('tbody tr').first().locator('td').last();
    expect((await description.boundingBox())?.width).toBeGreaterThanOrEqual(240);
    expect(await schemaTable.evaluate((table) => table.scrollWidth > table.clientWidth)).toBe(true);
    await schemaTable.evaluate((table) => { table.scrollLeft = table.scrollWidth; });
    await page.screenshot({ path: testInfo.outputPath('account-request-mobile320.png') });
    const contract = await (await page.request.get(`${base}/openapi.json`)).json();
    const archive = Object.values(contract.paths).flatMap((item: any) => Object.values(item)).find((op: any) => op.operationId === 'readinstanceExecutionArchive') as any;
    expect(archive.parameters.find((parameter: any) => parameter.name === 'limit').schema.default).toBe(500);
    expect(contract.components.schemas.AcceptInvitationRequest.required).toEqual(['token']);
    expect(contract.components.schemas.MemberChangeRequest.properties.status.nullable).toBe(true);
    expect(contract.components.schemas.MemberChangeRequest.properties.status.enum).toBeUndefined();
  });
}
