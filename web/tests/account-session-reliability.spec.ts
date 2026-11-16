import { ulid } from 'ulid';
import { test, expect, fixtureAccount, fixtureIdentity, type Page, type Route } from './support';
import type { AccountSnapshot } from '../src/api/accounts';
import { demoClient } from '../src/api/demo';

// These HTTP mocks verify browser behavior, not an installed server's authorization.
test.describe('account session reliability with mocked HTTP', () => {
  function account(username = 'mock-account-user', installationAdmin = false): AccountSnapshot {
    return {
      ...fixtureAccount,
      user: {
        ...fixtureAccount.user,
        id: ulid(),
        username,
        display_name: username,
        installation_admin: installationAdmin,
      },
      workspaces: [
        { id: ulid(), name: 'Mock account workspace', slug: 'mock-account', role: 'admin' },
      ],
    };
  }

  function anonymous(initialized = true): AccountSnapshot {
    return { authenticated: false, initialized, workspaces: [] };
  }

  async function mockInstallation(page: Page, current: () => AccountSnapshot) {
    await page.route('**/api/v1/**', (route) => {
      const url = new URL(route.request().url());
      const snapshot = current();
      if (url.pathname === '/api/v1/auth/session') return route.fulfill({ json: snapshot });
      if (url.pathname === '/api/v1/identity') {
        const tenant = url.searchParams.get('tenant_id') || snapshot.workspaces[0]?.id || '';
        return route.fulfill({
          json: {
            ...fixtureIdentity,
            user_id: snapshot.user?.id || '',
            username: snapshot.user?.username || '',
            installation_admin: snapshot.user?.installation_admin || false,
            tenant_id: tenant,
            target_tenant_id: tenant,
          },
        });
      }
      if (url.pathname.startsWith('/api/v1/auth/')) return route.abort('blockedbyclient');
      return route.fulfill({ json: { items: [], has_more: false } });
    });
  }

  async function expectNotStored(page: Page, ...values: string[]) {
    const storage = await page.evaluate(() =>
      JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }),
    );
    for (const value of values) expect(storage).not.toContain(value);
  }

  for (const scenario of [
    { rotated: true, status: 401 },
    { rotated: true, status: 200 },
    { rotated: false, status: 200 },
  ]) {
    test(`a late account ${scenario.status} response is ${scenario.rotated ? 'discarded after same-user session replacement' : 'retained after a same-session refresh'}`, async ({
      page,
    }) => {
      const original = account('mock-session-race-user');
      const replacement: AccountSnapshot = {
        ...original,
        user: { ...original.user!, display_name: 'Refreshed session-race account' },
        csrf_token: scenario.rotated ? 'mock-replacement-binding' : original.csrf_token,
      };
      let current = original;
      await mockInstallation(page, () => current);
      let capturedOld: Route | undefined;
      let oldFailed = false;
      let oldFinished = false;
      let releaseOld!: () => void;
      const release = new Promise<void>((resolve) => {
        releaseOld = resolve;
      });
      const staleSession = {
        id: ulid(),
        created_at: '2026-11-15T00:00:00Z',
        expires_at: '2099-01-01T00:00:00Z',
        current: false,
      };
      page.on('requestfailed', (request) => {
        if (capturedOld && request === capturedOld.request()) oldFailed = true;
      });
      await page.route('**/api/v1/auth/sessions', async (route) => {
        if (capturedOld) return route.fulfill({ json: { items: [] } });
        capturedOld = route;
        try {
          await release;
          if (!route.request().failure())
            await route.fulfill({
              status: scenario.status,
              json:
                scenario.status === 200
                  ? { items: [staleSession] }
                  : { error: { code: 'unauthorized', message: 'The previous session ended.' } },
            });
        } finally {
          oldFinished = true;
        }
      });
      let rejectedProbe = false;
      await page.route('**/api/v1/identity*', (route) => {
        const url = new URL(route.request().url());
        const actions = url.searchParams.getAll('action');
        if (actions.length && !rejectedProbe) {
          rejectedProbe = true;
          current = replacement;
          return route.fulfill({
            status: 403,
            json: {
              error: { code: 'forbidden', message: 'Refresh the current account state.' },
            },
          });
        }
        return route.fulfill({
          json: {
            ...fixtureIdentity,
            user_id: current.user!.id,
            tenant_id: current.workspaces[0].id,
            target_tenant_id: current.workspaces[0].id,
            capabilities: actions.map((action, index) => ({
              action,
              resource: url.searchParams.getAll('resource')[index],
              allowed: true,
            })),
          },
        });
      });
      let logoutCSRF = '';
      await page.route('**/api/v1/auth/logout', (route) => {
        logoutCSRF = route.request().headers()['x-csrf-token'];
        current = anonymous();
        return route.fulfill({ status: 204 });
      });
      try {
        await page.goto('/console/connection');
        await expect.poll(() => Boolean(capturedOld)).toBe(true);
        await page.getByText('Check a permission', { exact: true }).click();
        await page.getByLabel('Capability action').fill('job:Get');
        await page
          .getByLabel('Capability resource')
          .fill(`orbitjob:${original.workspaces[0].id}:*:job/*`);
        await page.getByRole('button', { name: 'Check policy grant', exact: true }).click();
        await expect(
          page.getByRole('heading', { name: 'Refreshed session-race account', exact: true }),
        ).toBeVisible();
        if (scenario.rotated) await expect.poll(() => oldFailed).toBe(true);
        else expect(oldFailed).toBe(false);
        releaseOld();
        await expect.poll(() => oldFinished).toBe(true);
        await expect(
          page.getByRole('region', { name: 'Account and workspace', exact: true }),
        ).toBeVisible();
        await page.getByText('Signed-in sessions', { exact: true }).click();
        if (scenario.rotated)
          await expect(page.getByText('Another browser session', { exact: true })).toHaveCount(0);
        else await expect(page.getByText('Another browser session', { exact: true })).toBeVisible();
        await page.getByRole('button', { name: 'Sign out', exact: true }).click();
        await expect(
          page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
        ).toBeVisible();
        expect(logoutCSRF).toBe(replacement.csrf_token);
      } finally {
        releaseOld();
      }
    });
  }

  for (const replacedUser of [false, true]) {
    test(`a refreshed ${replacedUser ? 'different account' : 'session binding'} rebuilds business credentials without replaying a rejected write`, async ({
      page,
    }) => {
      const original = account('mock-original-user');
      const replacement: AccountSnapshot = {
        ...original,
        user: replacedUser
          ? { ...original.user!, id: ulid(), username: 'mock-replacement-user' }
          : original.user,
        csrf_token: 'mock-replacement-csrf',
      };
      let current = original;
      await mockInstallation(page, () => current);
      const demo = demoClient();
      const saved = await demo.job('17');
      demo.dispose();
      const beforeName = 'mock-before-session-refresh';
      const afterName = 'mock-after-session-refresh';
      const writes: string[] = [];
      let identityRequests = 0;
      await page.route('**/api/v1/identity*', async (route) => {
        identityRequests++;
        await route.fallback();
      });
      await page.route('**/api/v1/jobs**', (route) => {
        const path = new URL(route.request().url()).pathname;
        if (path.endsWith('/trigger')) {
          writes.push(route.request().headers()['x-csrf-token']);
          if (writes.length === 1) {
            current = replacement;
            return route.fulfill({
              status: 403,
              json: { error: { code: 'forbidden', message: 'Session binding changed.' } },
            });
          }
          return route.fulfill({
            json: {
              namespace: saved.namespace,
              name: 'mock-after-refresh-run',
              occurrence_key: 'mock-after-refresh-occurrence',
              trigger: 'Manual',
              phase: 'Pending',
              created: true,
            },
          });
        }
        return route.fulfill({
          json: {
            items: [
              {
                ...saved,
                source_kind: 'kubernetes',
                name: current === original ? beforeName : afterName,
              },
            ],
          },
        });
      });
      await page.goto('/console/jobs');
      await page.getByRole('button', { name: `Trigger ${beforeName}`, exact: true }).click();
      await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
      await expect(
        page.getByRole('button', { name: `Trigger ${afterName}`, exact: true }),
      ).toBeVisible();
      await expect(page.getByRole('link', { name: beforeName, exact: true })).toHaveCount(0);
      expect(identityRequests).toBe(2);
      expect(writes).toEqual(['fixture-csrf']);
      await page.getByRole('button', { name: `Trigger ${afterName}`, exact: true }).click();
      await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
      await expect.poll(() => writes).toEqual(['fixture-csrf', 'mock-replacement-csrf']);
      await expectNotStored(page, 'fixture-csrf', 'mock-replacement-csrf');
    });
  }

  type CapabilityKind = 'setup' | 'invite' | 'reset';
  const submitNames = {
    setup: 'Create administrator account',
    invite: 'Accept invitation',
    reset: 'Reset password',
  };

  async function openCapability(page: Page, kind: CapabilityKind, token: string) {
    await page.goto(`/console/connection#${kind}=${encodeURIComponent(token)}`);
    await expect(page).toHaveURL(/\/console\/connection$/);
    await expect(page.getByRole('button', { name: submitNames[kind], exact: true })).toBeVisible();
    await expectNotStored(page, token);
  }

  async function newPassword(page: Page, kind: CapabilityKind, password: string) {
    if (kind !== 'reset')
      await page.getByLabel('Username', { exact: true }).fill('mock-created-user');
    await page.getByLabel('New password', { exact: true }).fill(password);
    await page.getByLabel('Confirm password', { exact: true }).fill(password);
    await page.getByRole('button', { name: submitNames[kind], exact: true }).click();
  }

  test('a signed-in user accepts an invitation with only its token', async ({ page }) => {
    const signedIn = account('mock-signed-in-user');
    const token = `mock-invite-${ulid()}`;
    const accepts: unknown[] = [];
    await mockInstallation(page, () => signedIn);
    await page.route('**/api/v1/auth/invitations/accept', (route) => {
      accepts.push({
        body: route.request().postDataJSON(),
        csrf: route.request().headers()['x-csrf-token'],
      });
      return route.fulfill({ json: signedIn });
    });
    await openCapability(page, 'invite', token);
    await expect(page.getByText(/Accept this invitation as/)).toContainText(
      signedIn.user!.username,
    );
    await expect(page.getByLabel('Username', { exact: true })).toHaveCount(0);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    expect(accepts).toEqual([]);
    await page.getByRole('button', { name: 'Accept invitation', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
    expect(accepts).toEqual([{ body: { token }, csrf: 'fixture-csrf' }]);
    await expectNotStored(page, token, 'fixture-csrf');
    expect(page.url()).not.toContain(token);
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
    expect(accepts).toHaveLength(1);
  });

  test('an existing account signs in before separately confirming the invitation', async ({
    page,
  }) => {
    const signedIn = account('mock-existing-user');
    let snapshot = anonymous();
    const token = `mock-invite-${ulid()}`;
    const password = 'mock-existing-private-password';
    const logins: unknown[] = [];
    const accepts: unknown[] = [];
    await mockInstallation(page, () => snapshot);
    await page.route('**/api/v1/auth/login', (route) => {
      logins.push(route.request().postDataJSON());
      snapshot = signedIn;
      return route.fulfill({ json: snapshot });
    });
    await page.route('**/api/v1/auth/invitations/accept', (route) => {
      accepts.push(route.request().postDataJSON());
      return route.fulfill({ json: signedIn });
    });
    await openCapability(page, 'invite', token);
    await page.getByRole('checkbox', { name: 'I already have an account' }).check();
    await expect(page.getByLabel('New password', { exact: true })).toHaveCount(0);
    await expect(page.getByLabel('Confirm password', { exact: true })).toHaveCount(0);
    await page.getByLabel('Username', { exact: true }).fill(signedIn.user!.username);
    await page.getByLabel('Password', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Accept invitation', exact: true }).click();
    await expect(page.getByText(/Accept this invitation as/)).toContainText(
      signedIn.user!.username,
    );
    expect(logins).toEqual([{ username: signedIn.user!.username, password, remember: true }]);
    expect(accepts).toEqual([]);
    await expect(page.getByLabel('Username', { exact: true })).toHaveCount(0);
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
    await expectNotStored(page, token, password);
    await page.getByRole('button', { name: 'Accept invitation', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
    expect(accepts).toEqual([{ token }]);
    await expectNotStored(page, token, password);
  });

  test('an invitation rejection keeps the current user and sends no username', async ({ page }) => {
    const signedIn = account('mock-different-user');
    const token = `mock-invite-for-another-user-${ulid()}`;
    const accepts: unknown[] = [];
    await mockInstallation(page, () => signedIn);
    await page.route('**/api/v1/auth/invitations/accept', (route) => {
      accepts.push(route.request().postDataJSON());
      return route.fulfill({
        status: 403,
        json: {
          error: {
            code: 'forbidden',
            message: 'This invitation belongs to another account.',
          },
        },
      });
    });
    await openCapability(page, 'invite', token);
    await expect(page.getByText(/Accept this invitation as/)).toContainText(
      signedIn.user!.username,
    );
    await expect(page.getByLabel('Username', { exact: true })).toHaveCount(0);
    await page.getByRole('button', { name: 'Accept invitation', exact: true }).click();
    await expect(page.getByRole('alert')).toContainText('belongs to another account');
    expect(accepts).toEqual([{ token }]);
    await expect(page.getByText(/Accept this invitation as/)).toContainText(
      signedIn.user!.username,
    );
    await expectNotStored(page, token);
    await page.getByRole('button', { name: 'Discard link', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Account', exact: true })).toBeVisible();
  });

  for (const kind of ['setup', 'invite', 'reset'] as const) {
    test(`${kind} rejects fewer than 15 Unicode code points and more than 1024 UTF-8 bytes before API submission`, async ({
      page,
    }) => {
      const snapshot = anonymous(kind !== 'setup');
      const token = `mock-${kind}-${ulid()}`;
      let writes = 0;
      await mockInstallation(page, () => snapshot);
      await page.route('**/api/v1/auth/**', (route) => {
        if (route.request().method() !== 'POST') return route.fallback();
        writes++;
        return route.fulfill({ status: 500, json: {} });
      });
      await openCapability(page, kind, token);
      const shortPasswords = ['😀'.repeat(8), '😀'.repeat(14)];
      for (const tooShort of shortPasswords) {
        await newPassword(page, kind, tooShort);
        await expect(page.getByRole('alert')).toContainText('at least 15 characters');
        expect(writes).toBe(0);
      }
      const tooLong = '😀'.repeat(256) + 'a';
      await newPassword(page, kind, tooLong);
      await expect(page.getByRole('alert')).toContainText('exceeds the supported length');
      expect(writes).toBe(0);
      await expectNotStored(page, token, ...shortPasswords, tooLong);
      await page.reload();
      await expect(
        page.getByRole('heading', {
          name: 'Sign in to OrbitJob',
          exact: true,
        }),
      ).toBeVisible();
      await expect(page.getByLabel('New password', { exact: true })).toHaveCount(0);
      expect(writes).toBe(0);
    });

    for (const boundary of [
      { name: '15 Unicode code points', password: '😀'.repeat(14) + 'a' },
      { name: '1024 UTF-8 bytes', password: '😀'.repeat(256) },
    ]) {
      test(`${kind} submits a new password at the ${boundary.name} boundary`, async ({ page }) => {
        let snapshot = anonymous(kind !== 'setup');
        const created = account('mock-created-user');
        const token = `mock-${kind}-${ulid()}`;
        const submissions: unknown[] = [];
        await mockInstallation(page, () => snapshot);
        const endpoint =
          kind === 'setup'
            ? '/setup'
            : kind === 'invite'
              ? '/invitations/accept'
              : '/password/reset';
        await page.route(`**/api/v1/auth${endpoint}`, (route) => {
          submissions.push(route.request().postDataJSON());
          if (kind === 'reset') return route.fulfill({ status: 204 });
          snapshot = created;
          return route.fulfill({ json: snapshot });
        });
        await openCapability(page, kind, token);
        await newPassword(page, kind, boundary.password);
        await expect(
          page.getByRole('heading', {
            name: kind === 'reset' ? 'Sign in to OrbitJob' : 'Jobs',
            exact: true,
          }),
        ).toBeVisible();
        expect(submissions).toEqual([
          {
            token,
            password: boundary.password,
            ...(kind === 'reset' ? {} : { username: 'mock-created-user', display_name: '' }),
          },
        ]);
        await expectNotStored(page, token, boundary.password);
        expect(page.url()).not.toContain(token);
      });
    }
  }

  async function mockMembers(page: Page, snapshot: AccountSnapshot) {
    const member = account('mock-workspace-member').user!;
    await page.route('**/api/v1/auth/members?*', (route) =>
      route.fulfill({
        json: {
          items: [
            {
              user: member,
              tenant_id: snapshot.workspaces[0].id,
              role: 'viewer',
              status: 'active',
            },
          ],
        },
      }),
    );
    await page.route('**/api/v1/auth/users', (route) =>
      route.fulfill({ json: { items: [{ ...member, status: 'active' }] } }),
    );
    return member;
  }

  test('a workspace administrator cannot see global password reset controls', async ({ page }) => {
    const signedIn = account('mock-workspace-admin');
    await mockInstallation(page, () => signedIn);
    const member = await mockMembers(page, signedIn);
    const resetRequests: string[] = [];
    page.on('request', (request) => {
      if (request.url().includes('/password-reset')) resetRequests.push(request.url());
    });
    await page.goto('/console/administration');
    const administration = page.getByRole('region', { name: 'Account administration' });
    await expect(administration.getByRole('cell', { name: member.username })).toBeVisible();
    await expect(administration.getByRole('button', { name: 'Disable member' })).toBeVisible();
    await expect(
      administration.getByRole('button', { name: 'Create password reset link', exact: true }),
    ).toHaveCount(0);
    await expect(
      administration.getByRole('heading', { name: 'Installation accounts' }),
    ).toHaveCount(0);
    expect(resetRequests).toEqual([]);
  });

  test('an installation administrator confirms the password then explicitly retries reset with CSRF', async ({
    page,
  }) => {
    let signedIn = account('mock-installation-admin', true);
    const token = `mock-reset-${ulid()}`;
    const password = 'mock-reauth-private-password';
    const resetRequests: unknown[] = [];
    const reauthRequests: unknown[] = [];
    let recentlyAuthenticated = false;
    await mockInstallation(page, () => signedIn);
    const member = await mockMembers(page, signedIn);
    await page.route(`**/api/v1/auth/members/${member.id}/password-reset`, (route) => {
      resetRequests.push({
        body: route.request().postDataJSON(),
        csrf: route.request().headers()['x-csrf-token'],
      });
      if (!recentlyAuthenticated)
        return route.fulfill({
          status: 403,
          json: {
            error: {
              code: 'recent_auth_required',
              message: 'Recent password authentication required.',
            },
          },
        });
      return route.fulfill({
        status: 201,
        json: { token, expires_at: '2099-01-01T00:00:00Z' },
      });
    });
    await page.route('**/api/v1/auth/reauth', (route) => {
      reauthRequests.push({
        body: route.request().postDataJSON(),
        csrf: route.request().headers()['x-csrf-token'],
      });
      recentlyAuthenticated = true;
      signedIn = { ...signedIn, csrf_token: 'mock-reauth-csrf' };
      return route.fulfill({ json: signedIn });
    });
    await page.goto('/console/administration');
    const reset = page.getByRole('button', { name: 'Create password reset link', exact: true });
    await expect(reset).toBeVisible();
    await reset.click();
    const dialog = page.getByRole('dialog', { name: 'Confirm your password' });
    await expect(dialog).toContainText('After confirmation, choose the action again.');
    await expect(page.getByRole('region', { name: 'One-time account link' })).toHaveCount(0);
    await dialog.getByLabel('Password', { exact: true }).fill(password);
    await dialog.getByRole('button', { name: 'Confirm password', exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect(reauthRequests).toEqual([{ body: { password }, csrf: 'fixture-csrf' }]);
    expect(resetRequests).toHaveLength(1);
    await reset.click();
    const revealed = page.getByRole('region', { name: 'One-time account link' });
    await expect(revealed.getByRole('textbox')).toHaveValue(new RegExp(`#reset=${token}$`));
    expect(resetRequests).toEqual([
      { body: { tenant_id: signedIn.workspaces[0].id }, csrf: 'fixture-csrf' },
      { body: { tenant_id: signedIn.workspaces[0].id }, csrf: 'mock-reauth-csrf' },
    ]);
    await expectNotStored(page, token, password, fixtureAccount.csrf_token!, signedIn.csrf_token!);
    expect(page.url()).not.toContain(token);
    await revealed.getByRole('button', { name: 'Discard link', exact: true }).click();
    await expect(revealed).toHaveCount(0);
    await expectNotStored(page, token);
  });
});
