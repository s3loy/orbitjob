import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function connect(page: Page, path: string) {
  await page.route('**/openapi.json', (route) => route.fulfill({ json: { paths: {} } }));
  await page.goto(path);
  await signIn(page, 'activity_recovery_fixture');
}
test('Activity collapses across navigation while receipts and nonterminal observation remain mounted', async ({
  page,
}) => {
  const fixture = demoClient();
  const check = await fixture.check('1');
  const sli = await fixture.sli('11');
  const workflow = await fixture.workflow('412');
  fixture.dispose();
  let accepted = false,
    reads = 0,
    phase = 'Running';
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (path === '/api/v1/checks/1') return route.fulfill({ json: check });
    if (path === '/api/v1/checks') return route.fulfill({ json: { items: [check] } });
    if (path === '/api/v1/slis' && route.request().method() === 'POST')
      return route.fulfill({ json: { id: 111 } });
    if (path === '/api/v1/slis/111')
      return route.fulfill({ json: { ...sli, id: 111, name: 'activity-indicator' } });
    if (path === '/api/v1/workflows') return route.fulfill({ json: { items: [workflow] } });
    if (path === '/api/v1/workflows/412') return route.fulfill({ json: workflow });
    if (path === '/api/v1/workflows/412/runs') {
      if (route.request().method() === 'POST') {
        accepted = true;
        return route.fulfill({
          json: {
            namespace: workflow.namespace,
            name: 'activity-workflow',
            occurrence_key: 'activity-occurrence',
            trigger: 'Manual',
            phase: 'Pending',
            created: true,
          },
        });
      }
      if (accepted) reads++;
      return route.fulfill({
        json: {
          items: accepted
            ? [
                {
                  id: 901,
                  workflow_source_uid: workflow.source_uid,
                  revision_id: 412,
                  occurrence_key: 'activity-occurrence',
                  trigger: 'Manual',
                  actor: 'fixture',
                  phase,
                  created_at: '2026-10-25T00:00:00Z',
                  updated_at: '2026-10-25T00:00:00Z',
                },
              ]
            : [],
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await connect(page, '/console/slis?create=1&source_uid=check-1');
  await page.getByLabel('Name', { exact: true }).fill('activity-indicator');
  await page.getByRole('button', { name: 'Create SLI', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'activity-indicator', exact: true }),
  ).toBeVisible();
  const navigation = page.getByRole('navigation', { name: 'Main navigation' });
  await navigation.getByRole('link', { name: 'Workflows', exact: true }).click();
  await page.getByRole('link', { name: workflow.name, exact: true }).click();
  await page.getByRole('button', { name: `Trigger ${workflow.name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  await expect.poll(() => reads).toBeGreaterThan(0);
  await navigation.getByRole('link', { name: 'Alerts', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Alerts', exact: true })).toBeVisible();
  const activity = page.locator('details.operation-activity');
  await expect(activity).not.toHaveAttribute('open');
  await expect(activity.locator('summary').first()).toContainText('Activity 2');
  await expect(activity.getByText('Created activity-indicator', { exact: true })).not.toBeVisible();
  const before = reads;
  phase = 'Succeeded';
  await expect.poll(() => reads, { timeout: 8000 }).toBeGreaterThan(before);
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator('summary').first().click();
  await expect(activity.getByText('Created activity-indicator', { exact: true })).toBeVisible();
  await expect(activity.getByRole('region', { name: 'Observed execution' })).toContainText(
    'Succeeded',
  );
  await expect(
    activity.getByRole('link', { name: 'Open this execution', exact: true }),
  ).toHaveAttribute('href', '/console/workflows/412/runs/901');
});

for (const kind of ['jobs', 'workflows'] as const)
  test(`${kind} confirmation leads with the checked human-readable configuration`, async ({
    page,
  }) => {
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fallback();
      if (path.startsWith(`/api/v1/definitions/${kind}/`) && route.request().method() === 'PUT') {
        const input = route.request().postDataJSON();
        return route.fulfill({
          json: {
            kind,
            namespace: 'orbitjob',
            name: kind === 'jobs' ? 'nightly-disk-usage' : 'billing-report-delivery',
            uid: '',
            resource_version: '',
            generation: 1,
            spec_json: input.spec_json,
            manifest_digest: 'review-digest',
            last_request_id: '',
            last_manifest_digest: '',
            request_id: input.request_id,
            state: 'validated',
          },
        });
      }
      return route.fulfill({ json: { items: [] } });
    });
    await connect(page, `/console/definitions/${kind}`);
    await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
    await page.getByRole('button', { name: 'Save definition', exact: true }).click();
    const dialog = page.getByRole('dialog');
    const summary = dialog.getByRole('region', { name: 'Workload save summary' });
    await expect(summary).toContainText('0 2 * * *');
    await expect(summary).toContainText('future runs');
    if (kind === 'jobs') {
      await expect(summary).toContainText('busybox:1.37.0');
      await expect(summary.getByRole('list', { name: 'Command entries' })).toHaveText('df');
      await expect(
        summary.getByRole('list', { name: 'Argument entries' }).getByRole('listitem'),
      ).toHaveText(['-P', '/']);
    } else {
      await expect(summary).toContainText('billing-extract');
      await expect(summary).toContainText('billing-publish');
      await expect(summary).toContainText('after extract');
    }
    const exact = dialog
      .locator('details')
      .filter({ hasText: 'Checked JSON and version protection' });
    await expect(exact).not.toHaveAttribute('open');
    await expect(dialog.getByLabel('Checked definition spec')).not.toBeVisible();
    await exact.locator('summary').click();
    await expect(dialog.getByLabel('Checked definition spec')).toBeVisible();
  });

for (const variant of ['status-only', 'spec-changed', 'uid-replaced'] as const)
  test(`409 recovery preserves edits and handles ${variant} without an automatic overwrite`, async ({
    page,
  }) => {
    const fixture = demoClient();
    const original = await fixture.definition('jobs', 'finance-prod', 'billing-reconciliation');
    fixture.dispose();
    let conflicted = false;
    const live = {
      ...original,
      resource_version: '101',
      ...(variant === 'spec-changed'
        ? {
            spec_json: original.spec_json.replace('300', '50'),
            manifest_digest: 'externally-changed',
          }
        : {}),
      ...(variant === 'uid-replaced'
        ? { uid: 'replacement-uid', manifest_digest: 'replacement-digest' }
        : {}),
    };
    const requests: Record<string, unknown>[] = [];
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fallback();
      if (path === '/api/v1/definitions/jobs/finance-prod/billing-reconciliation') {
        if (route.request().method() === 'GET')
          return route.fulfill({ json: conflicted ? live : original });
        const input = route.request().postDataJSON();
        requests.push(input);
        if (!conflicted) {
          conflicted = true;
          return route.fulfill({
            status: 409,
            json: { error: { code: 'CONFLICT', message: 'Resource version changed' } },
          });
        }
        return route.fulfill({
          json: {
            ...live,
            spec_json: input.spec_json,
            request_id: input.request_id,
            state: 'validated',
            manifest_digest: 'checked-edits',
          },
        });
      }
      return route.fulfill({ json: { items: [] } });
    });
    await connect(
      page,
      '/console/definitions/jobs?namespace=finance-prod&name=billing-reconciliation',
    );
    await expect(page.getByLabel('Timeout in seconds', { exact: true })).toHaveValue('300');
    await page.getByLabel('Timeout in seconds', { exact: true }).fill('301');
    await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
    await page.getByRole('button', { name: 'Refresh version and keep edits', exact: true }).click();
    await expect(page.getByLabel('Timeout in seconds', { exact: true })).toHaveValue('301');
    expect(requests).toHaveLength(1);
    expect(requests[0].expected_resource_version).toBe('100');
    if (variant === 'status-only') {
      await expect(page.getByText('Live version refreshed.', { exact: false })).toBeVisible();
      await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
      await expect(
        page.getByRole('button', { name: 'Save definition', exact: true }),
      ).toBeEnabled();
      expect(requests).toHaveLength(2);
      expect(requests[1].expected_resource_version).toBe('101');
      expect(JSON.parse(String(requests[1].spec_json)).timeoutSeconds).toBe(301);
    } else {
      const conflict = page.getByRole('region', { name: 'Live definition conflict' });
      await expect(conflict).toBeVisible();
      await expect(
        page.getByRole('button', { name: 'Check against cluster', exact: true }),
      ).toBeDisabled();
      await expect(
        page.getByRole('button', { name: 'Save definition', exact: true }),
      ).toBeDisabled();
      await expect(
        conflict.getByRole('button', { name: 'Replace draft with live spec' }),
      ).toBeVisible();
      if (variant === 'uid-replaced')
        await expect(conflict.getByRole('button', { name: 'Merge changes' })).toHaveCount(0);
      else await expect(conflict.getByRole('button', { name: 'Merge changes' })).toBeVisible();
      expect(requests).toHaveLength(1);
    }
    expect(requests.every((request) => request.dry_run === true)).toBe(true);
  });
