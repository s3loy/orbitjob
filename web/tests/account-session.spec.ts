import { test, expect, fixtureAccount, fixtureIdentity } from './support';

async function signIn(page: import('@playwright/test').Page) {
  await page.getByLabel('Username', { exact: true }).fill('fixture-user');
  await page.getByLabel('Password', { exact: true }).fill('private-password-fixture');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
}

test('login restores after refresh, sends remember preference, and signs out on the server', async ({
  page,
}) => {
  let signedIn = false;
  let login: unknown;
  let logoutCSRF = '';
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/login')) {
      signedIn = true;
      login = route.request().postDataJSON();
      return route.fulfill({ json: fixtureAccount });
    }
    if (path.endsWith('/logout')) {
      signedIn = false;
      logoutCSRF = route.request().headers()['x-csrf-token'];
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
  await page.route('**/api/v1/jobs?*', (route) => route.fulfill({ json: { items: [] } }));
  await page.goto('/console/jobs');
  await expect(page.getByRole('checkbox', { name: 'Keep me signed in for 7 days' })).toBeChecked();
  await page.getByRole('checkbox', { name: 'Keep me signed in for 7 days' }).uncheck();
  await signIn(page);
  expect(login).toEqual({
    username: 'fixture-user',
    password: 'private-password-fixture',
    remember: false,
  });
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await expect(page.getByLabel('Password', { exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'Account', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Open workspace', exact: true })).toHaveCount(1);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  expect(logoutCSRF).toBe('fixture-csrf');
  await expect(page.getByRole('heading', { name: 'Sign in to OrbitJob' })).toBeVisible();
  const stored = await page.evaluate(() =>
    JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }),
  );
  expect(stored).not.toContain('private-password-fixture');
  expect(stored).not.toContain('fixture-csrf');
});

test('temporary restore outage has retry and never claims the session expired', async ({
  page,
}) => {
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ status: 503, json: {} }));
  await page.goto('/console/connection');
  await expect(page.getByLabel('Session recovery')).toContainText('temporary connection failure');
  await expect(page.getByRole('button', { name: 'Try again', exact: true })).toBeVisible();
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: fixtureAccount }));
  await page.getByRole('button', { name: 'Try again', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Account' })).toBeVisible();
});

for (const kind of ['setup', 'invite', 'reset'] as const)
  test(`${kind} capability is stripped from the URL and submitted once`, async ({ page }) => {
    let submitted = '';
    await page.route('**/api/v1/auth/**', (route) => {
      const path = new URL(route.request().url()).pathname;
      if (route.request().method() === 'POST') {
        submitted = route.request().postDataJSON().token;
        return kind === 'reset'
          ? route.fulfill({ status: 204 })
          : route.fulfill({ json: fixtureAccount });
      }
      if (path.endsWith('/session'))
        return route.fulfill({
          json: { authenticated: false, initialized: kind !== 'setup', workspaces: [] },
        });
      return route.fulfill({ json: { items: [] } });
    });
    await page.route('**/api/v1/jobs?*', (route) => route.fulfill({ json: { items: [] } }));
    await page.goto(`/console/connection#${kind}=private-one-time-token`);
    await expect(page).toHaveURL(/\/connection$/);
    if (kind !== 'reset') await page.getByLabel('Username', { exact: true }).fill('new-user');
    await page.getByLabel('New password', { exact: true }).fill('new-private-password');
    await page.getByLabel('Confirm password', { exact: true }).fill('new-private-password');
    await page
      .getByRole('button', {
        name:
          kind === 'setup'
            ? 'Create administrator account'
            : kind === 'invite'
              ? 'Accept invitation'
              : 'Reset password',
        exact: true,
      })
      .click();
    await expect.poll(() => submitted).toBe('private-one-time-token');
    expect(
      await page.evaluate(() => JSON.stringify({ ...sessionStorage, ...localStorage })),
    ).not.toContain('private-one-time-token');
  });

test('membership selection targets each request and persists only a user-scoped workspace preference', async ({
  page,
}) => {
  const another = '00000000000000000000000003';
  const account = {
    ...fixtureAccount,
    workspaces: [
      ...fixtureAccount.workspaces,
      { id: another, name: 'Another workspace', slug: 'another', role: 'viewer' },
    ],
  };
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: account }));
  await page.route('**/api/v1/identity*', (route) => {
    const tenant =
      new URL(route.request().url()).searchParams.get('tenant_id') || fixtureIdentity.tenant_id;
    return route.fulfill({
      json: { ...fixtureIdentity, tenant_id: tenant, target_tenant_id: tenant },
    });
  });
  await page.route('**/api/v1/jobs?*', (route) => route.fulfill({ json: { items: [] } }));
  await page.route('**/api/v1/instances**', (route) =>
    route.fulfill({
      json: route.request().url().includes('/summary') ? { total: 0, phases: {} } : { items: [] },
    }),
  );
  await page.goto('/console/connection');
  await page.getByLabel('Workspace', { exact: true }).selectOption(another);
  await page.getByRole('button', { name: 'Open workspace' }).click();
  await page.getByRole('link', { name: 'Jobs', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  expect(
    await page.evaluate(() =>
      sessionStorage.getItem('orbitjob.workspace.00000000000000000000000002'),
    ),
  ).toBe(another);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
});

test('server rejection clears protected records and redirects to sign in', async ({ page }) => {
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: fixtureAccount }));
  let rejected = false;
  await page.route('**/api/v1/jobs**', (route) =>
    route.fulfill(
      rejected
        ? { status: 401, json: { error: { code: 'unauthorized', message: 'Revoked session' } } }
        : { json: { items: [] } },
    ),
  );
  await page.goto('/console/jobs');
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  rejected = true;
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to OrbitJob' })).toBeVisible();
  await expect(page.locator('.sidebar')).toHaveCount(0);
});

test('failed server logout clears local data and reports the retained server session honestly', async ({
  page,
}) => {
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: fixtureAccount }));
  await page.route('**/api/v1/auth/logout', (route) => route.abort('connectionfailed'));
  await page.goto('/console/connection');
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Sign in to OrbitJob' })).toBeVisible();
  await expect(page.getByLabel('Session recovery')).toContainText(
    'server session may still be active',
  );
});

test('last administrator rejection stays actionable and one-time links are discarded explicitly', async ({
  page,
}) => {
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/session')) return route.fulfill({ json: fixtureAccount });
    if (path.endsWith('/users') && route.request().method() === 'GET')
      return route.fulfill({ json: { items: [{ ...fixtureAccount.user, status: 'active' }] } });
    if (path.includes('/users/'))
      return route.fulfill({
        status: 409,
        json: {
          error: {
            code: 'last_admin',
            message: 'The final active administrator cannot be disabled.',
          },
        },
      });
    if (path.endsWith('/invitations'))
      return route.fulfill({
        json: { token: 'private-invitation-once', expires_at: '2099-01-01T00:00:00Z' },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/api/v1/**', (route) => {
    if (
      new URL(route.request().url()).pathname.startsWith('/api/v1/auth/') ||
      new URL(route.request().url()).pathname === '/api/v1/identity'
    )
      return route.fallback();
    return route.fulfill({ json: { items: [], has_more: false } });
  });
  await page.goto('/console/administration');
  await expect(page.getByRole('tab', { name: 'Accounts', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('button', { name: 'Disable account', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('final active administrator');
  await page.getByRole('button', { name: 'Create invitation', exact: true }).click();
  await expect(
    page.getByLabel('One-time account link', { exact: true }).getByRole('textbox'),
  ).toHaveValue(/#invite=private-invitation-once/);
  await page.getByRole('tab', { name: 'Workspaces', exact: true }).click();
  await expect(page.getByRole('region', { name: 'One-time account link' })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Accounts', exact: true }).click();
  await expect(
    page.getByRole('region', { name: 'One-time account link' }).getByRole('textbox'),
  ).toHaveValue(/#invite=private-invitation-once/);
  await page.getByRole('button', { name: 'Discard link', exact: true }).click();
  await expect(page.getByRole('region', { name: 'One-time account link' })).toHaveCount(0);
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    'private-invitation-once',
  );
});

test('changing a password revokes local access and requires a fresh sign in', async ({ page }) => {
  let changed: unknown;
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: fixtureAccount }));
  await page.route('**/api/v1/auth/password/change', (route) => {
    changed = {
      body: route.request().postDataJSON(),
      csrf: route.request().headers()['x-csrf-token'],
    };
    return route.fulfill({ status: 204 });
  });
  await page.goto('/console/connection');
  await page.getByText('Change password', { exact: true }).click();
  await page.getByLabel('Current password', { exact: true }).fill('old-private-password');
  await page.getByLabel('New password', { exact: true }).fill('new-private-password');
  await page.getByLabel('Confirm new password', { exact: true }).fill('new-private-password');
  await page.getByRole('button', { name: 'Change password and sign out' }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('status')).toContainText('All sessions were signed out');
  expect(changed).toEqual({
    body: { current_password: 'old-private-password', password: 'new-private-password' },
    csrf: 'fixture-csrf',
  });
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    'private-password',
  );
});

test('a rejected current password keeps an independently valid account session', async ({
  page,
}) => {
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: fixtureAccount }));
  await page.route('**/api/v1/auth/password/change', (route) =>
    route.fulfill({
      status: 401,
      json: { error: { code: 'unauthorized', message: 'The current password was not accepted.' } },
    }),
  );
  await page.goto('/console/connection');
  await page.getByText('Change password', { exact: true }).click();
  await page.getByLabel('Current password', { exact: true }).fill('wrong-private-password');
  await page.getByLabel('New password', { exact: true }).fill('new-private-password');
  await page.getByLabel('Confirm new password', { exact: true }).fill('new-private-password');
  await page.getByRole('button', { name: 'Change password and sign out' }).click();
  await expect(page.getByRole('heading', { name: 'Account', exact: true })).toBeVisible();
  await expect(page.getByLabel('Current password', { exact: true })).toHaveValue('');
  await expect(page.getByRole('alert')).toContainText('current password was not accepted');
});

test('a different account cannot recover an unknown write or another user’s editor draft', async ({
  page,
}) => {
  const { demoClient } = await import('../src/api/demo');
  const demo = demoClient();
  const original = await demo.functionDefinition('27');
  demo.dispose();
  let account = fixtureAccount;
  let rejected = false;
  let writes = 0;
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/login')) {
      account = {
        ...fixtureAccount,
        user: {
          ...fixtureAccount.user,
          id: '00000000000000000000000004',
          username: 'other-user',
          display_name: 'Other User',
        },
      };
      rejected = false;
      return route.fulfill({ json: account });
    }
    if (path.endsWith('/session'))
      return route.fulfill({
        json: rejected ? { authenticated: false, initialized: true, workspaces: [] } : account,
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/v1/auth/')) return route.fallback();
    if (url.pathname === '/api/v1/identity')
      return route.fulfill({
        json: { ...fixtureIdentity, user_id: account.user.id, username: account.user.username },
      });
    if (url.pathname === '/api/v1/functions/27') {
      if (route.request().method() === 'PUT') {
        writes++;
        return route.abort('connectionreset');
      }
      return route.fulfill({ json: original });
    }
    if (url.pathname === '/api/v1/jobs' && rejected)
      return route.fulfill({
        status: 401,
        json: { error: { code: 'unauthorized', message: 'Session revoked' } },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/functions/27/edit');
  await page.getByLabel('Function name', { exact: true }).fill('private-alice-unknown-edit');
  await page.getByRole('button', { name: 'Review function' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByRole('heading', { name: 'Save outcome unknown' })).toBeVisible();
  rejected = true;
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await page.getByLabel('Username', { exact: true }).fill('other-user');
  await page.getByLabel('Password', { exact: true }).fill('other-private-password');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await page.goto('/console/functions/27/edit');
  await expect(page.getByLabel('Function name', { exact: true })).toHaveValue(original.name);
  await expect(page.getByRole('heading', { name: 'Save outcome unknown' })).toHaveCount(0);
  await expect(page.getByText('private-alice-unknown-edit', { exact: true })).toHaveCount(0);
  expect(writes).toBe(1);
});

test('sensitive writes request recent authentication and retry only after a deliberate second action', async ({
  page,
}) => {
  let attempts = 0;
  let verified = false;
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/session')) return route.fulfill({ json: fixtureAccount });
    if (path.endsWith('/reauth')) {
      verified = true;
      return route.fulfill({ json: fixtureAccount });
    }
    if (path.endsWith('/invitations')) {
      attempts++;
      expect(route.request().headers()['x-csrf-token']).toBe('fixture-csrf');
      return verified
        ? route.fulfill({
            json: { token: 'one-time-invitation', expires_at: '2099-01-01T00:00:00Z' },
          })
        : route.fulfill({
            status: 403,
            json: {
              error: {
                code: 'recent_auth_required',
                message: 'Confirm your password before managing access.',
              },
            },
          });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/api/v1/**', (route) => {
    if (
      new URL(route.request().url()).pathname.startsWith('/api/v1/auth/') ||
      new URL(route.request().url()).pathname === '/api/v1/identity'
    )
      return route.fallback();
    return route.fulfill({ json: { items: [], has_more: false } });
  });
  await page.goto('/console/administration');
  await page.getByRole('button', { name: 'Create invitation', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Confirm your password' });
  await expect(dialog).toBeVisible();
  expect(attempts).toBe(1);
  await dialog.getByLabel('Password', { exact: true }).fill('private-password');
  await dialog.getByRole('button', { name: 'Confirm password', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(attempts).toBe(1);
  await page.getByRole('button', { name: 'Create invitation', exact: true }).click();
  await expect(
    page.getByRole('region', { name: 'One-time account link', exact: true }),
  ).toBeVisible();
  expect(attempts).toBe(2);
});

test('revoking the current browser session clears protected data after server invalidation', async ({
  page,
}) => {
  let valid = true;
  let revoked = '';
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/session'))
      return route.fulfill({
        json: valid ? fixtureAccount : { authenticated: false, initialized: true, workspaces: [] },
      });
    if (path.endsWith('/sessions/current') && route.request().method() === 'DELETE') {
      valid = false;
      revoked = route.request().headers()['x-csrf-token'];
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({
      json: {
        items: [
          {
            id: 'current',
            created_at: '2026-11-15T00:00:00Z',
            expires_at: '2099-01-01T00:00:00Z',
            current: true,
          },
        ],
      },
    });
  });
  await page.goto('/console/connection');
  await page.getByText('Signed-in sessions', { exact: true }).click();
  await page.getByRole('button', { name: 'Revoke this session', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.sidebar')).toHaveCount(0);
  expect(revoked).toBe('fixture-csrf');
});

test('an installation administrator without a workspace creates one using only global management probes', async ({
  page,
}) => {
  const id = '00000000000000000000000009';
  const row = { id, name: 'First workspace', slug: 'first-workspace', status: 'active' };
  let created = false;
  const probes: { action: string; resource: string }[] = [];
  const writes: string[] = [];
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/session'))
      return route.fulfill({
        json: { ...fixtureAccount, workspaces: created ? [{ ...row, role: 'admin' }] : [] },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/v1/auth/')) return route.fallback();
    if (url.pathname === '/api/v1/identity') {
      const actions = url.searchParams.getAll('action');
      const resources = url.searchParams.getAll('resource');
      const capabilities = actions.map((action, index) => {
        const probe = { action, resource: resources[index] };
        probes.push(probe);
        return { ...probe, allowed: action === 'tenant:Create' };
      });
      const target = url.searchParams.get('tenant_id') || '';
      return route.fulfill({
        json: { ...fixtureIdentity, tenant_id: target, target_tenant_id: target, capabilities },
      });
    }
    if (route.request().method() !== 'GET') {
      writes.push(url.pathname);
      if (url.pathname === '/api/v1/tenants') {
        created = true;
        return route.fulfill({ status: 201, json: row });
      }
      return route.fulfill({
        status: 403,
        json: { error: { code: 'forbidden', message: 'No scoped operation was granted.' } },
      });
    }
    return route.fulfill({
      json: {
        items: url.pathname === '/api/v1/workspaces' && created ? [row] : [],
        has_more: false,
      },
    });
  });
  await page.goto('/console/connection');
  await page
    .getByRole('link', { name: 'Create a workspace in Administration', exact: true })
    .click();
  await expect(page.getByRole('heading', { name: 'Administration', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Workspaces', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveCount(
    0,
  );
  await page.getByLabel('Name', { exact: true }).fill(row.name);
  await page.getByLabel('Slug', { exact: true }).fill(row.slug);
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await expect(
    page.getByRole('rowheader', { name: `${row.name} ${row.slug}`, exact: true }),
  ).toBeVisible();
  expect(probes.length).toBeGreaterThan(0);
  expect(
    probes.every(
      (probe) => probe.action === 'tenant:Create' && probe.resource === 'orbitjob:*:*:tenant/*',
    ),
  ).toBe(true);
  expect(writes).toEqual(['/api/v1/tenants']);
  await page.getByRole('link', { name: 'OrbitJob', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Account', exact: true })).toBeVisible();
  await page.getByLabel('Workspace', { exact: true }).selectOption(id);
  await page.getByRole('button', { name: 'Open workspace', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  expect(writes).toEqual(['/api/v1/tenants']);
});

for (const allowed of [true, false, 'forbidden'] as const)
  test(`Administration uses workspace B metadata with grant=${allowed} while business requests stay in A`, async ({
    page,
  }) => {
    const other = '00000000000000000000000008';
    const targetedProbes: string[] = [];
    const businessTargets: string[] = [];
    await page.route('**/api/v1/auth/**', (route) => {
      if (new URL(route.request().url()).pathname.endsWith('/session'))
        return route.fulfill({
          json: {
            ...fixtureAccount,
            user: { ...fixtureAccount.user, installation_admin: false },
            workspaces: [
              ...fixtureAccount.workspaces,
              ...(allowed === 'forbidden'
                ? []
                : [{ id: other, name: 'Other workspace', slug: 'other-workspace', role: 'admin' }]),
            ],
          },
        });
      return route.fulfill({ json: { items: [] } });
    });
    await page.route('**/api/v1/**', (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.startsWith('/api/v1/auth/')) return route.fallback();
      if (url.pathname === '/api/v1/identity') {
        const target = url.searchParams.get('tenant_id') || '';
        const actions = url.searchParams.getAll('action');
        const resources = url.searchParams.getAll('resource');
        if (actions.length) targetedProbes.push(target);
        if (target === other && allowed === 'forbidden')
          return route.fulfill({
            status: 403,
            json: {
              error: {
                code: 'forbidden',
                message: 'This account is not a member of this workspace.',
              },
            },
          });
        return route.fulfill({
          json: {
            ...fixtureIdentity,
            installation_admin: false,
            tenant_id: target,
            target_tenant_id: target,
            capabilities: actions.map((action, index) => ({
              action,
              resource: resources[index],
              allowed: target === other ? allowed === true : true,
            })),
          },
        });
      }
      if (url.pathname === '/api/v1/jobs')
        businessTargets.push(url.searchParams.get('tenant_id') || '');
      return route.fulfill({ json: { items: [], has_more: false } });
    });
    await page.goto(`/console/administration?panel=keys&workspace=${other}`);
    if (allowed === true)
      await expect(
        page.getByRole('heading', { name: 'Create API key', exact: true }),
      ).toBeVisible();
    else {
      await expect(page.getByRole('region', { name: 'Management access required' })).toBeVisible();
      await expect(page.getByRole('heading', { name: 'Create API key', exact: true })).toHaveCount(
        0,
      );
    }
    expect(targetedProbes).toContain(other);
    expect(targetedProbes.every((id) => id === other)).toBe(true);
    await page
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Jobs', exact: true })
      .click();
    await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
    expect(businessTargets).toEqual([fixtureIdentity.tenant_id]);
  });
