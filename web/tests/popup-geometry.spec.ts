import { expect, fixtureIdentity, signIn, test } from './support';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

for (const viewport of [
  { width: 1440, height: 1000, colorScheme: 'light' as const },
  { width: 320, height: 844, colorScheme: 'dark' as const },
]) {
  test(`small portaled menus fit their options at ${viewport.width}px`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.emulateMedia({ colorScheme: viewport.colorScheme });
    await page.route('**/api/v1/identity*', (route) =>
      route.fulfill({ json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] } }),
    );
    await page.goto('/console/manifests/job');
    await signIn(page);
    const trigger = page.getByRole('combobox', { name: 'Trigger mode', exact: true });
    await trigger.click();
    const popup = page.getByRole('listbox');
    await expect(popup.getByRole('option')).toHaveCount(2);
    await expect
      .poll(() => popup.evaluate((node) => Math.round(node.getBoundingClientRect().height)))
      .toBeLessThan(140);
    const bounds = await popup.boundingBox();
    expect(bounds!.width).toBeLessThanOrEqual(viewport.width);
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(viewport.width + 1);
    await page.keyboard.press('Escape');
    await expect(popup).toBeHidden();
    await expect(trigger).toBeFocused();
    await expect(trigger).toHaveText('Manual only');
    await trigger.click();
    await popup.getByRole('option', { name: 'Scheduled and manual', exact: true }).click();
    await expect(trigger).toHaveText('Scheduled and manual');
    await expect(
      page.getByRole('textbox', { name: 'Configured schedule', exact: true }),
    ).toBeVisible();
    await page.getByText('Run policies', { exact: true }).click();
    await page.getByRole('combobox', { name: 'Concurrency policy', exact: true }).click();
    await expect(popup.getByRole('option')).toHaveCount(2);
    await expect
      .poll(() => popup.evaluate((node) => Math.round(node.getBoundingClientRect().height)))
      .toBeLessThan(140);
    await page.keyboard.press('Escape');
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });
}
