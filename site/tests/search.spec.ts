import { expect, test, type Page } from '@playwright/test';

const base = (process.env.SITE_BASE || '/orbitjob').replace(/\/$/, '');

async function deferSearchUI(page: Page) {
  let release!: () => void;
  const ready = new Promise<void>((resolve) => { release = resolve; });
  await page.route('**/_astro/ui-core.*.js', async (route) => {
    await ready;
    await route.continue();
  });
  await page.goto(`${base}/`, { waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('button', { name: 'Search', exact: true })).toBeEnabled();
  return release;
}

test('cold keyboard search focuses its delayed input once it becomes available', async ({ page }) => {
  const release = await deferSearchUI(page);
  await page.keyboard.press('ControlOrMeta+k');
  const dialog = page.getByRole('dialog', { name: 'Search', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('textbox', { name: 'Search', exact: true })).toHaveCount(0);
  release();
  const input = dialog.getByRole('textbox', { name: 'Search', exact: true });
  await expect(input).toBeFocused();
  await page.keyboard.type('Trigger a manual run');
  await expect(dialog.locator('a[href*="/docs/reference/http/operations/triggerjob/"]').first()).toBeVisible();
});

test('closing search before its input loads preserves focus and supports reopening', async ({ page }) => {
  const release = await deferSearchUI(page);
  const opener = page.getByRole('button', { name: 'Search', exact: true });
  await opener.click();
  const dialog = page.getByRole('dialog', { name: 'Search', exact: true });
  await expect(dialog).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).not.toBeVisible();
  await expect(opener).toBeFocused();
  release();
  await expect(page.locator('site-search input')).toHaveCount(1);
  await expect(opener).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(dialog.getByRole('textbox', { name: 'Search', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(opener).toBeFocused();
});

for (const [query, route] of [
  ['Cancel a run', 'tasks/cancel-a-run'],
  ['Installation prerequisites', 'setup/prerequisites'],
  ['Read execution logs', 'tasks/read-logs'],
]) {
  test(`task search finds ${query} among the first results`, async ({ page }) => {
    await page.goto(`${base}/`);
    await page.getByRole('button', { name: 'Search', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Search', exact: true });
    await dialog.getByRole('textbox', { name: 'Search', exact: true }).fill(query);
    const result = dialog.locator(`a[href*="/docs/${route}/"]`).first();
    await expect(result).toBeVisible();
    expect(await dialog.locator('.pagefind-ui__result-link').evaluateAll(links => links.every(link => !(link as HTMLAnchorElement).pathname.includes('/zh-cn/')))).toBe(true);
    await expect.poll(async () => dialog.locator('.pagefind-ui__result-link').evaluateAll((links, route) => links.slice(0, 5).some(link => link.getAttribute('href')?.includes(`/docs/${route}/`)), route)).toBe(true);
    await result.click();
    await expect(page).toHaveURL(new RegExp(`/docs/${route}/`));
  });
}
