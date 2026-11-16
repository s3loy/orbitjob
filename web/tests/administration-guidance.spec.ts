import { signIn } from './support';
import { expect, test, type Page } from './support';
import { policyDocument, policyTemplates } from '../src/administration-policy';
const tenant = '00000000000000000000000001';
const finance = '00000000000000000000000002';
const operator = '00000000000000000000000021';
const readonly = '00000000000000000000000022';
const group = '00000000000000000000000031';
async function prepare(page: Page, { denied = false } = {}) {
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  const policies = [
    {
      id: operator,
      name: 'Workload operator',
      description: 'Operate workloads and monitoring.',
      platform: false,
      document: JSON.parse(policyDocument()),
    },
    {
      id: readonly,
      name: 'Diagnostics reader',
      description: 'View execution history and logs.',
      platform: false,
      document: JSON.parse(policyDocument(policyTemplates.readonly.actions)),
    },
  ];
  const keys: Record<string, unknown>[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const catalogPath = path.replace(
      /^\/api\/v1\/tenants\/[^/]+\/(policies|resource_groups)$/,
      '/api/v1/$1',
    );
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: {
          key_id: '00000000000000000000000099',
          kind: 'tenant',
          tenant_id: tenant,
          target_tenant_id: tenant,
          tenant_name: 'Operations',
          tenant_slug: 'operations',
          key_name: 'Admin fixture',
          workload_namespaces: [],
          group_id: '',
          boundary_policy_id: '',
          capabilities: [],
        },
      });
    if (request.method() !== 'GET') {
      const body = request.postDataJSON() as Record<string, unknown>;
      writes.push({ path, body });
      if (denied)
        return route.fulfill({
          status: 403,
          json: {
            error: { code: 'FORBIDDEN', message: 'This read-only credential cannot issue keys.' },
          },
        });
      if (path.endsWith('/api_keys')) {
        const row = {
          id: '00000000000000000000000041',
          name: body.name,
          key_prefix: 'fixture_key',
          created_at: '2026-10-24T00:00:00Z',
        };
        keys.push(row);
        return route.fulfill({
          status: 201,
          json: { ...row, key: 'otj_fixture_one_time_not_real' },
        });
      }
      return route.fulfill({
        status: 201,
        json: { id: `0000000000000000000000005${writes.length}`, name: body.name, description: '' },
      });
    }
    if (path === '/api/v1/workspaces')
      return denied
        ? route.fulfill({
            status: 403,
            json: { error: { code: 'FORBIDDEN', message: 'Workspace directory is not granted.' } },
          })
        : route.fulfill({
            json: {
              has_more: false,
              items: [
                { id: tenant, name: 'Operations', slug: 'operations', status: 'active' },
                { id: finance, name: 'Finance', slug: 'finance', status: 'active' },
              ],
            },
          });
    if (catalogPath === '/api/v1/policies') return route.fulfill({ json: { items: policies } });
    if (catalogPath === '/api/v1/resource_groups')
      return route.fulfill({
        json: { items: [{ id: group, name: 'Platform services', slug: 'platform' }] },
      });
    if (path.endsWith('/api_keys')) return route.fulfill({ json: { items: keys } });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/administration');
  await signIn(page, 'otj_admin_guidance_fixture_not_real');
  return writes;
}

test('named workspace and permission selections create exactly one clearly named key', async ({
  page,
}) => {
  const writes = await prepare(page);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    'Operations',
  );
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByLabel('Search workspace', { exact: true }).fill('finance');
  await page.getByRole('button', { name: 'Finance', exact: true }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Finance deploy service');
  await expect(page.getByLabel('Search policies')).toHaveCount(0);
  await expect(page.getByRole('checkbox', { name: /Diagnostics reader/ })).toBeVisible();
  await page.getByRole('checkbox', { name: /Workload operator/ }).check();
  await page.locator('summary').filter({ hasText: 'Advanced access restrictions' }).click();
  await page.getByRole('button', { name: 'Permission boundary', exact: true }).click();
  await page.getByLabel('Search permission boundary').fill('reader');
  await page.getByRole('button', { name: /Diagnostics reader/ }).click();
  await page.getByRole('button', { name: 'Resource group', exact: true }).click();
  await page.getByRole('button', { name: /Platform services platform/ }).click();
  await expect(
    page.getByText('Group keys work with group-aware Functions', { exact: false }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByLabel('Generated API key')).toHaveValue('otj_fixture_one_time_not_real');
  expect(writes).toEqual([
    {
      path: `/api/v1/tenants/${finance}/api_keys`,
      body: {
        name: 'Finance deploy service',
        policies: [operator],
        boundary_policy_id: readonly,
        resource_group_id: group,
      },
    },
  ]);
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    'Finance',
  );
  await expect(page.getByLabel('Generated API key')).toHaveValue('otj_fixture_one_time_not_real');
  await page.getByRole('button', { name: 'Discard key', exact: true }).click();
  await expect(page.getByRole('rowheader', { name: /Finance deploy service/ })).toBeVisible();
});

test('policy defaults are valid and advanced Deny documents stay lossless', async ({ page }) => {
  const writes = await prepare(page);
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Platform operator');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  expect(writes[0].body.document).toEqual(JSON.parse(policyDocument()));
  await page.getByRole('button', { name: 'Advanced JSON', exact: true }).click();
  const document = {
    version: '1',
    statement: [
      { effect: 'Deny', action: ['job:Trigger'], resource: ['orbitjob:self:*:job/restricted'] },
      { effect: 'Allow', action: ['job:Get'], resource: ['orbitjob:self:*:job/*'] },
    ],
  };
  await page.getByLabel('Policy document', { exact: true }).fill(JSON.stringify(document));
  await page.getByRole('button', { name: 'Validate and format JSON', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Guided builder', exact: true })).toBeDisabled();
  expect(JSON.parse(await page.getByLabel('Policy document').inputValue())).toEqual(document);
  await page.getByLabel('Name', { exact: true }).fill('Restricted operator');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect.poll(() => writes.length).toBe(2);
  expect(writes[1].body.document).toEqual(document);
  await expect(page.getByRole('region', { name: 'Recent changes', exact: true })).toContainText(
    'Restricted operator',
  );
  expect(
    await page
      .locator('.admin-activity-list')
      .evaluate((element) => getComputedStyle(element).maxHeight),
  ).not.toBe('none');
});

test('unsupported conditions and duplicate JSON stay visible and cannot produce a policy write', async ({
  page,
}) => {
  const writes = await prepare(page);
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByLabel('Name', { exact: true }).fill('Conditional policy draft');
  await page.getByRole('button', { name: 'Advanced JSON', exact: true }).click();
  const document = {
    version: '1',
    statement: [
      {
        effect: 'Allow',
        action: ['job:Get'],
        resource: ['orbitjob:self:*:job/*'],
        Condition: { network: 'private' },
      },
    ],
  };
  const field = page.getByLabel('Policy document', { exact: true });
  const raw = JSON.stringify(document).replace(
    '"network":"private"',
    '"network":"private","limit":9007199254740993',
  );
  await field.fill(raw);
  await page.getByRole('button', { name: 'Validate and format JSON', exact: true }).click();
  await expect(field).toHaveValue(raw);
  await expect(page.getByRole('status')).toContainText(
    'Unsupported policy field statement[0].Condition',
  );
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText(
    'Unsupported policy field statement[0].Condition',
  );
  expect(writes).toEqual([]);
  const duplicate =
    '{"version":"1","statement":[{"effect":"Deny","effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}';
  await field.fill(duplicate);
  await page.getByRole('button', { name: 'Validate and format JSON', exact: true }).click();
  await expect(field).toHaveValue(duplicate);
  await expect(page.getByRole('status')).toContainText('Duplicate policy field "effect"');
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Duplicate policy field "effect"');
  expect(writes).toEqual([]);
});

test('directory denial retains the current workspace and a denied write never retries', async ({
  page,
}) => {
  const writes = await prepare(page, { denied: true });
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    'Operations',
  );
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Workspace directory is not granted.');
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Read-only test');
  await page.getByRole('checkbox', { name: /Diagnostics reader/ }).check();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await expect(page.getByLabel('Generated API key')).toHaveCount(0);
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Management workspace', exact: true })).toHaveText(
    'Operations',
  );
  expect(writes).toHaveLength(1);
  expect(writes[0].path).toBe(`/api/v1/tenants/${tenant}/api_keys`);
});
