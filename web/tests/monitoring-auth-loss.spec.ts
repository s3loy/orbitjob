import { signIn } from './support';
import { expect, test } from './support';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

for (const status of [401, 403])
  test(`a ${status} monitoring list response ${status === 401 ? 'clears local drafts and requires sign in' : 'preserves Create-only access'}`, async ({
    page,
  }) => {
    let release!: () => void;
    let entered!: () => void;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    const requested = new Promise<void>((resolve) => {
      entered = resolve;
    });
    let writes = 0;
    await page.route('**/api/v1/**', async (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fallback();
      if (path === '/api/v1/checks' && route.request().method() === 'POST') {
        writes++;
        return route.fulfill({ json: { id: 222 } });
      }
      if (path === '/api/v1/checks') {
        entered();
        await held;
        return route.fulfill({
          status,
          json: {
            error: {
              code: status === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN',
              message: 'List access rejected',
            },
          },
        });
      }
      if (path === '/api/v1/checks/222')
        return route.fulfill({
          status: 403,
          json: { error: { code: 'FORBIDDEN', message: 'Detail read denied' } },
        });
      return route.fulfill({ json: { items: [] } });
    });
    await page.goto('/console/checks');
    await signIn(page, 'monitoring_auth_fixture');
    await requested;
    await page.getByRole('button', { name: 'New check', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Name', { exact: true }).fill('retained-check-draft');
    await dialog.getByLabel('URL', { exact: true }).fill('https://service.example/health');
    release();
    if (status === 401) {
      await expect(
        page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
      ).toBeVisible();
      await expect(dialog).toHaveCount(0);
      await expect(page.locator('.sidebar')).toHaveCount(0);
      expect(writes).toBe(0);
    } else {
      await expect(page.locator('[role="alert"]')).toContainText('Permission required');
      await expect(dialog.getByRole('button', { name: 'Create check', exact: true })).toBeEnabled();
      await dialog.getByRole('button', { name: 'Create check', exact: true }).click();
      const receipt = page.locator('main > section[aria-label="Monitoring creation receipts"]');
      await expect(receipt).toContainText('Created retained-check-draft');
      await expect(receipt).toContainText('Check #222');
      expect(writes).toBe(1);
    }
  });
