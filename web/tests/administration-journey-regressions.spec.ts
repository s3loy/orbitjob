import { signIn } from './support';
import { expect, test, type Page } from './support';
import { policyDocument } from '../src/administration-policy';
import type { Route } from '@playwright/test';
const tenantA = '00000000000000000000000001';
const tenantB = '00000000000000000000000002';
const policyA = '00000000000000000000000021';
type AuthenticationSource = 'keys' | 'policies' | 'resource_groups' | 'identity' | 'workspaces';
type Options = {
  readOnly?: boolean;
  slowDirectories?: boolean;
  rejectPolicy?: boolean;
  dialogDenied?: boolean;
  denyForeign?: boolean;
  authenticationFailure?: AuthenticationSource;
};
function gate() {
  let release!: () => void;
  const promise = new Promise<void>((resolve) => {
    release = resolve;
  });
  return { promise, release };
}
async function prepare(page: Page, options: Options = {}) {
  const directory = gate();
  const mutation = gate();
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const keyRequests: { method: string; path: string; tenant: string | null }[] = [];
  const reads = { policies: 0, resource_groups: 0 };
  let serial = 50;
  let authenticationRejected = false;
  const policies = new Map<string, Record<string, unknown>[]>([
    [
      tenantA,
      [
        {
          id: policyA,
          name: 'Operations reader',
          description: '',
          platform: false,
          document: JSON.parse(policyDocument()),
        },
      ],
    ],
    [tenantB, []],
  ]);
  const groups = new Map<string, Record<string, unknown>[]>([
    [tenantA, []],
    [tenantB, []],
  ]);
  const keys = new Map<string, Record<string, unknown>[]>([
    [
      tenantA,
      [
        {
          id: '00000000000000000000000041',
          name: 'Existing service',
          key_prefix: 'existing',
          created_at: '2026-10-25T00:00:00Z',
        },
      ],
    ],
    [tenantB, []],
  ]);
  const tenants = [{ id: tenantA, name: 'Operations', slug: 'operations', status: 'active' }];
  const denied = (route: Route, message: string) =>
    route.fulfill({ status: 403, json: { error: { code: 'FORBIDDEN', message } } });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const authenticationPath = {
      keys: `/api/v1/tenants/${tenantA}/api_keys`,
      policies: `/api/v1/tenants/${tenantA}/policies`,
      resource_groups: `/api/v1/tenants/${tenantA}/resource_groups`,
      identity: '/api/v1/identity',
      workspaces: '/api/v1/workspaces',
    };
    if (
      authenticationRejected &&
      options.authenticationFailure &&
      path === authenticationPath[options.authenticationFailure]
    )
      return route.fulfill({
        status: 401,
        json: { error: { code: 'UNAUTHORIZED', message: 'This API key has been revoked.' } },
      });
    if (path === '/api/v1/identity') {
      const actions = url.searchParams.getAll('action');
      const resources = url.searchParams.getAll('resource');
      return route.fulfill({
        json: {
          key_id: options.authenticationFailure
            ? '00000000000000000000000041'
            : '00000000000000000000000099',
          kind: 'tenant',
          tenant_id: tenantA,
          target_tenant_id: tenantA,
          tenant_name: 'Operations',
          tenant_slug: 'operations',
          workload_namespaces: ['operations'],
          group_id: '',
          boundary_policy_id: '',
          capabilities: actions.map((action, index) => ({
            action,
            resource: resources[index],
            allowed: !options.readOnly,
          })),
        },
      });
    }
    const scoped = /^\/api\/v1\/tenants\/([^/]+)\/(policies|resource_groups|api_keys)$/.exec(path);
    if (request.method() === 'GET') {
      if (path === '/api/v1/workspaces')
        return route.fulfill({ json: { items: tenants, has_more: false } });
      if (options.readOnly) return denied(route, 'Management directory access is not granted.');
      if (path === '/api/v1/tenants') return route.fulfill({ json: { items: tenants } });
      if (scoped) {
        const [, target, collection] = scoped;
        if (options.denyForeign && target === tenantB && collection !== 'api_keys')
          return denied(route, 'This target directory is not granted.');
        if (collection === 'policies' || collection === 'resource_groups') {
          reads[collection]++;
          if (options.slowDirectories && reads[collection] === 1) {
            await directory.promise;
            return route.fulfill({
              status: 503,
              json: {
                error: { code: 'UNAVAILABLE', message: 'Directory temporarily unavailable.' },
              },
            });
          }
        }
        const rows =
          collection === 'policies' ? policies : collection === 'resource_groups' ? groups : keys;
        return route.fulfill({ json: { items: rows.get(target) ?? [] } });
      }
      if (path === '/api/v1/policies' || path === '/api/v1/resource_groups')
        return route.fulfill({
          status: 400,
          json: { error: { code: 'VALIDATION', message: 'Use an explicit management workspace.' } },
        });
      return route.fulfill({ json: { items: [] } });
    }
    const body = (request.postData() ? request.postDataJSON() : {}) as Record<string, unknown>;
    writes.push({ path, body });
    if (path.startsWith('/api/v1/api_keys/'))
      keyRequests.push({
        method: request.method(),
        path,
        tenant: url.searchParams.get('tenant_id'),
      });
    if (
      options.authenticationFailure &&
      path === '/api/v1/api_keys/00000000000000000000000041/revoke'
    ) {
      authenticationRejected = true;
      return route.fulfill({ json: { revoked: true } });
    }
    if (options.readOnly) return denied(route, 'Management writes are not permitted.');
    if (options.dialogDenied && path.startsWith('/api/v1/api_keys/'))
      return denied(route, 'This key update is not permitted.');
    if (path === '/api/v1/tenants') {
      const row = {
        id: tenantB,
        name: String(body.name),
        slug: String(body.slug),
        status: 'active',
      };
      tenants.push(row);
      return route.fulfill({ status: 201, json: row });
    }
    if (scoped) {
      const [, target, collection] = scoped;
      if (collection === 'policies' && options.rejectPolicy) {
        await mutation.promise;
        return denied(route, 'Policy creation was definitely rejected.');
      }
      const id = String(++serial).padStart(26, '0');
      if (collection === 'api_keys') {
        const targetPolicies = policies.get(target) ?? [];
        const targetGroups = groups.get(target) ?? [];
        if (
          !(body.policies as string[]).every((value) =>
            targetPolicies.some((row) => row.id === value),
          ) ||
          (body.resource_group_id && !targetGroups.some((row) => row.id === body.resource_group_id))
        )
          return route.fulfill({
            status: 400,
            json: {
              error: {
                code: 'VALIDATION',
                message: 'A selected grant belongs to another workspace.',
              },
            },
          });
        const row = {
          id,
          name: body.name,
          key_prefix: 'fixture',
          created_at: '2026-10-25T00:00:00Z',
        };
        keys.set(target, [...(keys.get(target) ?? []), row]);
        return route.fulfill({
          status: 201,
          json: { ...row, key: 'otj_journey_fixture_one_time_not_real' },
        });
      }
      const row =
        collection === 'policies'
          ? { id, ...body, description: '', platform: false }
          : { id, name: body.name, slug: body.slug };
      const rows = collection === 'policies' ? policies : groups;
      rows.set(target, [...(rows.get(target) ?? []), row]);
      return route.fulfill({ status: 201, json: row });
    }
    return route.fulfill({
      status: 400,
      json: { error: { code: 'VALIDATION', message: 'Unexpected management write.' } },
    });
  });
  await page.goto('/console/administration');
  await signIn(page, 'otj_journey_fixture_not_real');
  return {
    writes,
    keyRequests,
    reads,
    releaseDirectories: directory.release,
    rejectPolicy: mutation.release,
    rejectAuthentication: () => {
      authenticationRejected = true;
    },
  };
}
async function follow(page: Page, name: string, kind: string, action: string) {
  const receipt = page
    .getByRole('region', { name: `Created ${kind} receipt`, exact: true })
    .filter({ has: page.getByRole('button', { name: action, exact: true }) });
  await page.locator('.admin-activity-entry > summary').filter({ hasText: name }).click();
  await receipt.getByRole('button', { name: action, exact: true }).click();
}

test('Default tenant administrator completes new workspace policy group and key setup in one scope', async ({
  page,
}) => {
  const { writes } = await prepare(page);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Old workspace draft');
  await page.getByRole('checkbox', { name: /Operations reader/ }).check();
  await page.getByRole('tab', { name: 'Workspaces', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Product team');
  await page.getByLabel('Slug', { exact: true }).fill('product');
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await follow(page, 'Product team', 'workspace', 'Create a key in this workspace');
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    'Product team',
  );
  await expect(page.getByLabel('Key name', { exact: true })).toHaveValue('');
  await expect(page.getByRole('checkbox', { name: /Operations reader/ })).toHaveCount(0);
  await page.getByLabel('Key name', { exact: true }).fill('Product deploy service');
  await page.getByRole('button', { name: 'Create a policy first', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Product operator');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await follow(page, 'Product operator', 'policy', 'Use this policy for a key');
  await expect(page.getByLabel('Key name', { exact: true })).toHaveValue('Product deploy service');
  await expect(page.getByRole('checkbox', { name: /Product operator/ })).toBeChecked();
  await page.getByRole('tab', { name: 'Resource groups', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Product services');
  await page.getByLabel('Slug', { exact: true }).fill('product-services');
  await page.getByRole('button', { name: 'Create resource group', exact: true }).click();
  await follow(page, 'Product services', 'resource group', 'Scope a key to this group');
  await expect(page.getByLabel('Key name', { exact: true })).toHaveValue('Product deploy service');
  await expect(page.getByRole('checkbox', { name: /Product operator/ })).toBeChecked();
  await expect(page.getByRole('button', { name: 'Resource group', exact: true })).toHaveText(
    'Product services',
  );
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByLabel('Generated API key')).toHaveValue(
    'otj_journey_fixture_one_time_not_real',
  );
  expect(writes.map((write) => write.path)).toEqual([
    '/api/v1/tenants',
    `/api/v1/tenants/${tenantB}/policies`,
    `/api/v1/tenants/${tenantB}/resource_groups`,
    `/api/v1/tenants/${tenantB}/api_keys`,
  ]);
  expect(writes[3].body).toMatchObject({
    name: 'Product deploy service',
    policies: ['00000000000000000000000051'],
    resource_group_id: '00000000000000000000000052',
  });
  await expect(page.getByRole('region', { name: 'Recent changes', exact: true })).not.toContainText(
    'otj_journey_fixture_one_time_not_real',
  );
  await page.getByRole('button', { name: 'Discard key', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Rename', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Revoke', exact: true })).toBeDisabled();
  await expect(
    page.getByText('Select a workspace where your account has API key management access.', {
      exact: true,
    }),
  ).toBeVisible();
});

test('pending directories are not empty and Refresh retries both failed permission directories', async ({
  page,
}) => {
  const { reads, writes, releaseDirectories } = await prepare(page, { slowDirectories: true });
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await expect(page.getByText('Loading policy directory…', { exact: true })).toBeVisible();
  await expect(page.getByText('No policies available.', { exact: false })).toHaveCount(0);
  await expect.poll(() => reads).toEqual({ policies: 1, resource_groups: 1 });
  releaseDirectories();
  await expect(
    page.getByRole('button', { name: 'Retry policy directory', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('checkbox', { name: /Operations reader/ })).toBeVisible();
  await expect.poll(() => reads).toEqual({ policies: 2, resource_groups: 2 });
  expect(writes).toEqual([]);
});

test('definite non-key rejection received off-tab remains rejected and does not lock the form', async ({
  page,
}) => {
  const { writes, rejectPolicy } = await prepare(page, { rejectPolicy: true });
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Rejected draft');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  rejectPolicy();
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Policy creation was definitely rejected.');
  await expect(page.getByRole('button', { name: 'Create policy', exact: true })).toBeEnabled();
  await expect(page.getByText('The outcome is unknown.', { exact: false })).toHaveCount(0);
  expect(writes).toHaveLength(1);
});

test('key rename and revoke rejection is actionable inside the active dialog', async ({ page }) => {
  const { writes, keyRequests } = await prepare(page, { dialogDenied: true });
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('button', { name: 'Rename', exact: true }).click();
  await page.getByLabel('New key name').fill('Updated service');
  await page.getByRole('button', { name: 'Save name', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('alert')).toContainText('This key update is not permitted.');
  await expect(dialog.getByLabel('New key name')).toHaveValue('Updated service');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Revoke', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('This key update is not permitted.');
  expect(writes).toHaveLength(2);
  expect(keyRequests).toEqual([
    { method: 'PATCH', path: '/api/v1/api_keys/00000000000000000000000041', tenant: tenantA },
    {
      method: 'POST',
      path: '/api/v1/api_keys/00000000000000000000000041/revoke',
      tenant: tenantA,
    },
  ]);
});

test('exact denied management capabilities replace creation forms without inferring denial from list grants', async ({
  page,
}) => {
  const { writes } = await prepare(page, { readOnly: true });
  for (const { name, resource, permission } of [
    { name: 'Workspaces', resource: 'workspace', permission: 'tenant:Create' },
    { name: 'API keys', resource: 'API key', permission: 'apikey:Create' },
    { name: 'Policies', resource: 'policy', permission: 'policy:Create' },
    { name: 'Resource groups', resource: 'resource group', permission: 'group:Create' },
  ]) {
    await page.getByRole('tab', { name, exact: true }).click();
    await expect(
      page.getByRole('region', { name: 'Management access required', exact: true }),
    ).toBeVisible();
    await expect(page.getByRole('button', { name: `Create ${resource}`, exact: true })).toHaveCount(
      0,
    );
    await page.getByText('Required permission', { exact: true }).click();
    await expect(page.getByText(permission, { exact: true })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Back to workloads', exact: true })).toBeVisible();
  }
  expect(writes).toEqual([]);
});

test('denied target catalogs retain the chosen target and never borrow the current workspace grants', async ({
  page,
}) => {
  const { writes } = await prepare(page, { denyForeign: true });
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Operations draft');
  await page.getByRole('checkbox', { name: /Operations reader/ }).check();
  await page.locator('summary').filter({ hasText: 'Use an exact workspace ID' }).click();
  await page.getByLabel('Workspace ID', { exact: true }).fill(tenantB);
  await page.getByRole('button', { name: 'Select workspace', exact: true }).click();
  await expect(page.getByLabel('Workspace ID', { exact: true })).toHaveValue(tenantB);
  await expect(
    page.getByRole('button', { name: 'Management workspace', exact: true }),
  ).not.toHaveText('Operations');
  await expect(page.getByRole('checkbox', { name: /Operations reader/ })).toHaveCount(0);
  await expect(page.getByLabel('Key name', { exact: true })).toHaveValue('');
  await expect(
    page.locator('summary').filter({ hasText: 'Use a policy ID from your administrator' }),
  ).toBeVisible();
  await expect(page.getByText('Selected: Operations reader', { exact: true })).toHaveCount(0);
  expect(writes).toEqual([]);
});

for (const source of [
  'keys',
  'policies',
  'resource_groups',
  'identity',
  'workspaces',
] satisfies AuthenticationSource[]) {
  test(`session rejection from ${source} after key revocation hides cached management actions`, async ({
    page,
  }) => {
    const { writes } = await prepare(page, { authenticationFailure: source });
    await page.getByRole('tab', { name: 'API keys', exact: true }).click();
    await expect(page.getByRole('row', { name: /Existing service/ })).toContainText('Active');
    await expect(page.getByRole('heading', { name: 'Create API key', exact: true })).toBeVisible();
    await expect(page.getByRole('checkbox', { name: /Operations reader/ })).toBeVisible();
    await page.getByRole('button', { name: 'Revoke', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
    if (source === 'workspaces') {
      await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
    }
    await expect(page.getByRole('alert')).toHaveCount(1);
    await expect(page.getByRole('alert')).toContainText('Your session has ended.');
    await expect(page.getByRole('row', { name: /Existing service/ })).toHaveCount(0);
    await expect(page.getByRole('heading', { name: 'Create API key', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Rename', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Revoke', exact: true })).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Create API key', exact: true })).toHaveCount(0);
    await expect(
      page.getByRole('button', { name: 'Management workspace', exact: true }),
    ).toHaveCount(0);
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel('Password', { exact: true })).toHaveValue('');
    expect(writes).toHaveLength(1);
  });
}

test('401 recovery clears revealed keys and confirmed receipts with the rejected session', async ({
  page,
}) => {
  const { writes, rejectAuthentication } = await prepare(page, { authenticationFailure: 'keys' });
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Confirmed deployment policy');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await follow(page, 'Confirmed deployment policy', 'policy', 'Use this policy for a key');
  await page.getByLabel('Key name', { exact: true }).fill('One-time deployment key');
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByLabel('Generated API key', { exact: true })).toHaveValue(
    'otj_journey_fixture_one_time_not_real',
  );
  rejectAuthentication();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveCount(1);
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel('Generated API key', { exact: true })).toHaveCount(0);
  await expect(
    page.getByRole('region', { name: 'Created policy receipt', exact: true }),
  ).toHaveCount(0);
  expect(writes).toHaveLength(2);
});
