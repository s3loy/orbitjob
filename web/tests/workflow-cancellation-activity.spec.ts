import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page, accepted: boolean) {
  const demo = demoClient();
  const workflow = await demo.workflow('412');
  const run = { ...(await demo.workflowRun('412', '94')), phase: 'Running' };
  demo.dispose();
  const writes: string[] = [];
  await page.clock.install({ time: new Date('2026-10-25T14:00:00Z') });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const { pathname } = new URL(route.request().url());
    if (pathname === '/api/v1/identity') return route.fallback();
    if (route.request().method() !== 'GET') writes.push(pathname);
    if (pathname === '/api/v1/workflows/412') return route.fulfill({ json: workflow });
    if (pathname === '/api/v1/workflows/412/runs/94') return route.fulfill({ json: run });
    if (pathname === '/api/v1/workflows/412/runs/94/cancel')
      return accepted
        ? route.fulfill({
            json: {
              namespace: workflow.namespace,
              name: workflow.name,
              occurrence_key: run.occurrence_key,
              phase: 'CancelRequested',
            },
          })
        : route.fulfill({
            status: 503,
            json: {
              error: { code: 'UNAVAILABLE', message: 'Cancellation response not confirmed' },
            },
          });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/workflows/412/runs/94');
  await signIn(page, 'workflow_cancel_activity_fixture');
  return writes;
}

for (const accepted of [true, false])
  test(`${accepted ? 'accepted' : 'unknown'} Workflow cancellation remains discoverable after navigation without replay`, async ({
    page,
  }) => {
    const writes = await fixture(page, accepted);
    await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm cancel request', exact: true }).click();
    if (!accepted) {
      await expect(page.getByRole('dialog')).toContainText('request outcome is unknown');
      await page.getByRole('button', { name: 'Keep observing', exact: true }).click();
    }
    const activity = page.locator('details.operation-activity');
    await expect(activity).toHaveAttribute('open', '');
    await expect(activity.locator('.activity-count')).toHaveText('1');
    if (!accepted) await expect(activity.locator('summary').first()).toContainText('1 need review');
    const receipt = activity.getByRole('region', {
      name: 'Workflow cancellation receipt for run 94',
      exact: true,
    });
    await expect(receipt.locator('details')).not.toHaveAttribute('open', '');
    await page
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Alerts', exact: true })
      .click();
    await expect(page.getByRole('heading', { name: 'Alerts', exact: true })).toBeVisible();
    await expect(activity).not.toHaveAttribute('open', '');
    await activity.locator('summary').first().click();
    await expect(receipt.locator('summary')).toContainText(
      accepted ? 'Workflow cancellation accepted' : 'Workflow cancellation outcome unknown',
    );
    const original = receipt.getByRole('link', { name: 'Review workflow run #94', exact: true });
    await expect(original).toHaveAttribute('href', '/console/workflows/412/runs/94');
    await page.clock.fastForward(10000);
    expect(writes).toEqual(['/api/v1/workflows/412/runs/94/cancel']);
    await original.click();
    await expect(
      page.getByRole('heading', { name: 'Workflow run #94', exact: true }),
    ).toBeVisible();
    await expect(
      page.getByText(
        accepted
          ? 'Cancel request accepted. The observed workflow phase determines the outcome.'
          : 'Cancellation request outcome unknown. A stop has not been confirmed.',
        { exact: true },
      ),
    ).toBeVisible();
    expect(writes).toEqual(['/api/v1/workflows/412/runs/94/cancel']);
  });
