import { readFile } from 'node:fs/promises';
import { expect, test } from '@playwright/test';

const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
const path = (route = '') => `${base}/zh-cn/${route}`;
async function triggerTitle() {
  const catalog = JSON.parse(await readFile(new URL('../src/generated/catalog.zh-cn.json', import.meta.url), 'utf8'));
  return catalog.operations.find((operation: { id: string }) => operation.id === 'triggerJob').summary as string;
}

test('Chinese entry, installation, benchmarks and language switching retain their local routes', async ({ page }, testInfo) => {
  await page.goto(path());
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
  await expect(page.getByRole('heading', { level: 1, name: 'OrbitJob 文档', exact: true })).toBeVisible();
  await expect(page.locator('a[href*="/demo/"]')).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath('documentation-zh-desktop.png') });
  await page.getByRole('link', { name: '使用 Kind 和 Helm 部署', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/docs/getting-started/`));
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(/\p{Script=Han}/u);
  await page.goto(path('benchmarks/'));
  await expect(page.getByRole('heading', { level: 1, name: '基准测试', exact: true })).toBeVisible();
  expect(await page.locator(`main a[href^="${base}/zh-cn/docs/load-testing/"]`).count()).toBeGreaterThan(0);
  await page.getByRole('combobox', { name: '选择语言', exact: true }).selectOption({ label: 'English' });
  await expect(page).toHaveURL(new RegExp(`${base}/benchmarks/$`));
  await expect(page.getByRole('heading', { level: 1, name: 'Benchmark process', exact: true })).toBeVisible();
  await page.getByRole('combobox', { name: 'Select language', exact: true }).selectOption({ label: '简体中文' });
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/benchmarks/$`));
});

test('Chinese operation and schema keep exact permissions, IDs and copyable request syntax', async ({ page, context }, testInfo) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await page.goto(path('docs/api/'));
  await expect(page.getByRole('heading', { level: 1, name: 'API 参考', exact: true })).toBeVisible();
  await page.locator('main').getByRole('link', { name: 'POST /api/v1/jobs/{id}/trigger', exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: await triggerTitle(), exact: true })).toBeVisible();
  const content = page.locator('.sl-markdown-content');
  await expect(content.getByRole('heading', { name: '访问权限', exact: true })).toBeVisible();
  await expect(content.getByRole('heading', { name: '请求', exact: true })).toBeVisible();
  await expect(content.getByRole('heading', { name: '响应', exact: true })).toBeVisible();
  await expect(content).toContainText('job:Trigger');
  await expect(content).toContainText('X-OrbitJob-Idempotency-Key');
  await page.screenshot({ path: testInfo.outputPath('api-operation-zh-desktop.png') });
  const command = content.locator('.expressive-code').filter({ hasText: 'curl --request POST' });
  await command.getByRole('button', { name: '复制到剪贴板', exact: true }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('$ORBITJOB_API/api/v1/jobs/$ID/trigger');
  await content.getByRole('link', { name: 'TriggerResult', exact: true }).first().click();
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/docs/api/schemas/triggerresult/`));
  await expect(page.getByRole('heading', { name: '字段', exact: true })).toBeVisible();
  await expect(page.getByRole('cell', { name: 'occurrence_key', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: '完整 Schema', exact: true })).toBeVisible();
});

test('Chinese site search retains full-text results and opens an exact translated API title by keyboard', async ({ page }, testInfo) => {
  const title = await triggerTitle();
  await page.goto(path());
  await expect(page.getByRole('button', { name: '搜索', exact: true })).toBeEnabled();
  await page.keyboard.press('ControlOrMeta+k');
  const dialog = page.getByRole('dialog', { name: '搜索', exact: true });
  const input = dialog.getByRole('textbox', { name: '搜索', exact: true });
  await expect(input).toBeFocused();
  await input.fill(`  ${title}  `);
  const region = dialog.getByRole('region', { name: '标题匹配', exact: true });
  const result = region.getByRole('link', { name: title, exact: true });
  await expect(result).toBeVisible();
  await expect(result).toHaveAttribute('href', path('docs/api/operations/triggerjob/'));
  await expect(dialog.locator('.pagefind-ui__result').first()).toBeVisible();
  await expect(dialog.locator('.pagefind-ui__message')).toHaveText(/^找到 \d+ 个 .+ 的相关结果$/);
  await page.screenshot({ path: testInfo.outputPath('api-search-zh-desktop.png') });
  await page.keyboard.press('Tab');
  await expect(dialog.getByRole('button', { name: '清除', exact: true })).toBeFocused();
  const focusStops = await dialog.locator('a[href], button, input, select, textarea, [tabindex]').count();
  for (let stop = 0; stop < focusStops; stop++) {
    await page.keyboard.press('Tab');
    if (await result.evaluate((link) => link === document.activeElement)) break;
  }
  await expect(result).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/docs/api/operations/triggerjob/$`));
  await expect(page.getByRole('heading', { level: 1, name: title, exact: true })).toBeVisible();
});

test('Chinese rich reference uses localized labels and display spec while keeping stable links and raw download', async ({ page }, testInfo) => {
  const requests: string[] = [];
  const external: string[] = [];
  page.on('request', (request) => requests.push(request.url()));
  await page.route('**/*', async (route) => {
    if (new URL(route.request().url()).origin !== 'http://127.0.0.1:18433') {
      external.push(route.request().url());
      await route.abort();
    } else await route.continue();
  });
  await page.goto(path('api/'));
  await expect(page.getByRole('heading', { name: 'HTTP API 参考', exact: true })).toBeVisible();
  await expect(page.locator('#api-load-status')).toBeHidden();
  await expect(page.locator('#api-reference').getByRole('button', { name: /^打开搜索/ })).toBeVisible();
  await expect(page.getByRole('link', { name: '下载原始 OpenAPI JSON', exact: true })).toHaveAttribute('href', `${base}/openapi.json`);
  await page.locator('.operation-index > summary').click();
  await page.getByRole('searchbox', { name: '查找接口', exact: true }).fill('job:Trigger');
  await page.getByRole('combobox', { name: 'HTTP 方法', exact: true }).selectOption('post');
  const operation = page.locator('[data-operation]:visible');
  await expect(operation).toHaveCount(1);
  await expect(operation).toContainText('所需权限：job:Trigger');
  await operation.locator('a[href="#tag/jobs/triggerJob"]').click();
  await expect(page.locator('[id$="/tag/jobs/triggerJob"]').first()).toBeInViewport();
  await page.screenshot({ path: testInfo.outputPath('rich-reference-zh-desktop.png') });
  await expect(page.getByRole('button', { name: /^(?:测试请求|发送请求|授权|Ask AI)(?:\s|$)/ })).toHaveCount(0);
  await expect(page.locator('input[type="password"]')).toHaveCount(0);
  expect(requests).toContain(`http://127.0.0.1:18433${base}/zh-cn/openapi.json`);
  expect(external).toEqual([]);
  await page.getByRole('link', { name: 'English', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/api/#tag/jobs/triggerJob$`));
  await expect(page.getByRole('heading', { name: 'HTTP API reference', exact: true })).toBeVisible();
  await page.getByRole('link', { name: '简体中文', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/api/#tag/jobs/triggerJob$`));
});

test('Chinese schema failures explain recovery and retain a working localized API index', async ({ page }) => {
  await page.route('**/zh-cn/openapi.json', (route) => route.fulfill({ status: 503, body: 'Temporarily unavailable' }));
  await page.goto(path('api/'));
  await expect(page.getByRole('alert')).toContainText('API 浏览器加载失败。');
  await page.getByRole('link', { name: '可搜索的 API 文档', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/docs/api/$`));
  await expect(page.getByRole('heading', { level: 1, name: 'API 参考', exact: true })).toBeVisible();
});

test('Chinese mobile navigation and theme fit 320px and reach the full API index', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.goto(path());
  await expect(page.getByRole('heading', { level: 1, name: 'OrbitJob 文档', exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('documentation-zh-mobile320.png') });
  await page.getByRole('button', { name: '菜单', exact: true }).click();
  const sidebar = page.locator('#starlight__sidebar');
  await sidebar.getByRole('combobox', { name: '选择主题', exact: true }).selectOption('dark');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.screenshot({ path: testInfo.outputPath('navigation-zh-dark-mobile320.png') });
  await sidebar.getByRole('combobox', { name: '选择主题', exact: true }).selectOption('light');
  await sidebar.getByRole('link', { name: '完整 API 参考', exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${base}/zh-cn/docs/api/$`));
  await expect(page.getByRole('heading', { level: 1, name: 'API 参考', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
});
