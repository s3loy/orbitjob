import { signIn } from './support';
import { expect, fixtureIdentity, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

type Variant =
  | 'pinned'
  | 'legacy'
  | 'missing'
  | 'definition-denied'
  | 'run-denied'
  | 'retired'
  | 'retired-list-denied'
  | 'retired-get-denied'
  | 'retired-limit'
  | 'retired-cached';
async function history(page: Page, variant: Variant) {
  const client = demoClient();
  const definition = await client.workflow('412');
  const run = await client.workflowRun('412', '94');
  client.dispose();
  let historyOpened = false;
  const lookups: number[] = [];
  const currentReads: string[] = [];
  const current = { ...definition, id: 613, name: 'Renamed current workflow' };
  let retired = variant.startsWith('retired') && variant !== 'retired-cached';
  if (variant === 'legacy') {
    delete run.tasks;
    delete run.task_decisions;
    delete run.pinned_revision_available;
    run.steps = run.steps.map((step) => {
      const old = { ...step };
      delete old.task_name;
      return old;
    });
  }
  if (variant === 'missing') {
    run.tasks = null;
    run.pinned_revision_available = false;
  }
  if (variant === 'pinned')
    run.steps = [{ ...run.steps[0], id: 888, task_name: undefined }, ...run.steps];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    const path = url.pathname;
    const deny = () =>
      route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Independent grant denied' } },
      });
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: {
          ...fixtureIdentity,
        },
      });
    if (path === '/api/v1/workflows') {
      if (retired && historyOpened) {
        const offset = Number(url.searchParams.get('offset'));
        lookups.push(offset);
        if (variant === 'retired-list-denied' || variant === 'retired-cached') return deny();
        const unrelated = Array.from({ length: 20 }, (_, index) => ({
          ...definition,
          id: 1000 + offset + index,
          source_uid: `unrelated-${offset + index}`,
        }));
        return route.fulfill({
          json: { items: offset === 0 || variant === 'retired-limit' ? unrelated : [current] },
        });
      }
      return route.fulfill({ json: { items: [definition] } });
    }
    if (path === '/api/v1/workflows/412') {
      if (retired && historyOpened)
        return route.fulfill({
          status: 404,
          json: { error: { code: 'NOT_FOUND', message: 'The old revision is inactive.' } },
        });
      return variant === 'definition-denied' ? deny() : route.fulfill({ json: definition });
    }
    if (path === '/api/v1/workflows/613') {
      currentReads.push(path);
      return variant === 'retired-get-denied' ? deny() : route.fulfill({ json: current });
    }
    if (path === '/api/v1/workflows/412/runs') return route.fulfill({ json: { items: [run] } });
    if (path === '/api/v1/workflows/412/runs/94') {
      historyOpened = true;
      return variant === 'run-denied' ? deny() : route.fulfill({ json: run });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/');
  await signIn(page, 'fixture_not_a_real_credential');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Workflows', exact: true })
    .click();
  await page.getByRole('link', { name: definition.name, exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: '#94', exact: true }).click();
  return {
    lookups,
    currentReads,
    retireDefinition: () => {
      retired = true;
    },
  };
}
async function revealRunContext(page: Page) {
  const context = page.locator('details[aria-label="Current workflow definition context"]');
  await context.locator(':scope > summary').click();
  return context;
}
test('pinned history retains old DAG, skip evidence and exact task mapping', async ({ page }) => {
  await history(page, 'pinned');
  const pinned = page.getByRole('region', { name: 'Pinned workflow history' });
  await expect(pinned.getByRole('group', { name: 'Pinned revision tasks' })).toBeVisible();
  await expect(pinned.getByText('Revision #398', { exact: true })).toBeVisible();
  await expect(pinned.getByRole('button', { name: /reconcile/ })).toHaveCount(0);
  await pinned.getByRole('button', { name: /publish billing-publish-v2/ }).click();
  await expect(pinned.getByRole('region', { name: 'Selected pinned task' })).toContainText(
    'Skipped (fail_fast)',
  );
  await expect(
    page.getByRole('row').filter({ has: page.getByRole('link', { name: '#888', exact: true }) }),
  ).toContainText('Not recorded');
  await expect(
    page.getByRole('row').filter({ has: page.getByRole('link', { name: '#1040', exact: true }) }),
  ).toContainText('extract');
  const context = await revealRunContext(page);
  await expect(context.getByRole('link', { name: 'Revision #412', exact: true })).toBeVisible();
});
for (const variant of ['legacy', 'missing'] as const)
  test(`${variant} history stays readable without current-DAG substitution`, async ({ page }) => {
    await history(page, variant);
    await expect(page.getByText('Pinned task graph unavailable', { exact: true })).toBeVisible();
    await expect(page.getByRole('group', { name: 'Pinned revision tasks' })).toHaveCount(0);
    await expect(page.getByRole('link', { name: '#1040', exact: true })).toBeVisible();
    if (variant === 'legacy') await page.getByText('Task decisions', { exact: true }).click();
    await expect(
      page.getByText(
        variant === 'legacy'
          ? 'Skip decisions are unavailable for this run.'
          : 'The pinned revision could not be loaded.',
        { exact: false },
      ),
    ).toBeVisible();
  });
test('independent current-definition denial does not hide recorded pinned history', async ({
  page,
}) => {
  await history(page, 'definition-denied');
  await expect(page.getByRole('group', { name: 'Pinned revision tasks' })).toBeVisible();
  await expect(
    page.getByText('The current definition is unavailable.', { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
});
test('workflow run denial does not show a current graph as historical evidence', async ({
  page,
}) => {
  await history(page, 'run-denied');
  await expect(page.getByRole('alert')).toContainText('Permission required');
  await expect(page.getByRole('group', { name: 'Pinned revision tasks' })).toHaveCount(0);
});

test('retired parent resolves the current definition by exact source UID across authorized pages', async ({
  page,
}) => {
  const { lookups, currentReads } = await history(page, 'retired');
  const pinned = page.getByRole('region', { name: 'Pinned workflow history' });
  const context = await revealRunContext(page);
  await expect(pinned.getByText('Revision #398', { exact: true })).toBeVisible();
  await expect(pinned.getByRole('group', { name: 'Pinned revision tasks' })).toBeVisible();
  await expect(context.getByRole('link', { name: 'Revision #613', exact: true })).toHaveAttribute(
    'href',
    '/console/workflows/613',
  );
  await expect(context).toContainText('Renamed current workflow');
  await expect(context.getByRole('link', { name: 'Revision #412', exact: true })).toHaveCount(0);
  await expect(page.getByRole('alert')).toHaveCount(0);
  expect(lookups).toEqual([0, 20]);
  expect(currentReads).toEqual(['/api/v1/workflows/613']);
});

for (const variant of ['retired-list-denied', 'retired-get-denied', 'retired-limit'] as const)
  test(`${variant} keeps pinned history readable without a dead current-definition link`, async ({
    page,
  }) => {
    const { lookups, currentReads } = await history(page, variant);
    const context = await revealRunContext(page);
    await expect(page.getByRole('group', { name: 'Pinned revision tasks' })).toBeVisible();
    await expect(page.getByRole('link', { name: '#1040', exact: true })).toBeVisible();
    await expect(
      page.getByText('The current definition is unavailable.', { exact: false }),
    ).toBeVisible();
    await expect(context).toContainText('at most 200 workflows available to your account');
    await expect(page.getByRole('link', { name: 'Browse workflows', exact: true })).toHaveAttribute(
      'href',
      '/console/workflows',
    );
    await expect(
      context.getByRole('link', { name: /Revision|Open current definition/, includeHidden: true }),
    ).toHaveCount(0);
    await expect(page.getByRole('alert')).toHaveCount(0);
    expect(lookups).toEqual(
      variant === 'retired-limit'
        ? Array.from({ length: 10 }, (_, i) => i * 20)
        : variant === 'retired-get-denied'
          ? [0, 20]
          : [0],
    );
    expect(currentReads).toHaveLength(variant === 'retired-get-denied' ? 1 : 0);
  });

test('a stale successful current definition is hidden when its replacement lookup is denied', async ({
  page,
}) => {
  await page.clock.install();
  const { retireDefinition, lookups } = await history(page, 'retired-cached');
  const context = await revealRunContext(page);
  await expect(context.getByRole('link', { name: 'Revision #412', exact: true })).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await expect(page).toHaveURL(/\/console\/jobs$/);
  retireDefinition();
  // Expire the application's 10-second query freshness without a real-time wait.
  await page.clock.fastForward(10_001);
  await page.goBack();
  await expect(page).toHaveURL(/\/console\/workflows\/412\/runs\/94$/);
  await revealRunContext(page);
  await expect(
    page.getByText('The current definition is unavailable.', { exact: false }),
  ).toBeVisible();
  await expect(
    context.getByRole('link', { name: /Revision|Open current definition/, includeHidden: true }),
  ).toHaveCount(0);
  await expect(page.getByRole('group', { name: 'Pinned revision tasks' })).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
  expect(lookups).toEqual([0]);
});
