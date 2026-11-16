import { signIn } from './support';
import { test, expect, type Page } from './support';
const stamp = '2026-10-19T01:00:00Z';
const fn = {
  ready: true,
  active_revision_version: 1,
  id: 27,
  tenant_id: 'tenant',
  resource_group_id: '',
  name: 'indexer',
  description: 'Search catalog',
  status: 'active',
  image: 'busybox:1.37',
  command: ['sh'],
  args: [],
  timeout_seconds: 120,
  retry_limit: 1,
  history_success: 20,
  history_failed: 20,
  labels: {},
  version: 3,
  created_at: stamp,
  updated_at: stamp,
};
const wf = {
  id: 412,
  name: 'billing-close',
  namespace: 'orbitjob',
  source_uid: 'wf-source',
  generation: 1,
  schedule: '',
  suspend: false,
  fail_policy: 'Continue',
  timeout_seconds: 120,
  task_count: 1,
  created_at: stamp,
  tasks: [{ name: 'index', job_ref: 'indexer', depends_on: [] }],
};
const run = {
  id: 94,
  workflow_source_uid: 'wf-source',
  revision_id: 412,
  occurrence_key: 'wf-occurrence',
  trigger: 'Manual',
  actor: 'key',
  phase: 'Running',
  created_at: stamp,
  updated_at: stamp,
  steps: [],
};
async function setup(page: Page, kind = 'function') {
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    const path = new URL(route.request().url()).pathname;
    const data =
      path === '/api/v1/functions'
        ? { items: [fn] }
        : path === '/api/v1/functions/27'
          ? fn
          : path === '/api/v1/workflows'
            ? { items: [wf] }
            : path === '/api/v1/workflows/412'
              ? wf
              : path === '/api/v1/workflows/412/runs'
                ? { items: [run] }
                : path === '/api/v1/workflows/412/runs/94'
                  ? run
                  : { items: [] };
    return route.fulfill({ json: data });
  });
  await page.goto('/console/connection');
  await signIn(page, 'otj_m4_test_fake_key');
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: kind === 'function' ? 'Functions' : 'Workflows', exact: true })
    .click();
  await page
    .getByRole('link', { name: kind === 'function' ? 'indexer' : 'billing-close', exact: true })
    .click();
}
for (const kind of ['function', 'workflow'] as const) {
  test(`${kind} unknown result preserves request identity and gates new intent across navigation`, async ({
    page,
  }) => {
    await setup(page, kind);
    const keys: string[] = [];
    let attempt = 0;
    const endpoint =
      kind === 'function'
        ? '**/api/v1/functions/27/invoke?wait_seconds=0*'
        : /\/api\/v1\/workflows\/412\/runs(?:\?.*)?$/;
    await page.route(endpoint, async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      keys.push(route.request().headers()['x-orbitjob-idempotency-key']);
      if (++attempt === 1) return route.abort('connectionreset');
      return route.fulfill({
        json: {
          namespace: 'orbitjob',
          name: 'actual-run-ref',
          occurrence_key: 'server-occurrence-key',
          trigger: kind === 'function' ? 'Function' : 'Manual',
          phase: 'Pending',
          created: false,
        },
      });
    });
    const verb = kind === 'function' ? 'Invoke' : 'Trigger';
    await page
      .getByRole('button', {
        name: `${verb} ${kind === 'function' ? 'indexer' : 'billing-close'}`,
        exact: true,
      })
      .click();
    await page.getByRole('button', { name: `Confirm ${verb.toLowerCase()}`, exact: true }).click();
    await expect(page.getByText('The previous result is unknown.', { exact: false })).toBeVisible();
    await page.getByRole('button', { name: 'Keep reviewing', exact: true }).click();
    const receipt = page.getByRole('region', { name: new RegExp('receipt for') });
    await expect(receipt).toContainText('outcome unknown');
    await expect(receipt).not.toContainText('actual-run-ref');
    await expect(
      page.getByRole('button', {
        name: `${verb} ${kind === 'function' ? 'indexer' : 'billing-close'}`,
        exact: true,
      }),
    ).toBeDisabled();
    await page
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Checks', exact: true })
      .click();
    await expect(page).toHaveURL(/\/console\/checks$/);
    const activity = page.locator('details.operation-activity');
    await expect(activity).not.toHaveAttribute('open');
    await activity.locator('summary').first().click();
    await expect(receipt).toBeVisible();
    await receipt.getByRole('button', { name: 'Retry same request' }).click();
    await expect(receipt.locator('summary').first()).toContainText('accepted');
    await receipt.getByText('Request identity and original response', { exact: true }).click();
    await expect(receipt.getByText('server-occurrence-key', { exact: true })).toBeVisible();
    await expect(receipt).toContainText('server-occurrence-key');
    expect(keys).toHaveLength(2);
    expect(keys[0]).toBe(keys[1]);
    expect(keys[0]).not.toBe('server-occurrence-key');
    await expect(receipt.getByRole('button', { name: 'Dismiss' })).toBeEnabled();
    await receipt.getByRole('button', { name: 'Dismiss' }).click();
    await expect(receipt).toHaveCount(0);
  });
}
test('409 is rejected without blind replay; refresh then deliberate new confirmation', async ({
  page,
}) => {
  await setup(page);
  let count = 0;
  await page.route('**/api/v1/functions/27/invoke?wait_seconds=0*', (route) => {
    count++;
    return route.fulfill({
      status: 409,
      json: { error: { code: 'CONFLICT', message: 'The function has no active revision yet' } },
    });
  });
  await page.getByRole('button', { name: 'Invoke indexer' }).click();
  await page.getByRole('button', { name: 'Confirm invoke', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('This request was rejected');
  await expect(
    page.getByRole('dialog').getByRole('button', { name: 'Confirm invoke' }),
  ).toBeDisabled();
  await page.getByRole('button', { name: 'Keep reviewing' }).click();
  expect(count).toBe(1);
  await expect(page.getByRole('button', { name: 'Retry same request' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Refresh definition', exact: true }).click();
  await page.getByRole('button', { name: 'Invoke indexer' }).click();
  await expect(page.getByRole('button', { name: 'Confirm invoke' })).toBeEnabled();
  expect(count).toBe(1);
});
test('pending submit is guarded and disconnect clears unknown receipts', async ({ page }) => {
  await setup(page);
  let release!: () => void;
  const hold = new Promise<void>((r) => (release = r));
  let count = 0;
  await page.route('**/api/v1/functions/27/invoke?wait_seconds=0*', async (route) => {
    count++;
    await hold;
    await route.abort('connectionreset');
  });
  await page.getByRole('button', { name: 'Invoke indexer' }).click();
  await page.getByRole('button', { name: 'Confirm invoke' }).click();
  await expect(page.getByRole('button', { name: 'Keep reviewing' })).toBeDisabled();
  release();
  await expect(page.getByRole('dialog')).toContainText('unknown');
  await page.getByRole('button', { name: 'Keep reviewing' }).click();
  expect(count).toBe(1);
  await page.getByRole('link', { name: 'Account', exact: true }).first().click();
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await expect(page.getByRole('region', { name: /receipt for/ })).toHaveCount(0);
});
test('cancellation unknown persists; accepted response leaves observed phase unchanged', async ({
  page,
}) => {
  await setup(page, 'workflow');
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: '#94', exact: true }).click();
  let calls = 0;
  await page.route(/\/api\/v1\/workflows\/412\/runs\/94\/cancel(?:\?.*)?$/, (route) =>
    ++calls === 1
      ? route.abort('connectionreset')
      : route.fulfill({
          json: {
            namespace: 'orbitjob',
            name: 'wf-94',
            occurrence_key: 'wf-occurrence',
            phase: 'Running',
          },
        }),
  );
  await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm cancel request' }).click();
  await page.getByRole('button', { name: 'Keep observing' }).click();
  await expect(
    page.getByText('Cancellation request outcome unknown.', { exact: false }),
  ).toBeVisible();
  await page.getByRole('link', { name: 'Back to Workflow runs', exact: true }).click();
  await page.getByRole('link', { name: '#94', exact: true }).click();
  await expect(
    page.getByText('Cancellation request outcome unknown.', { exact: false }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Review cancel request' }).click();
  await page.getByRole('button', { name: 'Confirm cancel request' }).click();
  await expect(page.getByText('Cancel request accepted.', { exact: false })).toBeVisible();
  await expect(page.locator('.run-meta')).toContainText('Running');
  await expect(page.locator('.run-meta')).not.toContainText('Canceled');
});

test('Workflow unknown trigger blocks the same source after active revision changes', async ({
  page,
}) => {
  await setup(page, 'workflow');
  let posts = 0;
  await page.route(/\/api\/v1\/workflows\/412\/runs(?:\?.*)?$/, (route) =>
    route.request().method() === 'POST'
      ? (posts++, route.abort('connectionreset'))
      : route.fallback(),
  );
  await page.getByRole('button', { name: 'Trigger billing-close' }).click();
  await page.getByRole('button', { name: 'Confirm trigger' }).click();
  await expect(page.getByRole('dialog')).toContainText('unknown');
  await page.getByRole('button', { name: 'Keep reviewing' }).click();
  await page.route(/\/api\/v1\/workflows(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { items: [{ ...wf, id: 413 }] } }),
  );
  await page.route(/\/api\/v1\/workflows\/413(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { ...wf, id: 413 } }),
  );
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Workflows', exact: true })
    .click();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('link', { name: 'billing-close', exact: true })).toHaveAttribute(
    'href',
    '/console/workflows/413',
  );
  await page.getByRole('link', { name: 'billing-close', exact: true }).click();
  await page.getByText('Definition identity', { exact: true }).click();
  await expect(
    page.locator('.fact-pair').filter({ has: page.locator('dt', { hasText: /^Revision$/ }) }),
  ).toHaveText('Revision#413');
  await expect(page.getByRole('button', { name: 'Trigger billing-close' })).toBeDisabled();
  const activity = page.locator('details.operation-activity');
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator('summary').first().click();
  const replay = page.waitForRequest(
    (r) => r.method() === 'POST' && new URL(r.url()).pathname.endsWith('/workflows/412/runs'),
  );
  await page.getByRole('button', { name: 'Retry same request' }).click();
  await replay;
  expect(posts).toBe(2);
});

for (const accepted of [false, true])
  test(`Workflow ${accepted ? 'accepted' : 'unknown'} cancellation survives revision change`, async ({
    page,
  }) => {
    await setup(page, 'workflow');
    await page.getByRole('tab', { name: 'Runs', exact: true }).click();
    await page.getByRole('link', { name: '#94', exact: true }).click();
    await page.route(/\/api\/v1\/workflows\/412\/runs\/94\/cancel(?:\?.*)?$/, (route) =>
      accepted
        ? route.fulfill({
            json: {
              name: 'wf-94',
              namespace: 'orbitjob',
              occurrence_key: 'wf-occurrence',
              phase: 'Running',
            },
          })
        : route.abort('connectionreset'),
    );
    await page.getByRole('button', { name: 'Request cancel', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm cancel request' }).click();
    if (!accepted) {
      await expect(page.getByRole('dialog')).toContainText('unknown');
      await page.getByRole('button', { name: 'Keep observing' }).click();
    }
    await page.route(/\/api\/v1\/workflows(?:\?.*)?$/, (route) =>
      route.fulfill({ json: { items: [{ ...wf, id: 413 }] } }),
    );
    await page.route(/\/api\/v1\/workflows\/413(?:\?.*)?$/, (route) =>
      route.fulfill({ json: { ...wf, id: 413 } }),
    );
    await page.route(/\/api\/v1\/workflows\/413\/runs(?:\?.*)?$/, (route) =>
      route.fulfill({ json: { items: [run] } }),
    );
    await page.route(/\/api\/v1\/workflows\/413\/runs\/94(?:\?.*)?$/, (route) =>
      route.fulfill({ json: run }),
    );
    await page
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Workflows', exact: true })
      .click();
    await page.getByRole('button', { name: 'Refresh', exact: true }).click();
    await expect(page.getByRole('link', { name: 'billing-close', exact: true })).toHaveAttribute(
      'href',
      '/console/workflows/413',
    );
    await page.getByRole('link', { name: 'billing-close', exact: true }).click();
    await page.getByRole('tab', { name: 'Runs', exact: true }).click();
    await page.getByRole('link', { name: '#94', exact: true }).click();
    await expect(
      page.getByText(
        accepted ? 'Cancel request accepted.' : 'Cancellation request outcome unknown.',
        { exact: false },
      ),
    ).toBeVisible();
    if (accepted)
      await expect(page.getByRole('button', { name: 'Cancel requested' })).toBeDisabled();
    else await expect(page.getByRole('button', { name: 'Review cancel request' })).toBeEnabled();
  });

test('a directly opened CancelUnknown Workflow run supports observation without another stop request', async ({
  page,
}) => {
  await setup(page, 'workflow');
  await page.route(/\/api\/v1\/workflows\/412\/runs\/94(?:\?.*)?$/, (route) =>
    route.fulfill({ json: { ...run, phase: 'CancelUnknown' } }),
  );
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  await page.getByRole('link', { name: '#94', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Request cancel', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Refresh', exact: true })).toBeEnabled();
  await expect(page.getByText('A stop has not been confirmed.', { exact: false })).toBeVisible();
});
