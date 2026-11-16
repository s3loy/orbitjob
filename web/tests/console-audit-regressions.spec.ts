import { ulid } from 'ulid';
import { expect, test, fixtureAccount, fixtureIdentity } from './support';
import type { Key, Policy } from '../src/api/administration';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

test('a created workspace becomes selectable without reloading the selected workload connection', async ({
  page,
}) => {
  const row = {
    id: ulid(),
    name: 'Created workspace',
    slug: 'created-workspace',
    status: 'active',
  };
  let created = false;
  let creationTarget = '';
  let sessionReads = 0;
  const selectedTargets: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/auth/session') {
      sessionReads++;
      return route.fulfill({
        json: {
          ...fixtureAccount,
          workspaces: [
            ...fixtureAccount.workspaces,
            ...(created ? [{ ...row, role: 'admin' }] : []),
          ],
        },
      });
    }
    if (url.pathname.startsWith('/api/v1/auth/')) return route.fallback();
    if (url.pathname === '/api/v1/identity') {
      const target = url.searchParams.get('tenant_id') || fixtureIdentity.tenant_id;
      selectedTargets.push(target);
      const resources = url.searchParams.getAll('resource');
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          tenant_id: target,
          target_tenant_id: target,
          capabilities: url.searchParams
            .getAll('action')
            .map((action, index) => ({ action, resource: resources[index], allowed: true })),
        },
      });
    }
    if (url.pathname === '/api/v1/tenants' && route.request().method() === 'POST') {
      creationTarget = url.searchParams.get('tenant_id') || '';
      created = true;
      return route.fulfill({ status: 201, json: row });
    }
    if (url.pathname === '/api/v1/instances/summary')
      return route.fulfill({ json: { total: 0, phases: {} } });
    return route.fulfill({
      json: {
        items:
          url.pathname === '/api/v1/workspaces'
            ? [{ ...fixtureAccount.workspaces[0], status: 'active' }, ...(created ? [row] : [])]
            : [],
        has_more: false,
      },
    });
  });
  await page.goto('/console/administration?panel=tenants');
  await page.getByLabel('Name', { exact: true }).fill(row.name);
  await page.getByLabel('Slug', { exact: true }).fill(row.slug);
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await expect(
    page.getByRole('rowheader', { name: `${row.name} ${row.slug}`, exact: true }),
  ).toBeVisible();
  expect(creationTarget).toBe(fixtureIdentity.tenant_id);
  await page.locator('.sidebar').getByRole('link', { name: 'Account', exact: true }).click();
  await expect(
    page.getByLabel('Workspace', { exact: true }).locator(`option[value="${row.id}"]`),
  ).toHaveText(`${row.name} (admin)`);
  expect(sessionReads).toBeGreaterThan(1);
  await page.getByLabel('Workspace', { exact: true }).selectOption(row.id);
  await page.getByRole('button', { name: 'Open workspace', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  expect(selectedTargets).toContain(row.id);
});

test('unconfirmed source controls reconnect only for the same personal account', async ({
  page,
}) => {
  const alice = { id: ulid(), username: 'alice', display_name: 'Alice', installation_admin: false };
  const bob = { id: ulid(), username: 'bob', display_name: 'Bob', installation_admin: false };
  let user = alice;
  let signedIn = true;
  let writes = 0;
  const control = {
    source_mode: 'kubernetes',
    source_uid: 'audit-source',
    version: 0,
    schedule_paused: false,
    execution_paused: false,
    tombstoned: false,
    can_manage: true,
  };
  const snapshot = () =>
    signedIn
      ? { ...fixtureAccount, user, csrf_token: `csrf-${user.id}` }
      : { authenticated: false, initialized: true, workspaces: [] };
  await page.route('**/api/v1/**', (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/auth/logout') {
      signedIn = false;
      return route.fulfill({ status: 204 });
    }
    if (path === '/api/v1/auth/login') {
      signedIn = true;
      return route.fulfill({ json: snapshot() });
    }
    if (path === '/api/v1/auth/session') return route.fulfill({ json: snapshot() });
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          user_id: user.id,
          username: user.username,
          installation_admin: false,
        },
      });
    if (path.endsWith('/execution-controls')) {
      if (route.request().method() === 'GET') return route.fulfill({ json: control });
      writes++;
      return route.fulfill({
        status: 503,
        json: { error: { code: 'unavailable', message: 'Unknown write outcome' } },
      });
    }
    if (path === '/api/v1/jobs/42')
      return route.fulfill({
        json: {
          id: 42,
          tenant_id: fixtureIdentity.tenant_id,
          source_uid: control.source_uid,
          generation: 1,
          name: 'audit-job',
          namespace: 'tasks',
          actor: 'fixture',
          schedule: '',
          schedule_summary: 'Manual only',
          source_kind: 'ScheduledJob',
          source_mode: 'kubernetes',
          suspend: false,
          concurrency_policy: 'Allow',
          misfire_policy: 'Skip',
          created_at: '2026-11-17T00:00:00Z',
          spec_hash: 'hash',
          timeout_seconds: 60,
          retry_max_attempts: 1,
          job_template: { image: 'busybox:1.37.0', backoff_limit: 0 },
        },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await page.route('**/openapi.json', (route) => route.fulfill({ json: { paths: {} } }));
  const panel = page.getByRole('region', { name: 'Execution controls', exact: true });
  await page.goto('/console/jobs/42?tab=definition');
  await panel.getByRole('switch', { name: 'Pause tasks', exact: true }).click();
  await panel.getByRole('button', { name: 'Save controls', exact: true }).click();
  await expect(panel.getByText('Save result not confirmed', { exact: true })).toBeVisible();
  await page.reload();
  await expect(panel.getByText('Save result not confirmed', { exact: true })).toBeVisible();
  await expect(panel.getByRole('switch', { name: 'Pause tasks', exact: true })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  async function signInAs(next: typeof user) {
    await page.locator('.sidebar').getByRole('link', { name: 'Account', exact: true }).click();
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    user = next;
    await page.getByLabel('Username', { exact: true }).fill(user.username);
    await page.getByLabel('Password', { exact: true }).fill('fixture-private-password');
    const login = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === '/api/v1/auth/login' &&
        response.request().method() === 'POST',
    );
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    expect((await (await login).json()).user.id).toBe(next.id);
    await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
    await page.locator('.sidebar').getByRole('link', { name: 'Account', exact: true }).click();
    await expect(page.getByRole('heading', { name: user.display_name, exact: true })).toBeVisible();
    await page.goto('/console/jobs/42?tab=definition');
    await expect(panel.getByRole('switch', { name: 'Pause tasks', exact: true })).toBeVisible();
  }
  await signInAs(bob);
  await expect(panel.getByText('Save result not confirmed', { exact: true })).toHaveCount(0);
  await expect(panel.getByRole('switch', { name: 'Pause tasks', exact: true })).toHaveAttribute(
    'aria-checked',
    'false',
  );
  await signInAs(alice);
  await expect(panel.getByText('Save result not confirmed', { exact: true })).toBeVisible();
  await expect(panel.getByRole('switch', { name: 'Pause tasks', exact: true })).toHaveAttribute(
    'aria-checked',
    'true',
  );
  expect(writes).toBe(1);
});

test('a human manages workspace B keys while workload requests remain in workspace A', async ({
  page,
}) => {
  const a = fixtureIdentity.tenant_id,
    b = ulid();
  const workspaces = [
    { ...fixtureAccount.workspaces[0], name: 'Workspace A', status: 'active' },
    { id: b, name: 'Workspace B', slug: 'workspace-b', role: 'admin', status: 'active' },
  ];
  const account = {
    ...fixtureAccount,
    user: { ...fixtureAccount.user, installation_admin: false },
    workspaces,
  };
  const key = (name: string): Key => ({
    id: ulid(),
    name,
    key_prefix: 'fixture-prefix',
    created_at: '2026-11-17T00:00:00Z',
  });
  const keys: Record<string, Key[]> = { [a]: [key('A service key')], [b]: [] };
  const policy: Policy = {
    id: ulid(),
    name: 'Fixture policy',
    description: 'Fixture access',
    platform: false,
    document: {
      version: '1',
      statement: [{ effect: 'Allow', action: ['job:Get'], resource: ['orbitjob:self:*:job/*'] }],
    },
  };
  const mutations: { method: string; target: string }[] = [];
  const workloadTargets: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      path = url.pathname;
    if (path === '/api/v1/auth/session') return route.fulfill({ json: account });
    if (path.startsWith('/api/v1/auth/')) return route.fallback();
    if (path === '/api/v1/identity') {
      const target = url.searchParams.get('tenant_id') || a,
        resources = url.searchParams.getAll('resource');
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          installation_admin: false,
          tenant_id: target,
          target_tenant_id: target,
          tenant_name: target === a ? 'Workspace A' : 'Workspace B',
          capabilities: url.searchParams
            .getAll('action')
            .map((action, index) => ({ action, resource: resources[index], allowed: true })),
        },
      });
    }
    if (path === '/api/v1/workspaces')
      return route.fulfill({ json: { items: workspaces, has_more: false } });
    const tenant = path.match(/^\/api\/v1\/tenants\/([^/]+)\/(api_keys|policies|resource_groups)$/);
    if (tenant) {
      const [, id, kind] = tenant;
      if (kind === 'api_keys') {
        if (req.method() === 'POST') {
          expect(id).toBe(b);
          const item = key(req.postDataJSON().name);
          keys[id].push(item);
          return route.fulfill({ status: 201, json: { ...item, key: 'fixture-only-secret' } });
        }
        return route.fulfill({ json: { items: keys[id] } });
      }
      return route.fulfill({
        json: {
          items: kind === 'policies' ? [policy] : [],
        },
      });
    }
    const management = path.match(/^\/api\/v1\/api_keys\/([^/]+)(\/revoke)?$/);
    if (management) {
      const target = url.searchParams.get('tenant_id') || '';
      expect(target).toBe(b);
      expect(req.headers()['x-csrf-token']).toBe(account.csrf_token);
      expect(req.headers()).not.toHaveProperty('authorization');
      mutations.push({ method: req.method(), target });
      const item = keys[b].find((value) => value.id === management[1])!;
      if (req.method() === 'PATCH') {
        item.name = req.postDataJSON().name;
        return route.fulfill({ json: { id: item.id, name: item.name } });
      }
      item.revoked_at = '2026-11-17T00:01:00Z';
      return route.fulfill({ json: { revoked: true } });
    }
    if (path === '/api/v1/jobs') workloadTargets.push(url.searchParams.get('tenant_id') || '');
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/administration?panel=keys');
  await expect(page.getByRole('rowheader', { name: /A service key/ })).toBeVisible();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page
    .getByRole('group', { name: 'Workspace options', exact: true })
    .getByRole('button', { name: /Workspace B/ })
    .click();
  await page.getByLabel('Key name', { exact: true }).fill('B service key');
  await page.getByRole('checkbox', { name: /Fixture policy/ }).check();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByRole('region', { name: 'One-time API key', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Discard key', exact: true }).click();
  const row = page.getByRole('row').filter({ hasText: 'B service key' });
  await expect(row.getByRole('button', { name: 'Rename', exact: true })).toBeEnabled();
  await row.getByRole('button', { name: 'Rename', exact: true }).click();
  await page.getByLabel('New key name', { exact: true }).fill('Renamed B key');
  await page.getByRole('button', { name: 'Save name', exact: true }).click();
  const renamed = page.getByRole('row').filter({ hasText: 'Renamed B key' });
  await expect(renamed).toBeVisible();
  await renamed.getByRole('button', { name: 'Revoke', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
  await expect(renamed.getByRole('cell', { name: 'Revoked', exact: true })).toBeVisible();
  await expect(page.getByLabel('Current workspace', { exact: true })).toHaveText('Workspace A');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  expect(workloadTargets).toEqual([a]);
  expect(mutations).toEqual([
    { method: 'PATCH', target: b },
    { method: 'POST', target: b },
  ]);
});
