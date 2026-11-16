import { expect, test, fixtureAccount, fixtureIdentity } from './support';
const tenant = fixtureIdentity.tenant_id;
const other = '00000000000000000000000003';
test('human capability probes stay correctable and workspace changes clear probe drafts', async ({
  page,
}) => {
  const requests: string[] = [];
  await page.route('**/api/v1/auth/session', (route) =>
    route.fulfill({
      json: {
        ...fixtureAccount,
        workspaces: [
          ...fixtureAccount.workspaces,
          { id: other, name: 'Other workspace', slug: 'other', role: 'admin' },
        ],
      },
    }),
  );
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/v1/auth/')) return route.fallback();
    requests.push(url.pathname + url.search);
    if (url.pathname.endsWith('/identity')) {
      if (url.searchParams.get('action') === 'invalid')
        return route.fulfill({
          status: 400,
          json: { error: { code: 'VALIDATION', message: 'Invalid action' } },
        });
      const target = url.searchParams.get('tenant_id') || tenant;
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          tenant_id: target,
          target_tenant_id: target,
          capabilities: url.searchParams.has('action')
            ? [{ action: 'job:Get', resource: `orbitjob:${target}:*:job/17`, allowed: true }]
            : [],
        },
      });
    }
    return route.fulfill({
      json: url.pathname.endsWith('/summary') ? { total: 0, phases: {} } : { items: [] },
    });
  });
  await page.goto('/console/connection');
  await page.getByText('Check a permission', { exact: true }).click();
  await page.getByLabel('Capability action').fill('invalid');
  await page.getByLabel('Capability resource').fill(`orbitjob:${tenant}:*:job/17`);
  await page.getByRole('button', { name: 'Check policy grant' }).click();
  await expect(page.getByRole('alert')).toContainText('Invalid action');
  await page.getByLabel('Capability action').fill('job:Get');
  await page.getByRole('button', { name: 'Check policy grant' }).click();
  await expect(page.getByText('job:Get: Allowed by policy')).toBeVisible();
  await page.getByLabel('Workspace', { exact: true }).selectOption(other);
  await page.getByRole('button', { name: 'Open workspace' }).click();
  await page.locator('.sidebar-bottom').getByRole('link', { name: 'Account', exact: true }).click();
  await page.getByText('Check a permission', { exact: true }).click();
  await expect(page.getByLabel('Capability action')).toHaveValue('');
  expect(requests.some((p) => p.includes(`tenant_id=${other}`))).toBe(true);
});
