import { signIn } from './support';
import { expect, test } from './support';
import { demoClient } from '../src/api/demo';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });
async function connect(page: import('@playwright/test').Page, path: string) {
  await page.goto(path);
  await signIn(page, 'otj_product_lifecycle_fixture');
}
test('a lost Function update reconciles normalized labels and permits a second pause edit', async ({
  page,
}) => {
  const fixture = demoClient();
  let current = {
    ...(await fixture.functionDefinition('27')),
    version: 1,
    active_revision_version: 1,
  };
  fixture.dispose();
  let writes = 0;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/functions/27') {
      if (route.request().method() === 'PUT') {
        const { expected_version, ...input } = route.request().postDataJSON();
        expect(expected_version).toBe(current.version);
        writes++;
        current = {
          ...current,
          ...input,
          name: input.name.trim(),
          image: input.image.trim(),
          labels: { a: 1, z: 2 },
          version: current.version + 1,
          active_revision_version: current.version + 1,
          ready: input.status === 'active',
        };
        if (writes === 1)
          return route.fulfill({
            status: 503,
            json: { error: { code: 'INTERNAL', message: 'Save response unavailable' } },
          });
      }
      return route.fulfill({ json: current });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/functions/27/edit');
  await page.getByLabel('Function name').fill('  normalized-report  ');
  await page.getByText('Advanced settings', { exact: true }).click();
  await page.getByLabel('Labels JSON').fill('{"z":2,"a":1}');
  await page.getByRole('button', { name: 'Review function' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByRole('heading', { name: 'Save outcome unknown' })).toBeVisible();
  await page.getByRole('button', { name: 'Check stored definition' }).click();
  await expect(page.getByRole('heading', { name: 'Saved version is ready' })).toBeVisible();
  await page.getByRole('button', { name: 'Continue editing current version' }).click();
  await page.getByRole('combobox', { name: 'Invocation state' }).click();
  await page.getByRole('option', { name: 'Paused', exact: true }).click();
  await page.getByRole('button', { name: 'Review function' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByRole('heading', { name: 'Saved and paused' })).toBeVisible();
  expect(current.version).toBe(3);
  expect(writes).toBe(2);
});
test('Workflow preview uses the selected timezone and pausing its schedule still permits manual start', async ({
  page,
}) => {
  await page.clock.install({ time: new Date('2026-10-24T12:00:00Z') });
  const fixture = demoClient();
  const workflow = {
    ...(await fixture.workflow('412')),
    suspend: true,
    time_zone: 'Asia/Shanghai',
    concurrency_policy: 'Forbid',
  };
  fixture.dispose();
  let preview: unknown;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/workflows/schedule-preview') {
      preview = route.request().postDataJSON();
      return route.fulfill({
        json: { time_zone: 'Asia/Shanghai', upcoming: ['2026-10-25T00:00:00Z'] },
      });
    }
    if (url.pathname === '/api/v1/workflows/412') return route.fulfill({ json: workflow });
    if (url.pathname.endsWith('/runs') && route.request().method() === 'POST')
      return route.fulfill({
        json: {
          namespace: workflow.namespace,
          name: 'workflow-rejected',
          occurrence_key: 'manual-forbid',
          trigger: 'Manual',
          phase: 'Rejected',
          created: true,
        },
      });
    if (url.pathname.endsWith('/runs'))
      return route.fulfill({
        json: {
          items: [
            {
              id: 900,
              workflow_source_uid: workflow.source_uid,
              revision_id: 412,
              occurrence_key: 'manual-forbid',
              trigger: 'Manual',
              actor: 'fixture',
              phase: 'Rejected',
              reason: 'concurrency_forbid',
              created_at: '2026-10-24T00:00:00Z',
              updated_at: '2026-10-24T00:00:00Z',
            },
          ],
        },
      });
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/manifests/workflow');
  await page.getByText('Run policies', { exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Concurrency policy' })).toHaveText(
    'Prevent overlapping runs',
  );
  await page.getByRole('combobox', { name: 'Trigger mode' }).click();
  await page.getByRole('option', { name: 'Scheduled and manual' }).click();
  await page.getByRole('textbox', { name: 'Configured schedule', exact: true }).fill('0 8 * * *');
  await page.getByRole('textbox', { name: 'Time zone', exact: true }).fill('Asia/Shanghai');
  await page.getByRole('button', { name: 'Preview next starts' }).click();
  await expect(page.getByText('Times shown in Asia/Shanghai', { exact: false })).toBeVisible();
  expect(preview).toEqual({ schedule: '0 8 * * *', time_zone: 'Asia/Shanghai', count: 5 });
  await connect(page, '/console/workflows/412');
  await expect(page.getByText('Automatic starts paused', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: `Trigger ${workflow.name}` }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await expect(page.getByText('No steps were started.', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Open this execution' })).toHaveAttribute(
    'href',
    '/console/workflows/412/runs/900',
  );
});
