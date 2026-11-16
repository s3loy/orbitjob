import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';
import { parse, stringify } from 'yaml';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

// Keep transport fixtures independent of Vite's browser-only JSON schema modules.
const workflowFixture = {
  apiVersion: 'workloads.orbitjob.io/v1alpha1',
  kind: 'WorkflowJob',
  metadata: { name: 'billing-close', namespace: 'orbitjob' },
  spec: {
    schedule: '',
    timeZone: 'UTC',
    suspend: false,
    concurrencyPolicy: 'Forbid',
    failPolicy: 'FailFast',
    timeoutSeconds: 120,
    history: { successfulRuns: 3, failedRuns: 3 },
    tasks: [
      { name: 'extract', jobRef: { name: 'billing-extract' } },
      {
        name: 'publish',
        jobRef: { name: 'billing-publish' },
        dependsOn: ['extract'],
        condition: {
          type: 'all',
          rules: [{ task: 'extract', field: 'phase', operator: 'eq', values: ['Succeeded'] }],
        },
      },
    ],
  },
};

async function fixture(page: Page) {
  const demo = demoClient();
  const original = await demo.job('17');
  demo.dispose();
  const state = { status: 200, reads: [] as URL[], details: 0 };
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/jobs') {
      state.reads.push(url);
      if (state.status !== 200)
        return route.fulfill({
          status: state.status,
          json: { error: { code: 'DENIED', message: 'Job listing unavailable' } },
        });
      const namespace = url.searchParams.get('namespace') ?? '';
      const offset = Number(url.searchParams.get('offset'));
      const search = url.searchParams.get('search') ?? '';
      const items = offset
        ? [{ ...original, namespace, id: 99, name: 'chosen-job', source_kind: 'kubernetes' }]
        : Array.from({ length: 20 }, (_, index) => ({
            ...original,
            namespace,
            id: index + 20,
            name: index === 0 ? 'first-job' : `function-projection-${index}`,
            source_kind: index === 0 ? 'kubernetes' : 'function',
          }));
      return route.fulfill({ json: { items: search === 'missing' ? [] : items } });
    }
    if (/\/api\/v1\/jobs\/\d+$/.test(url.pathname)) state.details++;
    return route.fulfill({ json: { items: [] } });
  });
  return { state, original };
}

async function connect(page: Page, path = '/console/manifests/workflow') {
  await page.goto(path);
  await signIn(page, 'workflow_job_picker_fixture');
}

async function importWorkflow(page: Page) {
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByLabel('Generated YAML', { exact: true }).fill(stringify(workflowFixture));
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('tab', { name: 'Tasks', exact: true }).click();
}

test('workflow tasks choose authorized ScheduledJobs by name without changing dependencies or rules', async ({
  page,
}) => {
  const { state } = await fixture(page);
  await connect(page);
  await importWorkflow(page);
  await page.getByRole('button', { name: 'publish', exact: true }).click();
  await page.getByRole('button', { name: 'Choose an existing job', exact: true }).click();
  const picker = page.getByRole('region', { name: 'Jobs in workflow namespace' });
  await expect(
    picker.getByRole('button', { name: 'Use job first-job', exact: true }),
  ).toBeVisible();
  await expect(picker.getByRole('button', { name: /Use job function-projection/ })).toHaveCount(0);
  await expect(picker.getByRole('button', { name: 'More jobs', exact: true })).toBeEnabled();
  await picker.getByRole('button', { name: 'More jobs', exact: true }).click();
  await picker.getByRole('button', { name: 'Use job chosen-job', exact: true }).click();
  await expect(page.getByLabel('Referenced Job name', { exact: true })).toHaveValue('chosen-job');
  await expect(page.getByLabel('Dependencies', { exact: true })).toHaveValue('extract');
  await expect(page.getByRole('combobox', { name: 'Rule combination', exact: true })).toHaveText(
    'All rules',
  );
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  const spec = parse(await page.getByLabel('Generated YAML').inputValue()).spec;
  const original = parse(stringify(workflowFixture)).spec;
  expect(spec.tasks[1]).toEqual({ ...original.tasks[1], jobRef: { name: 'chosen-job' } });
  expect(
    state.reads.map((url) => [url.searchParams.get('namespace'), url.searchParams.get('offset')]),
  ).toEqual([
    ['orbitjob', '0'],
    ['orbitjob', '20'],
  ]);
  expect(state.details).toBe(0);
});

test('a denied job list keeps manual references editable and explains cluster validation', async ({
  page,
}) => {
  const { state } = await fixture(page);
  state.status = 403;
  await connect(page);
  await importWorkflow(page);
  await page.getByRole('button', { name: 'Choose an existing job', exact: true }).click();
  const picker = page.getByRole('region', { name: 'Jobs in workflow namespace' });
  await expect(picker.getByRole('alert')).toContainText('Permission required');
  await expect(picker).toContainText('Browsing and saving require separate permissions');
  await page.getByLabel('Referenced Job name', { exact: true }).fill('manually-known-job');
  await expect(page.getByLabel('Referenced Job name', { exact: true })).toHaveValue(
    'manually-known-job',
  );
  await expect(page.getByRole('button', { name: 'Check draft', exact: true })).toBeEnabled();
});

test('search has an explicit no-match state and namespace changes cannot show an older result', async ({
  page,
}) => {
  const { state } = await fixture(page);
  await connect(page);
  await importWorkflow(page);
  await page.getByRole('button', { name: 'Choose an existing job', exact: true }).click();
  let picker = page.getByRole('region', { name: 'Jobs in workflow namespace' });
  await picker.getByLabel('Search jobs in workflow namespace').fill('missing');
  await expect(picker).toContainText('No matching jobs on this page');
  await expect(picker.getByRole('button', { name: /^Use job/ })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByLabel('Namespace', { exact: true }).fill('other-namespace');
  await page.getByRole('tab', { name: 'Tasks', exact: true }).click();
  await page.getByRole('button', { name: 'Choose an existing job', exact: true }).click();
  picker = page.getByRole('region', { name: 'Jobs in workflow namespace' });
  await expect(picker).toContainText('Jobs in other-namespace');
  await expect(picker.getByLabel('Search jobs in workflow namespace')).toHaveValue('');
  await expect(
    picker.getByRole('button', { name: 'Use job first-job', exact: true }),
  ).toBeVisible();
  expect(state.reads.at(-1)?.searchParams.get('namespace')).toBe('other-namespace');
});

test('the definition form reuses namespace job choices and preserves the rest of its spec', async ({
  page,
}) => {
  const { state } = await fixture(page);
  await connect(page, '/console/definitions/workflows');
  await page.getByRole('button', { name: 'Choose an existing job', exact: true }).first().click();
  const picker = page.getByRole('region', { name: 'Jobs in workflow namespace' });
  await picker.getByRole('button', { name: 'Use job first-job', exact: true }).click();
  await expect(page.getByLabel('Task 1 job', { exact: true })).toHaveValue('first-job');
  await expect(page.getByLabel('Task 2 dependencies', { exact: true })).toHaveValue('extract');
  expect(state.reads[0].searchParams.get('namespace')).toBe('orbitjob');
  expect(state.details).toBe(0);
});

test('job authoring explains shared Forbid admission and retains existing Replace', async ({
  page,
}) => {
  await fixture(page);
  await connect(page, '/console/manifests/job');
  await page.getByText('Run policies', { exact: true }).click();
  await page.getByRole('combobox', { name: 'Concurrency policy', exact: true }).click();
  await expect(page.getByRole('option', { name: 'Replace', exact: true })).toHaveCount(0);
  await page.getByRole('option', { name: 'Prevent overlapping runs', exact: true }).click();
  await expect(
    page.getByText('Scheduled, manual and workflow-task runs cannot overlap', {
      exact: false,
    }),
  ).toContainText('Accepted manual starts wait');
  const legacy = {
    apiVersion: 'workloads.orbitjob.io/v1alpha1',
    kind: 'ScheduledJob',
    metadata: { name: 'legacy-job', namespace: 'orbitjob' },
    spec: {
      schedule: '',
      concurrencyPolicy: 'Replace',
      timeoutSeconds: 120,
      jobTemplate: { image: 'busybox:1.37', command: ['sh'], args: ['-c', 'echo legacy'] },
    },
  };
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByLabel('Generated YAML').fill(stringify(legacy));
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await page.getByText('Run policies', { exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Concurrency policy', exact: true })).toHaveText(
    'Replace',
  );
  await expect(
    page.getByText('Replace requests cancellation for scheduled starts', {
      exact: false,
    }),
  ).toBeVisible();
  await page.getByRole('combobox', { name: 'Concurrency policy', exact: true }).click();
  await expect(page.getByRole('option', { name: 'Replace', exact: true })).toBeDisabled();
});

test('a job-browser session rejection clears the protected authoring draft before sign-in', async ({
  page,
}) => {
  const { state } = await fixture(page);
  await connect(page);
  await importWorkflow(page);
  state.status = 401;
  await page.getByRole('button', { name: 'Choose an existing job', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Your session has ended.');
  await expect(
    page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel('Referenced Job name', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Review and save', exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Generated YAML')).toHaveCount(0);
  state.status = 200;
  await signIn(page);
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByLabel('Generated YAML')).not.toHaveValue(/billing-extract|billing-close/);
});
