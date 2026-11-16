import { readFile } from 'node:fs/promises';
import { expect, test } from '@playwright/test';

const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
async function triggerTitle() {
  const catalog = JSON.parse(await readFile(new URL('../src/generated/catalog.zh-cn.json', import.meta.url), 'utf8'));
  return catalog.operations.find((operation: { id: string }) => operation.id === 'triggerJob').summary as string;
}

test('Chinese title supplement discards an outdated query and clears on close', async ({ page }) => {
  const title = await triggerTitle();
  let release!: () => void;
  const pending = new Promise<void>((resolve) => { release = resolve; });
  let calls = 0;
  await page.route('**/zh-cn/search-titles.json', async (route) => {
    if (++calls === 1) await pending;
    await route.continue().catch(() => {});
  });
  await page.goto(`${base}/zh-cn/`);
  const opener = page.getByRole('button', { name: '搜索', exact: true });
  await opener.click();
  const dialog = page.getByRole('dialog', { name: '搜索', exact: true });
  const input = dialog.getByRole('textbox', { name: '搜索', exact: true });
  await input.fill(title);
  await expect.poll(() => calls).toBe(1);
  await input.fill('这段文本不应匹配任何文档xyz');
  await page.keyboard.press('Escape');
  await expect(opener).toBeFocused();
  release();
  await opener.click();
  await expect(page.getByRole('region', { name: '标题匹配', exact: true })).not.toBeVisible();
  await expect(dialog.locator('a[href*="/docs/reference/http/operations/triggerjob/"]')).toHaveCount(0);
  await input.fill(title);
  const region = page.getByRole('region', { name: '标题匹配', exact: true });
  await expect(region.getByRole('link', { name: title, exact: true })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(region).not.toBeVisible();
  await expect(opener).toBeFocused();
});

test('a Chinese title-only match hides the no-match message and restores it for the next unmatched query', async ({ page }) => {
  const title = '这段文本不应匹配任何文档xyz';
  const operationPath = `${base}/zh-cn/docs/reference/http/operations/triggerjob/`;
  await page.route('**/zh-cn/search-titles.json', (route) => route.fulfill({
    json: [{ title, route: operationPath }],
  }));
  await page.goto(`${base}/zh-cn/`);
  await page.getByRole('button', { name: '搜索', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '搜索', exact: true });
  const input = dialog.getByRole('textbox', { name: '搜索', exact: true });
  const region = dialog.getByRole('region', { name: '标题匹配', exact: true });
  const message = dialog.locator('.pagefind-ui__message');
  await input.fill(`  ${title}  `);
  const result = region.getByRole('link', { name: title, exact: true });
  await expect(result).toBeVisible();
  await expect(result).toHaveAttribute('href', operationPath);
  await expect(dialog.locator('.pagefind-ui__result')).toHaveCount(0);
  await expect(message).toBeHidden();
  const unmatched = `${title}也没有目录条目`;
  await input.fill(unmatched);
  await expect(message).toBeVisible();
  await expect(message).toHaveText(`未找到 ${unmatched} 的相关结果`);
  await expect(region).toBeHidden();
  await expect(region.getByRole('link')).toHaveCount(0);
});

test('a title catalog failure preserves the real no-match message and subsequent full-text results', async ({ page }) => {
  let catalogRequests = 0;
  await page.route('**/zh-cn/search-titles.json', (route) => {
    catalogRequests++;
    return route.fulfill({ status: 503, body: 'Unavailable' });
  });
  await page.goto(`${base}/zh-cn/`);
  await page.getByRole('button', { name: '搜索', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '搜索', exact: true });
  const input = dialog.getByRole('textbox', { name: '搜索', exact: true });
  await input.fill('这段文本不应匹配任何文档xyz');
  await expect.poll(() => catalogRequests).toBeGreaterThan(0);
  await expect(dialog.locator('.pagefind-ui__message')).toBeVisible();
  await expect(dialog.locator('.pagefind-ui__message')).toContainText('未找到');
  await expect(page.getByRole('region', { name: '标题匹配', exact: true })).not.toBeVisible();
  await input.fill('触发');
  await expect(dialog.locator('.pagefind-ui__result').first()).toBeVisible();
  await expect(dialog.locator('a[href*="/zh-cn/docs/reference/http/operations/triggerjob/"]').first()).toBeVisible();
  await expect(page.getByRole('region', { name: '标题匹配', exact: true })).not.toBeVisible();
});
