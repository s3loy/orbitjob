import { signIn } from './support';
import { expect, test } from './support';
import { demoClient } from '../src/api/demo';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

for (const historical of [false, true])
  test(`Workflow failure evidence remains honest for ${historical ? 'historical conflicting success' : 'a missing reference'}`, async ({
    page,
  }) => {
    const fixture = demoClient();
    const workflow = await fixture.workflow('412');
    const original = await fixture.workflowRun('412', '94');
    fixture.dispose();
    const run = {
      ...original,
      phase: historical ? 'Succeeded' : 'Failed',
      steps: [],
      pinned_revision_available: true,
      tasks: [{ name: 'export', job_ref: 'export-typo', depends_on: [] }],
      task_decisions: {
        export: { skipped: true, reason: historical ? 'deadline' : 'missing_job_reference' },
      },
    };
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fallback();
      if (path === '/api/v1/workflows/412') return route.fulfill({ json: workflow });
      if (path === '/api/v1/workflows/412/runs/94') return route.fulfill({ json: run });
      return route.fulfill({ json: { items: [] } });
    });
    await page.goto('/console/workflows/412/runs/94');
    await signIn(page, 'workflow_failure_guidance_fixture');
    const history = page.getByRole('region', { name: 'Pinned workflow history' });
    if (historical) {
      await expect(history).toContainText('Task decisions conflict with the recorded success.');
      await expect(history).toContainText('Check the failed tasks to verify the outcome.');
      await expect(page.locator('.run-meta')).toContainText('Succeeded');
    } else {
      await expect(history).toContainText('Job reference missing');
      await expect(history).toContainText('same namespace as this Workflow');
    }
    await expect(history).toContainText('export-typo');
    await expect(history).not.toContainText('missing_job_reference');
    await expect(page.getByText('No recorded steps', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeDisabled();
  });
