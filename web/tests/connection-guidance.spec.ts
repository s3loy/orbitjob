import { expect, test, signIn } from './support';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

test('account entry explains invitation and password recovery without accepting API keys', async ({
  page,
}) => {
  await page.goto('/console/connection');
  await expect(page.getByRole('heading', { name: 'Sign in to OrbitJob' })).toBeVisible();
  await expect(
    page.getByText('Need an account or forgot your password?', { exact: false }),
  ).toBeVisible();
  await expect(page.getByLabel('API key', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: /bootstrap|initial key/i })).toHaveCount(0);
});

test('an uninitialized installation needs an installer-issued setup capability', async ({
  page,
}) => {
  await page.route('**/api/v1/auth/session', (route) =>
    route.fulfill({ json: { authenticated: false, initialized: false, workspaces: [] } }),
  );
  await page.goto('/console/connection');
  await expect(page.getByRole('heading', { name: 'Finish installation setup' })).toBeVisible();
  await expect(page.getByLabel('Password', { exact: true })).toHaveCount(0);
  await expect(page.getByText('one-time setup link', { exact: false })).toBeVisible();
});

test('failed password login clears the password and gives generic recovery', async ({ page }) => {
  await page.route('**/api/v1/auth/login', (route) =>
    route.fulfill({
      status: 401,
      json: { error: { code: 'unauthorized', message: 'Invalid credentials' } },
    }),
  );
  await page.goto('/console/connection');
  await signIn(page, 'private-invalid-password');
  await expect(page.getByRole('alert')).toContainText('Sign in failed.');
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    'private-invalid-password',
  );
});

for (const width of [320, 390])
  test(`account guidance stays readable at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 850 });
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto('/console/connection');
    await expect(page.getByRole('button', { name: 'Sign in', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
  });
