import { expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

type Profile = { username: string; password: string; userId: string };
const credentials = JSON.parse(
  readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'),
) as Profile & {
  accountsByKey?: Record<string, Profile>;
};

export async function accountWriteHeaders(page: Page) {
  const snapshot = await page.request.get('/api/v1/auth/session');
  expect(snapshot.status()).toBe(200);
  const session = await snapshot.json();
  expect(session.authenticated).toBe(true);
  expect(typeof session.csrf_token).toBe('string');
  expect(session.csrf_token).toBeTruthy();
  return {
    Origin: new URL(page.url()).origin,
    'X-CSRF-Token': session.csrf_token as string,
  };
}

export async function signInAccount(page: Page, fixtureKey?: string) {
  const profile = fixtureKey ? credentials.accountsByKey?.[fixtureKey] : credentials;
  if (!profile?.username || !profile.password)
    throw new Error(
      'This browser scenario needs a real human account profile in accountsByKey. API-key and group boundaries must be tested through API requests.',
    );
  const snapshot = await page.request.get('/api/v1/auth/session');
  expect(snapshot.status()).toBe(200);
  const current = await snapshot.json();
  if (current.authenticated && current.user?.username !== profile.username) {
    const logout = await page.request.post('/api/v1/auth/logout', {
      headers: {
        Origin: new URL(page.url()).origin,
        'X-CSRF-Token': current.csrf_token,
      },
    });
    expect(logout.status()).toBe(204);
    await page.reload();
  }
  const username = page.getByLabel('Username', { exact: true });
  await expect
    .poll(
      async () =>
        (await username.isVisible()) ||
        (await page.locator('.sidebar').isVisible()) ||
        (await page.getByLabel('Account and workspace').isVisible()),
    )
    .toBe(true);
  if (!(await username.isVisible())) return;
  await username.fill(profile.username);
  await page.getByLabel('Password', { exact: true }).fill(profile.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}
