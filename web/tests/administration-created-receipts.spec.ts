import { signIn } from './support';
import { ulid } from 'ulid';
import { expect, test, type Page } from './support';
const keyID = ulid();
const ids = {
  tenants: ulid(),
  policies: ulid(),
  groups: ulid(),
};
const paths = {
  tenants: '/api/v1/tenants',
  policies: '/api/v1/policies',
  groups: '/api/v1/resource_groups',
};
async function live(page: Page, { delay = false, emptyList = false, unknown = false } = {}) {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const writes: string[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const url = new URL(route.request().url());
    const path = url.pathname;
    const catalogPath = path.replace(
      /^\/api\/v1\/tenants\/[^/]+\/(policies|resource_groups)$/,
      '/api/v1/$1',
    );
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: {
          kind: 'tenant',
          key_id: keyID,
          tenant_id: '00000000000000000000000001',
          target_tenant_id: '00000000000000000000000001',
          group_id: '',
          boundary_policy_id: '',
          capabilities: [],
        },
      });
    const kind = (Object.keys(paths) as (keyof typeof paths)[]).find(
      (kind) => paths[kind] === catalogPath,
    );
    if (kind && route.request().method() === 'POST') {
      writes.push(path);
      const input = route.request().postDataJSON() as { name: string; slug?: string };
      if (delay) await gate;
      try {
        return await route.fulfill({
          status: 201,
          json: unknown
            ? {}
            : {
                id: ids[kind],
                name: input.name,
                slug: input.slug ?? '',
                status: 'active',
                description: '',
              },
        });
      } catch {
        return;
      }
    }
    if (path === '/api/v1/workspaces')
      return emptyList
        ? route.fulfill({ json: { items: [], has_more: false } })
        : route.fulfill({
            status: 403,
            json: { error: { code: 'FORBIDDEN', message: 'Workspace directory is not granted.' } },
          });
    if (kind)
      return emptyList
        ? route.fulfill({ json: { items: [] } })
        : route.fulfill({
            status: 403,
            json: {
              error: { code: 'FORBIDDEN', message: 'List is not granted for this credential.' },
            },
          });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/administration');
  await signIn(page, 'otj_create_receipt_fixture_not_real');
  return { release, writes };
}
async function form(page: Page, kind: keyof typeof ids) {
  const name = kind === 'tenants' ? 'workspace' : kind === 'policies' ? 'policy' : 'resource group';
  await page
    .getByRole('tab', {
      name:
        kind === 'tenants' ? 'Workspaces' : kind === 'policies' ? 'Policies' : 'Resource groups',
      exact: true,
    })
    .click();
  await page.getByLabel('Name', { exact: true }).fill(`Created ${name}`);
  if (kind !== 'policies') await page.getByLabel('Slug', { exact: true }).fill(`created-${kind}`);
  return name;
}
async function jobs(page: Page) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
}
async function reviewChanges(page: Page) {
  const activity = page.getByRole('group', { name: 'Operation activity', exact: true });
  await expect(activity).toBeVisible();
  await expect(activity).not.toHaveAttribute('open', '');
  await activity.locator(':scope > summary').click();
  await activity.getByRole('link', { name: /View recent changes/ }).click();
}
test('create-only tenant policy and group keep returned IDs despite list 403 and navigation', async ({
  page,
}) => {
  const { writes } = await live(page);
  for (const kind of ['tenants', 'policies', 'groups'] as const) {
    const name = await form(page, kind);
    await expect(page.getByRole('alert')).toContainText('Permission required');
    await page.getByRole('button', { name: `Create ${name}`, exact: true }).click();
    await page
      .locator('.admin-activity-entry > summary')
      .filter({ hasText: `Created ${name}` })
      .click();
    const receipt = page.getByRole('region', { name: `Created ${name} receipt`, exact: true });
    await receipt.locator('.admin-record-identity > summary').click();
    await expect(receipt.getByLabel(`Created ${name} ID`, { exact: true })).toHaveText(ids[kind]);
    await expect(receipt).toContainText('Reading this record requires separate permission');
    await jobs(page);
    await reviewChanges(page);
    await page
      .getByRole('region', { name: `Created ${name} receipt`, exact: true })
      .locator('.admin-record-identity > summary')
      .click();
    await expect(page.getByLabel(`Created ${name} ID`, { exact: true })).toHaveText(ids[kind]);
  }
  expect(writes).toHaveLength(3);
  await page.evaluate(() => {
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
  });
  await page
    .getByRole('region', { name: 'Created workspace receipt', exact: true })
    .locator('.admin-record-identity > summary')
    .click();
  await page.getByRole('button', { name: 'Copy Created workspace ID', exact: true }).click();
  await expect(
    page.getByText('Select the ID above and copy manually.', { exact: true }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Account', exact: true }).last().click();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('status', { name: 'Recent changes', exact: true })).toHaveCount(0);
});
test('non-secret result completed off-page remains reviewable with an empty list', async ({
  page,
}) => {
  const { release, writes } = await live(page, { delay: true, emptyList: true });
  await form(page, 'tenants');
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  await jobs(page);
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  release();
  // A newly completed off-page request can open feedback on the current page.
  // Existing receipts still use reviewChanges to verify navigation collapses them.
  const activity = page.getByRole('group', { name: 'Operation activity', exact: true });
  await expect(activity).toBeVisible();
  if ((await activity.getAttribute('open')) === null)
    await activity.locator(':scope > summary').click();
  await activity.getByRole('link', { name: /View recent changes/ }).click();
  await page
    .getByRole('region', { name: 'Created workspace receipt', exact: true })
    .locator('.admin-record-identity > summary')
    .click();
  await expect(page.getByLabel('Created workspace ID', { exact: true })).toHaveText(ids.tenants);
  await page.getByRole('tab', { name: 'Workspaces', exact: true }).click();
  await expect(page.getByText('No workspaces yet', { exact: true })).toBeVisible();
  expect(writes).toEqual(['/api/v1/tenants']);
});
test('disconnect before delayed non-secret completion cannot restore old receipts', async ({
  page,
}) => {
  const { release, writes } = await live(page, { delay: true });
  await form(page, 'tenants');
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await expect.poll(() => writes.length).toBe(1);
  await page.getByRole('link', { name: 'Account', exact: true }).last().click();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  release();
  await expect(
    page.getByRole('button', { name: 'Explore sample data', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Explore sample data', exact: true }).click();
  await page.getByRole('link', { name: 'Administration', exact: true }).last().click();
  await expect(page.getByRole('heading', { name: 'Administration', exact: true })).toBeVisible();
  await expect(page.getByRole('status', { name: 'Recent changes', exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Created workspace ID', { exact: true })).toHaveCount(0);
});
test('unknown non-key creation still requires reconciliation and creates no receipt', async ({
  page,
}) => {
  const { writes } = await live(page, { unknown: true });
  await form(page, 'groups');
  await page.getByRole('button', { name: 'Create resource group', exact: true }).click();
  await expect(page.getByText('The outcome is unknown.', { exact: false })).toBeVisible();
  await jobs(page);
  await page.getByRole('link', { name: 'Administration', exact: true }).last().click();
  await page.getByRole('tab', { name: 'Resource groups', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Create resource group', exact: true }),
  ).toBeDisabled();
  await expect(page.getByRole('status', { name: 'Recent changes', exact: true })).toHaveCount(0);
  expect(writes).toHaveLength(1);
});

test('created resources lead to named key setup and retain the chosen workspace across tabs', async ({
  page,
}) => {
  const { writes } = await live(page, { emptyList: true });
  for (const kind of ['tenants', 'policies', 'groups'] as const) {
    const name = await form(page, kind);
    await page.getByRole('button', { name: `Create ${name}`, exact: true }).click();
    await page
      .locator('.admin-activity-entry > summary')
      .filter({ hasText: `Created ${name}` })
      .click();
    const receipt = page.getByRole('region', { name: `Created ${name} receipt`, exact: true });
    await receipt
      .getByRole('button', {
        name:
          kind === 'tenants'
            ? 'Create a key in this workspace'
            : kind === 'policies'
              ? 'Use this policy for a key'
              : 'Scope a key to this group',
        exact: true,
      })
      .click();
    await expect(page.getByRole('tab', { name: 'API keys', exact: true })).toHaveAttribute(
      'data-state',
      'active',
    );
    await expect(
      page.getByRole('button', { name: 'Management workspace', exact: true }),
    ).toHaveText('Created workspace');
    if (kind === 'policies')
      await expect(page.getByText('Selected: Created policy', { exact: true })).toBeVisible();
    if (kind === 'groups')
      await expect(page.getByRole('button', { name: 'Resource group', exact: true })).toHaveText(
        'Created resource group',
      );
  }
  expect(writes).toHaveLength(3);
});
