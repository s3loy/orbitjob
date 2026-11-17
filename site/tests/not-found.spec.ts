import { readFile } from 'node:fs/promises';
import { expect, test } from '@playwright/test';

const origin = 'http://127.0.0.1:18433';
const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');
const artifact = () => readFile(new URL('../dist/404.html', import.meta.url), 'utf8');

for (const [locale, lang, title] of [['', 'en', 'Page not found'], ['/zh-cn', 'zh-CN', '页面未找到']]) {
  test(`missing ${lang} route serves the root static 404 at the original URL`, async ({ page }) => {
    const pathname = `${base}${locale}/docs/this-page-does-not-exist/`;
    const requested = `${origin}${pathname}?next=//unsafe.invalid/#%3Cscript%3E`;
    const response = await page.goto(requested);
    expect(response?.status()).toBe(404);
    expect(await response?.text()).toBe(await artifact());
    await expect(page).toHaveURL(requested);
    await expect(page.locator('html')).toHaveAttribute('lang', lang);
    await expect(page).toHaveTitle(`${title} | OrbitJob`);
    await expect(page.locator('h1 > span').first()).toHaveText(title);
    await expect(page.locator('[data-recovery-sections] > section').first()).toHaveAttribute('lang', lang);
    await expect(page.locator('link[rel="canonical"], link[rel="alternate"]')).toHaveCount(0);
    const links = await page.locator('main a').evaluateAll(anchors => anchors.map(anchor => anchor.getAttribute('href')));
    expect(links).toEqual(expect.arrayContaining([`${base}/`, `${base}/zh-cn/`]));
    expect(links.every(link => link?.startsWith(`${base}/`) && !/[?#]/.test(link) && !link.startsWith('//'))).toBe(true);
    await page.getByRole('link', { name: lang === 'zh-CN' ? '打开中文文档首页' : 'Open documentation home', exact: true }).click();
    await expect(page).toHaveURL(`${origin}${base}${locale}/`);
    await expect(page.getByRole('heading', { level: 1, name: lang === 'zh-CN' ? 'OrbitJob 文档' : 'OrbitJob documentation', exact: true })).toBeVisible();
  });
}

test('Chinese recovery works without JavaScript and marks both text languages', async ({ browser }, testInfo) => {
  const context = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 320, height: 740 } });
  try {
    const page = await context.newPage();
    const requested = `${origin}${base}/zh-cn/unknown/?language=en#missing`;
    const response = await page.goto(requested);
    expect(response?.status()).toBe(404);
    expect(await response?.text()).toBe(await artifact());
    await expect(page).toHaveURL(requested);
    await expect(page.locator('#recovery-en')).toHaveAttribute('lang', 'en');
    await expect(page.locator('#recovery-zh')).toHaveAttribute('lang', 'zh-CN');
    await expect(page.locator('h1 [lang="zh-CN"]')).toHaveText('页面未找到');
    await expect(page.locator('#recovery-zh')).toContainText('请检查网址');
    await expect(page.getByRole('link', { name: '打开中文文档首页', exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Open documentation home', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    await page.screenshot({ path: testInfo.outputPath('404-bilingual-mobile-no-javascript.png'), fullPage: true });
    await page.getByRole('link', { name: '打开中文文档首页', exact: true }).click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN');
    await expect(page.getByRole('heading', { level: 1, name: 'OrbitJob 文档', exact: true })).toBeVisible();
  } finally {
    await context.close();
  }
});

test('query strings and similar path prefixes cannot select Chinese or change recovery targets', async ({ page }) => {
  await page.goto(`${base}/zh-cn-extra/unknown/?next=//unsafe.invalid/zh-cn/#/zh-cn/`);
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page).toHaveTitle('Page not found | OrbitJob');
  await expect(page.getByRole('link', { name: '打开中文文档首页', exact: true })).toHaveAttribute('href', `${base}/zh-cn/`);
});
