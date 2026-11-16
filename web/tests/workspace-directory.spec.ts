import { signIn } from './support';
import { expect, test, fixtureAccount, type Page } from './support';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });
const own = '00000000000000000000000001';
const other = '00000000000000000000000032';
const workspace = (id: string, name: string, slug: string) => ({
  id,
  name,
  slug,
  status: 'active',
});

async function prepare(page: Page, kind: 'admin' | 'platform' | 'ordinary' | 'group' = 'admin') {
  const state = {
    rows: [workspace(own, 'Operations', 'operations')],
    status: 200,
    reads: [] as URL[],
    identityTargets: [] as (string | null)[],
    connectionIdentityTargets: [] as (string | null)[],
    writes: [] as string[],
    failScopes: new Set<string>(),
  };
  let signedIn = false;
  await page.route('**/api/v1/auth/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/login')) signedIn = true;
    if (path.endsWith('/logout')) {
      signedIn = false;
      return route.fulfill({ status: 204 });
    }
    if (path.endsWith('/login') || path.endsWith('/session'))
      return route.fulfill({
        json: signedIn
          ? {
              ...fixtureAccount,
              user: {
                ...fixtureAccount.user,
                installation_admin: kind === 'admin' || kind === 'platform',
              },
              workspaces: state.rows
                .filter((row) => (kind !== 'ordinary' && kind !== 'group') || row.id === own)
                .map((row) => ({
                  ...row,
                  role: kind === 'ordinary' || kind === 'group' ? 'viewer' : 'admin',
                })),
            }
          : { authenticated: false, initialized: true, workspaces: [] },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path === '/api/v1/identity') {
      const target = url.searchParams.get('tenant_id');
      state.identityTargets.push(target);
      if (!url.searchParams.has('action')) state.connectionIdentityTargets.push(target);
      return route.fulfill({
        json: {
          key_id: '',
          subject_type: 'user',
          user_id: fixtureAccount.user.id,
          username: fixtureAccount.user.username,
          kind: 'tenant',
          tenant_id: target || own,
          target_tenant_id: target || own,
          tenant_name: target === other ? '\u8d22\u52a1\u56e2\u961f' : 'Operations',
          tenant_slug: target === other ? 'finance-team' : 'operations',
          group_id: '',
          boundary_policy_id: '',
          workload_namespaces: [],
          capabilities: url.searchParams.getAll('action').map((action, index) => ({
            action,
            resource: url.searchParams.getAll('resource')[index],
            allowed: kind === 'admin' || kind === 'platform',
          })),
        },
      });
    }
    if (path === '/api/v1/workspaces') {
      state.reads.push(url);
      if (state.status !== 200)
        return route.fulfill({
          status: state.status,
          json: {
            error: {
              code: state.status === 401 ? 'UNAUTHORIZED' : 'FORBIDDEN',
              message: 'Directory request rejected.',
            },
          },
        });
      const search = (url.searchParams.get('search') || '').toLowerCase();
      const offset = Number(url.searchParams.get('offset') || 0);
      const visible = (
        kind === 'ordinary' || kind === 'group'
          ? state.rows.filter((row) => row.id === own)
          : state.rows
      ).filter(
        (row) => row.name.toLowerCase().includes(search) || row.slug.toLowerCase().includes(search),
      );
      return route.fulfill({
        json: { items: visible.slice(offset, offset + 20), has_more: visible.length > offset + 20 },
      });
    }
    if (request.method() === 'POST') {
      state.writes.push(path);
      const input = request.postDataJSON();
      if (state.failScopes.has(path))
        return route.fulfill({
          status: 503,
          json: { error: { code: 'UNAVAILABLE', message: 'Write response unavailable.' } },
        });
      if (path === '/api/v1/tenants') {
        const row = workspace(other, input.name, input.slug);
        state.rows.push(row);
        return route.fulfill({ status: 201, json: row });
      }
      if (path.endsWith('/api_keys'))
        return route.fulfill({
          status: 201,
          json: {
            id: 'new-key',
            name: input.name,
            key_prefix: 'fixture',
            key: 'otj_fixture_secret_not_real',
            created_at: '2026-10-25T00:00:00Z',
          },
        });
    }
    if (path.endsWith('/policies'))
      return route.fulfill({
        json: {
          items: [
            {
              id: 'policy',
              name: 'Operator policy',
              description: 'Operate tasks',
              platform: false,
              document: {
                version: '1',
                statement: [{ effect: 'Allow', action: ['job:Get'], resource: ['*'] }],
              },
            },
          ],
        },
      });
    return route.fulfill({
      json: path.endsWith('/summary') ? { total: 0, phases: {} } : { items: [] },
    });
  });
  return state;
}

async function connect(page: Page, path = '/console/administration?panel=tenants') {
  await page.goto(path);
  await signIn(page, 'otj_workspace_fixture_not_real');
  if (new URL(path, 'http://console.invalid').pathname === '/console/connection') {
    await expect(page).toHaveURL(/\/console\/overview$/);
    await page
      .locator('.sidebar-bottom')
      .getByRole('link', { name: 'Account', exact: true })
      .click();
    await expect(page.getByRole('heading', { name: 'Account', exact: true })).toBeVisible();
  }
  if (new URL(path, 'http://console.invalid').pathname === '/console/administration') {
    await expect(page.getByRole('heading', { name: 'Administration', exact: true })).toBeVisible();
    await expect(
      page.getByRole('table').getByRole('rowheader', { name: 'Operations', exact: true }),
    ).toBeVisible();
  }
}

async function openManagementLink(page: Page, workspaceID: string) {
  await page.evaluate((target) => {
    window.history.pushState({}, '', `/console/administration?workspace=${target}&panel=keys`);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, workspaceID);
}

test('a human account selects a membership from a large directory and validates its identity before entry', async ({
  page,
}) => {
  const state = await prepare(page, 'platform');
  state.rows.push(
    ...Array.from({ length: 20 }, (_, index) =>
      workspace(
        `0000000000000000000000${String(index + 10).padStart(4, '0')}`,
        `Team ${index}`,
        `team-${index}`,
      ),
    ),
    workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'),
  );
  await connect(page, '/console/connection');
  await page.getByLabel('Workspace', { exact: true }).selectOption(other);
  await page.getByRole('button', { name: 'Open workspace', exact: true }).click();
  await expect(page).toHaveURL(/\/console\/overview$/);
  expect(state.connectionIdentityTargets).toEqual([own, other]);
  expect(state.reads).toEqual([]);
});

test('server refresh and a new connection discover a created workspace by Chinese name and slug', async ({
  page,
}) => {
  const state = await prepare(page);
  await connect(page);
  await page.getByLabel('Name', { exact: true }).fill('\u8d22\u52a1\u56e2\u961f');
  await page.getByLabel('Slug', { exact: true }).fill('finance-team');
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(1);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await page.getByLabel('Search workspace', { exact: true }).fill('\u8d22\u52a1');
  await page
    .getByRole('button', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true })
    .click();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    '\u8d22\u52a1\u56e2\u961f',
  );
  await page.reload();
  await signIn(page, 'otj_workspace_reconnected_fixture');
  await expect(page.getByRole('heading', { name: 'Administration', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByLabel('Search workspace', { exact: true }).fill('finance-team');
  await expect(
    page.getByRole('button', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toBeVisible();
  expect(state.writes).toEqual(['/api/v1/tenants']);
});

test('an installation administrator manages foreign workspace access without changing the workload connection', async ({
  page,
}) => {
  const state = await prepare(page);
  state.rows.push(workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'));
  await connect(page);
  await page
    .getByRole('row', { name: /\u8d22\u52a1\u56e2\u961f finance-team/ })
    .getByRole('button', { name: 'Manage access', exact: true })
    .click();
  await expect(page.getByRole('heading', { name: 'API keys', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    '\u8d22\u52a1\u56e2\u961f',
  );
  await page.getByText('Use an exact workspace ID', { exact: true }).click();
  await expect(page.getByLabel('Workspace ID', { exact: true })).toHaveValue(other);
  expect(state.identityTargets).toContain(other);
  expect(state.connectionIdentityTargets).toEqual([own]);
  await expect(page.locator('.sidebar .workspace')).toContainText('Operations');
  expect(state.writes).toEqual([]);
});

for (const kind of ['ordinary', 'group'] as const)
  test(`${kind} directory contains only its own workspace and cannot create a foreign target`, async ({
    page,
  }) => {
    const state = await prepare(page, kind);
    state.rows.push(workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'));
    await connect(page);
    await expect(page.getByRole('rowheader', { name: 'Operations', exact: true })).toBeVisible();
    await expect(page.getByRole('rowheader', { name: /\u8d22\u52a1\u56e2\u961f/ })).toHaveCount(0);
    await expect(page.getByRole('region', { name: 'Management access required' })).toBeVisible();
    await page.getByLabel('Search workspaces', { exact: true }).fill('finance');
    await expect(
      page.getByRole('rowheader', { name: /Operations|\u8d22\u52a1\u56e2\u961f/ }),
    ).toHaveCount(0);
    expect(state.reads.at(-1)?.searchParams.get('search')).toBe('finance');
    expect(state.writes).toEqual([]);
  });

test('directory failure is retryable and a later 401 hides cached choices and management forms', async ({
  page,
}) => {
  const state = await prepare(page);
  state.rows.push(workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'));
  await connect(page);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await expect(
    page.getByRole('button', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toBeVisible();
  state.status = 403;
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Directory request rejected');
  await expect(
    page.getByRole('button', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText('No matching workspaces', { exact: false })).toHaveCount(0);
  state.status = 200;
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(
    page.getByRole('button', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toBeVisible();
  state.status = 401;
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Your session has ended.');
  await expect(
    page.getByRole('button', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toHaveCount(0);
  await expect(page.getByLabel('Name', { exact: true })).toHaveCount(0);
});

test('a foreign workspace link cannot override a revealed key scope', async ({ page }) => {
  const state = await prepare(page);
  state.rows.push(workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'));
  await connect(page);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Scope locked fixture');
  await page.getByRole('checkbox', { name: /Operator policy/ }).check();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByLabel('Generated API key', { exact: true })).toHaveValue(
    'otj_fixture_secret_not_real',
  );
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  await page.getByLabel('Workspace', { exact: true }).selectOption(other);
  await page.getByRole('button', { name: 'Open workspace', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(
    'Finish pending access changes and save or discard revealed credentials before switching workspaces.',
  );
  await expect(page.locator('.sidebar .workspace')).toContainText('Operations');
  await openManagementLink(page, other);
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    'Operations',
  );
  await expect(page.getByText('Finish the pending operation', { exact: false })).toBeVisible();
  await expect(page.getByLabel('Generated API key', { exact: true })).toHaveValue(
    'otj_fixture_secret_not_real',
  );
  expect(state.writes).toEqual([`/api/v1/tenants/${own}/api_keys`]);
});

test('administrator table paginates and searches the authorized server directory', async ({
  page,
}) => {
  const state = await prepare(page);
  state.rows.push(
    ...Array.from({ length: 20 }, (_, index) =>
      workspace(
        `0000000000000000000000${String(index + 10).padStart(4, '0')}`,
        `Team ${index}`,
        `team-${index}`,
      ),
    ),
    workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'),
  );
  await connect(page);
  await expect(page.getByRole('heading', { name: 'Workspaces', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Next page', exact: true }).click();
  await expect(
    page.getByRole('rowheader', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toBeVisible();
  expect(state.reads.at(-1)?.searchParams.get('offset')).toBe('20');
  await page.getByLabel('Search workspaces', { exact: true }).fill('\u8d22\u52a1');
  await expect(
    page.getByRole('rowheader', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Previous page', exact: true })).toBeDisabled();
  expect(state.reads.at(-1)?.searchParams.get('search')).toBe('\u8d22\u52a1');
  expect(state.reads.at(-1)?.searchParams.get('offset')).toBe('0');
  await page
    .getByRole('row', { name: /\u8d22\u52a1\u56e2\u961f finance-team/ })
    .getByRole('button', { name: 'Manage access', exact: true })
    .click();
  await expect(page.getByRole('heading', { name: 'API keys', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    '\u8d22\u52a1\u56e2\u961f',
  );
});

test('an Administration 401 suppresses successful directory caches after returning to Connection', async ({
  page,
}) => {
  const state = await prepare(page);
  state.rows.push(workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'));
  await connect(page, '/console/connection');
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  await expect(page.getByLabel('Workspace', { exact: true })).toContainText(
    '\u8d22\u52a1\u56e2\u961f',
  );
  await page.getByRole('link', { name: 'Administration', exact: true }).click();
  await page.getByRole('tab', { name: 'Workspaces', exact: true }).click();
  await expect(
    page.getByRole('rowheader', { name: '\u8d22\u52a1\u56e2\u961f finance-team', exact: true }),
  ).toBeVisible();
  state.status = 401;
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Your session has ended.');
  const reads = state.reads.length;
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel('Workspace', { exact: true })).toHaveCount(0);
  await expect(page.locator('.sidebar')).toHaveCount(0);
  expect(state.reads).toHaveLength(reads);
  expect(state.writes).toEqual([]);
});

test('multiple unknown management writes remain reachable in their original workspace after navigation', async ({
  page,
}) => {
  const state = await prepare(page);
  const third = '00000000000000000000000033';
  state.rows.push(
    workspace(other, '\u8d22\u52a1\u56e2\u961f', 'finance-team'),
    workspace(third, 'Support', 'support'),
  );
  const policyPath = `/api/v1/tenants/${other}/policies`;
  const groupPath = `/api/v1/tenants/${other}/resource_groups`;
  state.failScopes.add(policyPath);
  state.failScopes.add(groupPath);
  await connect(page);
  await page
    .getByRole('row', { name: /\u8d22\u52a1\u56e2\u961f finance-team/ })
    .getByRole('button', { name: 'Manage access', exact: true })
    .click();
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Unknown policy');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'I reconciled the outcome', exact: true }),
  ).toBeVisible();
  await page.getByRole('tab', { name: 'Resource groups', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Unknown group');
  await page.getByLabel('Slug', { exact: true }).fill('unknown-group');
  await page.getByRole('button', { name: 'Create resource group', exact: true }).click();
  await expect.poll(() => state.writes.length).toBe(2);
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  await page.getByLabel('Workspace', { exact: true }).selectOption(third);
  await page.getByRole('button', { name: 'Open workspace', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(
    'Finish pending access changes and save or discard revealed credentials before switching workspaces.',
  );
  await expect(page.locator('.sidebar .workspace')).toContainText('Operations');
  await openManagementLink(page, third);
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    '\u8d22\u52a1\u56e2\u961f',
  );
  const recovery = page.getByRole('region', { name: 'Unfinished management operations' });
  await expect(recovery.getByRole('button')).toHaveCount(2);
  await recovery.getByRole('button', { name: /Review unknown groups/ }).click();
  await page.getByRole('button', { name: 'I reconciled the outcome', exact: true }).click();
  await expect(recovery.getByRole('button')).toHaveCount(1);
  await recovery.getByRole('button', { name: /Review unknown policies/ }).click();
  await page.getByRole('button', { name: 'I reconciled the outcome', exact: true }).click();
  await expect(recovery).toHaveCount(0);
  expect(state.writes).toEqual([policyPath, groupPath]);
});
