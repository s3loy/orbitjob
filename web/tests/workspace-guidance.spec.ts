import { signIn } from './support';
import { expect, test, fixtureIdentity, type Page } from './support';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });
const tenant = fixtureIdentity.tenant_id;
async function connect(page: Page, namespaces?: string[], path = '/console/overview') {
  const reads: string[] = [];
  const writes: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === 'GET') reads.push(path);
    else writes.push(path);
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          tenant_name: 'Platform operations',
          tenant_slug: 'platform-ops',
          key_name: 'Daily operator',
          ...(namespaces === undefined ? {} : { workload_namespaces: namespaces }),
        },
      });
    return route.fulfill({
      json: path.endsWith('/summary') ? { total: 0, phases: {} } : { items: [] },
    });
  });
  await page.goto(path);
  await signIn(page, 'otj_workspace_guidance_not_real');
  return { reads, writes };
}
test('named workspace context and bounded keyboard actions require no administration reads', async ({
  page,
}) => {
  const { reads, writes } = await connect(page, ['team-runtime', 'batch-runtime']);
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
  const workspace = page.locator('.sidebar .workspace');
  await expect(workspace).toHaveText('Platform operations');
  await expect(
    workspace.getByRole('link', { name: 'Review Platform operations workspace', exact: true }),
  ).toHaveText('Platform operations');
  await expect(page.getByRole('region', { name: 'Execution outcomes' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Observation scope' })).toHaveCount(0);
  await expect(page.getByText('Platform operations', { exact: true })).toHaveCount(1);
  await expect(page.locator('.sidebar')).not.toContainText(tenant);
  await expect(page.getByText(tenant, { exact: true })).toBeHidden();
  const primary = page.locator('.workspace-action-primary');
  expect(await primary.evaluate((element) => getComputedStyle(element).borderTopWidth)).toBe('1px');
  await primary.focus();
  await page.keyboard.press('Tab');
  await expect(page.locator('.workspace-action').nth(1)).toBeFocused();
  expect(
    await page
      .locator('.workspace-action')
      .nth(1)
      .evaluate((element) => getComputedStyle(element).outlineWidth),
  ).toBe('2px');
  await page.screenshot({ path: 'test-results/workspace-overview-desktop.png', fullPage: true });
  await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
  await page
    .getByRole('combobox', { name: 'Namespace', exact: true })
    .selectOption('batch-runtime');
  await expect(page.getByRole('combobox', { name: 'Namespace', exact: true })).toHaveValue(
    'batch-runtime',
  );
  expect(reads.some((path) => /\/tenants|\/policies|\/resource_groups|\/api_keys/.test(path))).toBe(
    false,
  );
  expect(writes).toEqual([]);
});
test('workspace IDs stay in explicit details with working copy and account names appear once', async ({
  page,
}) => {
  const { reads, writes } = await connect(page, ['team-runtime']);
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  const identity = page.getByRole('region', { name: 'Account access', exact: true });
  const account = page.getByRole('region', { name: 'Account and workspace', exact: true });
  await expect(account.getByRole('heading', { name: 'Fixture User', exact: true })).toHaveCount(1);
  await expect(identity).not.toContainText('Fixture User');
  await expect(identity.getByText('platform-ops', { exact: true })).toBeHidden();
  await expect(identity.getByText('team-runtime', { exact: true })).toBeHidden();
  await expect(identity.getByText(tenant, { exact: true })).toBeHidden();
  await identity.locator('summary').filter({ hasText: 'Workspace access and environment' }).click();
  await expect(identity.getByText('platform-ops', { exact: true })).toBeVisible();
  await expect(identity.getByText('team-runtime', { exact: true })).toBeVisible();
  await expect(identity.getByText(tenant, { exact: true })).toBeVisible();
  await page.evaluate(() =>
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => true }),
  );
  await identity.getByRole('button', { name: 'Copy workspace ID', exact: true }).click();
  await expect(identity.getByText('Workspace ID copied.', { exact: true })).toBeVisible();
  expect(reads.some((path) => /\/tenants|\/policies|\/resource_groups|\/api_keys/.test(path))).toBe(
    false,
  );
  expect(writes).toEqual([]);
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    'otj_workspace_guidance_not_real',
  );
});
test('a reported empty mapping gives an honest environment setup state', async ({ page }) => {
  const { writes } = await connect(page, [], '/console/manifests/job');
  await expect(page.getByRole('textbox', { name: 'Namespace', exact: true })).toBeDisabled();
  await expect(
    page.getByText('No execution namespace is connected', { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Review connection', exact: true })).toBeVisible();
  expect(writes).toEqual([]);
});
test('legacy namespace metadata remains unknown rather than a missing environment claim', async ({
  page,
}) => {
  const { writes } = await connect(page, undefined, '/console/manifests/job');
  const field = page.getByRole('textbox', { name: 'Namespace', exact: true });
  await expect(field).toBeEnabled();
  await expect(
    page.getByText('The workspace has not provided its namespace list.', { exact: false }),
  ).toBeVisible();
  await expect(page.getByText('No execution namespace is connected', { exact: true })).toHaveCount(
    0,
  );
  await field.fill('administrator-provided');
  await expect(field).toHaveValue('administrator-provided');
  expect(writes).toEqual([]);
});

test('Apply without disclosed namespace metadata keeps manual entry and cluster validation', async ({
  page,
}) => {
  const checks: { path: string; dry_run: boolean }[] = [];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === '/api/v1/identity')
      return route.fulfill({
        json: {
          ...fixtureIdentity,
        },
      });
    if (url.pathname === '/api/v1/tenants')
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'tenant:List is not granted.' } },
      });
    if (url.pathname.startsWith('/api/v1/definitions/jobs/') && request.method() === 'PUT') {
      const input = request.postDataJSON() as {
        dry_run: boolean;
        request_id: string;
        spec_json: string;
      };
      checks.push({ path: url.pathname, dry_run: input.dry_run });
      return route.fulfill({
        json: {
          kind: 'jobs',
          namespace: 'provided-runtime',
          name: 'manual-namespace-job',
          uid: 'manual-namespace-job-source',
          resource_version: '1',
          generation: 1,
          spec_json: input.spec_json,
          manifest_digest: 'digest',
          last_request_id: '',
          last_manifest_digest: '',
          request_id: input.request_id,
          state: 'validated',
        },
      });
    }
    return route.fulfill({
      json: url.pathname.endsWith('/summary') ? { total: 0, phases: {} } : { items: [] },
    });
  });
  await page.goto('/console/manifests/job');
  await signIn(page, 'manual-namespace-password-fixture');
  const namespace = page.getByRole('textbox', { name: 'Namespace', exact: true });
  await expect(namespace).toBeEnabled();
  await expect(page.getByText('No execution namespace is connected', { exact: true })).toHaveCount(
    0,
  );
  await namespace.fill('provided-runtime');
  await page.getByLabel('Name', { exact: true }).fill('manual-namespace-job');
  await page.getByLabel('Container image', { exact: true }).fill('busybox:1.37.0');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
  expect(checks).toEqual([
    { path: '/api/v1/definitions/jobs/provided-runtime/manual-namespace-job', dry_run: true },
  ]);
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  await expect(page.getByText('No execution namespace is connected', { exact: true })).toHaveCount(
    0,
  );
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Overview', exact: true })
    .click();
  await expect(
    page.getByRole('region', { name: 'Execution setup needed', exact: true }),
  ).toHaveCount(0);
});
test('an imported namespace outside the mapping stays visible for correction', async ({ page }) => {
  const { writes } = await connect(page, ['team-runtime'], '/console/manifests/job');
  await page.getByLabel('Name', { exact: true }).fill('mapped-job');
  await page.getByLabel('Container image', { exact: true }).fill('busybox:1.37.0');
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('team-runtime');
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  const yaml = await page.getByLabel('Generated YAML', { exact: true }).inputValue();
  expect(yaml).toMatch(/namespace:\s*"?team-runtime"?/);
  await page
    .getByLabel('Generated YAML', { exact: true })
    .fill(yaml.replace(/namespace:\s*"?team-runtime"?/, 'namespace: "another-workspace"'));
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  const field = page.getByRole('combobox', { name: 'Namespace', exact: true });
  await expect(field).toHaveValue('another-workspace');
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await expect(
    page.getByText('Choose a namespace in this workspace', { exact: true }),
  ).toBeVisible();
  await field.selectOption('team-runtime');
  await expect(field).not.toHaveAttribute('aria-invalid', 'true');
  expect(writes).toEqual([]);
});
for (const width of [320, 390])
  test(`named workspace actions fit ${width}px dark mode`, async ({ page }) => {
    await page.setViewportSize({ width, height: 844 });
    await page.emulateMedia({ colorScheme: 'dark' });
    await connect(page, ['team-runtime']);
    await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({
      path: `test-results/workspace-overview-${width}-dark.png`,
      fullPage: true,
    });
  });
