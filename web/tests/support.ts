import { test as base, expect, type Page } from '@playwright/test';
export type { Page, Route } from '@playwright/test';

export const fixtureIdentity = {
  key_id: '',
  subject_type: 'user',
  user_id: '00000000000000000000000002',
  username: 'fixture-user',
  display_name: 'Fixture User',
  membership_role: 'admin',
  installation_admin: true,
  kind: 'tenant',
  tenant_id: '00000000000000000000000001',
  target_tenant_id: '00000000000000000000000001',
  group_id: '',
  boundary_policy_id: '',
  capabilities: [],
};
export const fixtureAccount = {
  authenticated: true,
  initialized: true,
  user: {
    id: '00000000000000000000000002',
    username: 'fixture-user',
    display_name: 'Fixture User',
    installation_admin: true,
  },
  csrf_token: 'fixture-csrf',
  expires_at: '2099-01-01T00:00:00Z',
  workspaces: [
    { id: fixtureIdentity.tenant_id, name: 'Fixture workspace', slug: 'fixture', role: 'admin' },
  ],
};
export const test = base.extend({
  page: async ({ page }, use) => {
    let signedIn = false;
    page.on('response', (response) => {
      if (
        response.status() === 401 &&
        new URL(response.url()).pathname.startsWith('/api/v1/') &&
        !new URL(response.url()).pathname.endsWith('/auth/login')
      )
        signedIn = false;
    });
    await page.route('**/api/v1/auth/**', (route) => {
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/login')) {
        signedIn = true;
        return route.fulfill({ json: fixtureAccount });
      }
      if (path.endsWith('/logout')) {
        signedIn = false;
        return route.fulfill({ status: 204 });
      }
      if (path.endsWith('/session'))
        return route.fulfill({
          json: signedIn
            ? fixtureAccount
            : { authenticated: false, initialized: true, workspaces: [] },
        });
      return route.fulfill({ json: { items: [] } });
    });
    await page.route('**/api/v1/identity*', (route) => route.fulfill({ json: fixtureIdentity }));
    await use(page);
  },
});
export { expect };

export async function signIn(page: Page, password = 'fixture-private-password') {
  const username = page.getByLabel('Username', { exact: true });
  await expect
    .poll(
      async () =>
        (await username.isVisible()) ||
        (await page.locator('.sidebar').isVisible()) ||
        (await page.getByLabel('Account and workspace').isVisible()),
      { timeout: 15_000 },
    )
    .toBe(true);
  if (!(await username.isVisible())) return;
  await username.fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}
