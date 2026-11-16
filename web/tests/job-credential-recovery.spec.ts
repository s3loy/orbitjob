import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const job = { ...(await demo.job('17')), concurrency_policy: 'Replace' };
  const recorded = await demo.run('1042');
  demo.dispose();
  const run = {
    ...recorded,
    id: 77,
    occurrence_key: 'credential-recovery-occurrence',
    phase: 'Running',
    attempts: recorded.attempts.map((attempt) => ({ ...attempt, run_id: 77 })),
  };
  const state = {
    failure: '',
    status: 401,
    triggers: 0,
    reads: [] as string[],
  };
  await page.route('**/openapi.json', (route) =>
    route.fulfill({
      json: {
        paths: {
          '/api/v1/instances': {
            get: {
              parameters: [
                { name: 'source_uid', in: 'query' },
                { name: 'occurrence_key', in: 'query' },
              ],
            },
          },
        },
      },
    }),
  );
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const path = new URL(route.request().url()).pathname;
    if (path === '/api/v1/identity') return route.fallback();
    if (route.request().method() === 'GET') state.reads.push(path);
    if (path === state.failure || (state.failure === 'logs' && path.endsWith('/logs')))
      return route.fulfill({
        status: state.status,
        json: { error: { code: 'REJECTED', message: 'Fixture access rejected' } },
      });
    if (path === '/api/v1/jobs/17/trigger') {
      state.triggers++;
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'retained-trigger',
          occurrence_key: run.occurrence_key,
          trigger: 'Manual',
          phase: 'Pending',
          created: true,
        },
      });
    }
    return route.fulfill({
      json:
        path === '/api/v1/jobs'
          ? { items: [job] }
          : path === '/api/v1/jobs/17'
            ? job
            : path === '/api/v1/instances'
              ? { items: [run] }
              : path === '/api/v1/instances/77'
                ? run
                : path === '/api/v1/instances/summary'
                  ? { total: 1, phases: { Running: 1 } }
                  : { items: [] },
    });
  });
  return { state, job, run };
}

async function connect(page: Page, path: string) {
  await page.goto(`/console${path}`);
  await signIn(page, 'job_credential_recovery_fixture');
}

async function navigate(page: Page, name: 'Jobs' | 'Overview') {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

for (const entry of [
  { path: '/jobs', failure: '/api/v1/jobs', heading: 'Jobs' },
  { path: '/jobs/17', failure: '/api/v1/jobs/17', heading: 'Job unavailable' },
  { path: '/jobs/17', failure: '/api/v1/instances', heading: 'Job unavailable' },
  { path: '/overview', failure: '/api/v1/instances/summary', heading: 'Overview' },
])
  test(`${entry.failure} 401 invalidates the connection before another page can use cached actions`, async ({
    page,
  }) => {
    const { state } = await fixture(page);
    state.failure = entry.failure;
    await connect(page, entry.path);
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(page.locator('.sidebar')).toHaveCount(0);
    await expect(page.getByRole('button', { name: /^Trigger / })).toHaveCount(0);
    expect(state.triggers).toBe(0);
  });

test('log 401 clears cached actions and the accepted trigger receipt with the session', async ({
  page,
}) => {
  const { state, job } = await fixture(page);
  await connect(page, '/overview');
  await expect(page.getByRole('region', { name: 'Execution outcomes' })).toContainText(
    'Recorded runs',
  );
  await navigate(page, 'Jobs');
  await page.getByRole('link', { name: job.name, exact: true }).click();
  await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('When the active policy is Forbid');
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const receipt = page.getByRole('region', { name: `Trigger receipt for ${job.name}` });
  await receipt.getByRole('link', { name: 'View run', exact: true }).click();
  state.failure = 'logs';
  await page.locator('details.attempt-log-disclosure > summary').click();
  await page.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.sidebar')).toHaveCount(0);
  await expect(receipt).toHaveCount(0);
  expect(state.triggers).toBe(1);
});

for (const path of ['/jobs', '/overview'])
  test(`${path} denied reads do not infer missing Create permission`, async ({ page }) => {
    const { state } = await fixture(page);
    state.status = 403;
    state.failure = path === '/jobs' ? '/api/v1/jobs' : '/api/v1/instances/summary';
    await connect(page, path);
    await expect(page.getByRole('alert').first()).toContainText('Permission required');
    await page.getByRole('link', { name: 'Create a job', exact: true }).first().click();
    await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toBeEnabled();
    expect(state.triggers).toBe(0);
  });

test('job definition explains shared Forbid admission and scheduled Replace cancellation', async ({
  page,
}) => {
  const { job } = await fixture(page);
  const definition = page.locator('.definition-list').first();
  for (const [policy, label, explanation] of [
    [
      'Forbid',
      'Prevent overlapping runs',
      'Scheduled, manual and workflow-task runs cannot overlap. Accepted manual starts wait for earlier runs to finish.',
    ],
    [
      'Replace',
      'Stop earlier runs for scheduled starts',
      'A scheduled start requests cancellation and waits for prior runs to finish. Manual and workflow-task starts leave running work in place.',
    ],
  ]) {
    job.concurrency_policy = policy;
    await connect(page, '/jobs/17');
    await page.getByRole('tab', { name: 'Definition', exact: true }).click();
    const policySummary = definition
      .locator('dt', { hasText: /^Overlapping runs$/ })
      .locator('+ dd');
    await expect(policySummary).toContainText(label);
    await expect(policySummary).toContainText(explanation);
  }
});

for (const denied of [false, true])
  test(`exact trigger correlation ${denied ? 'retains a denied history read' : 'refreshes an initially empty history in place'}`, async ({
    page,
  }) => {
    const { state, job, run } = await fixture(page);
    let historyReads = 0;
    await page.route(/\/api\/v1\/instances(?:\?.*)?$/, (route) => {
      const params = new URL(route.request().url()).searchParams;
      if (!params.has('source_uid')) return route.fallback();
      if (params.has('occurrence_key')) return route.fulfill({ json: { items: [run] } });
      historyReads++;
      return denied
        ? route.fulfill({
            status: 403,
            json: { error: { code: 'FORBIDDEN', message: 'History read denied' } },
          })
        : route.fulfill({ json: { items: state.triggers ? [run] : [] } });
    });
    await connect(page, '/jobs/17');
    if (denied)
      await expect(
        page.getByRole('tabpanel', { name: 'Runs', exact: true }).getByRole('alert'),
      ).toContainText('History read denied');
    else await expect(page.getByText('No runs to show', { exact: true })).toBeVisible();
    const before = historyReads;
    await page.getByRole('button', { name: `Trigger ${job.name}`, exact: true }).click();
    await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
    const receipt = page.getByRole('region', { name: `Trigger receipt for ${job.name}` });
    await expect(receipt.getByRole('link', { name: 'View run', exact: true })).toBeVisible();
    if (denied) {
      await expect(
        page.getByRole('tabpanel', { name: 'Runs', exact: true }).getByRole('alert'),
      ).toContainText('History read denied');
      await expect(page.getByRole('link', { name: 'View run 77', exact: true })).toHaveCount(0);
      expect(historyReads).toBe(before);
    } else {
      await expect(page.getByRole('link', { name: 'View run 77', exact: true })).toBeVisible();
      expect(historyReads).toBeGreaterThan(before);
    }
    expect(state.triggers).toBe(1);
  });

for (const accepted of [false, true])
  test(`known 401 clears the local ${accepted ? 'accepted' : 'unknown'} definition receipt without repeating its write`, async ({
    page,
  }) => {
    const { state } = await fixture(page);
    const resource = {
      kind: 'jobs',
      namespace: 'finance-prod',
      name: 'credential-draft',
      uid: 'retained-definition-uid',
      resource_version: '2',
      generation: 1,
      spec_json: '{"schedule":"","jobTemplate":{"image":"busybox:1.37"}}',
      manifest_digest: 'retained-digest',
      last_request_id: '',
      last_manifest_digest: '',
    };
    let writes = 0;
    await page.route('**/api/v1/definitions/jobs/**', (route) => {
      if (route.request().method() === 'GET') return route.fulfill({ json: resource });
      writes++;
      const body = route.request().postDataJSON();
      if (!body.dry_run && !accepted) return route.abort('connectionreset');
      return route.fulfill({
        json: {
          ...resource,
          spec_json: body.spec_json,
          request_id: body.request_id,
          state: body.dry_run ? 'validated' : 'accepted',
          last_request_id: body.dry_run ? '' : body.request_id,
          last_manifest_digest: body.dry_run ? '' : resource.manifest_digest,
        },
      });
    });
    await connect(page, '/definitions/jobs?namespace=finance-prod&name=credential-draft');
    await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
    await page.getByRole('button', { name: 'Save definition', exact: true }).click();
    await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
    if (accepted)
      await expect(
        page.getByRole('heading', { name: 'Saved configuration', exact: true }),
      ).toBeVisible();
    else await expect(page.getByText('Save outcome unknown', { exact: true })).toBeVisible();
    state.failure = '/api/v1/jobs';
    await navigate(page, 'Jobs');
    await expect(page.getByRole('alert').first()).toContainText('Your session has ended.');
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole('link', {
        name: accepted ? 'Review saved definition' : 'Review draft',
        exact: true,
      }),
    ).toHaveCount(0);
    await expect(page.getByLabel('Definition spec JSON', { exact: true })).toHaveCount(0);
    expect(writes).toBe(2);
  });
