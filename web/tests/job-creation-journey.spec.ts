import { expect, fixtureIdentity, signIn, test, type Page } from './support';
import { demoClient } from '../src/api/demo';
import type { DefinitionInput } from '../src/api/definitions';

test.use({
  baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174',
  viewport: { width: 1440, height: 1000 },
});

async function setup(page: Page) {
  const demo = demoClient();
  const source = await demo.job('17');
  const recorded = await demo.run('1041');
  demo.dispose();
  const state = {
    created: false,
    observedGeneration: 1,
    acceptedGeneration: 1,
    rejectCreateCollision: false,
    checks: [] as DefinitionInput[],
    saves: [] as DefinitionInput[],
    triggers: [] as string[],
    reads: [] as URL[],
  };
  const job = {
    ...source,
    name: 'hello-orbitjob',
    tenant_id: fixtureIdentity.tenant_id,
    schedule: '',
    schedule_summary: 'Manual only',
    suspend: false,
    job_template: {
      image: 'busybox:1.37.0',
      command: ['printf'],
      args: ['Hello from OrbitJob!\\n'],
      backoff_limit: 0,
    },
    retry_max_attempts: 1,
    timeout_seconds: 120,
  };
  const run = {
    ...recorded,
    tenant_id: fixtureIdentity.tenant_id,
    source_uid: job.source_uid,
    source_name: job.name,
    source_namespace: job.namespace,
    source_kind: 'ScheduledJob',
    revision_id: job.id,
    trigger: 'Manual',
    phase: 'Succeeded',
    attempt: 1,
    max_attempts: 1,
    attempts: [{ ...recorded.attempts[0], phase: 'Succeeded' }],
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
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (path.startsWith('/api/v1/auth/')) return route.fallback();
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: { ...fixtureIdentity, workload_namespaces: [job.namespace] },
      });
    if (request.method() === 'GET') state.reads.push(url);
    if (path === `/api/v1/definitions/jobs/${job.namespace}/${job.name}`) {
      const input = request.postDataJSON() as DefinitionInput;
      (input.dry_run ? state.checks : state.saves).push(input);
      if (state.rejectCreateCollision && state.created && input.expected_uid === '')
        return route.fulfill({
          status: 409,
          json: {
            error: {
              code: 'CONFLICT',
              message: 'A Job already uses this name. Choose a new name.',
            },
          },
        });
      if (!input.dry_run) state.created = true;
      return route.fulfill({
        json: {
          kind: 'jobs',
          namespace: job.namespace,
          name: job.name,
          uid: job.source_uid,
          generation: state.acceptedGeneration,
          resource_version: '8',
          spec_json: input.spec_json,
          manifest_digest: 'checked-hello-digest',
          last_request_id: input.dry_run ? '' : input.request_id,
          last_manifest_digest: input.dry_run ? '' : 'checked-hello-digest',
          request_id: input.request_id,
          state: input.dry_run ? 'validated' : 'accepted',
        },
      });
    }
    if (path === '/api/v1/jobs')
      return route.fulfill({
        json: {
          items: state.created ? [{ ...job, generation: state.observedGeneration }] : [],
        },
      });
    if (path === `/api/v1/jobs/${job.id}`) return route.fulfill({ json: job });
    if (path === `/api/v1/jobs/${job.id}/trigger`) {
      state.triggers.push(request.headers()['x-orbitjob-idempotency-key']);
      return route.fulfill({
        json: {
          namespace: job.namespace,
          name: 'hello-manual-request',
          occurrence_key: run.occurrence_key,
          trigger: 'Manual',
          phase: 'Pending',
          created: true,
        },
      });
    }
    if (path === '/api/v1/instances')
      return route.fulfill({ json: { items: state.triggers.length ? [run] : [] } });
    if (path === `/api/v1/instances/${run.id}`) return route.fulfill({ json: run });
    return route.fulfill({
      status: 403,
      json: { error: { code: 'FORBIDDEN', message: 'This fixture provides no execution logs.' } },
    });
  });
  await page.goto('/console/manifests/job');
  await signIn(page);
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
  return { state, job, run };
}

async function createHello(page: Page) {
  await page.getByRole('button', { name: 'Use hello example', exact: true }).click();
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
}

test('a real-mode hello draft reaches a confirmed save and its first recorded run without a detail-page detour', async ({
  page,
}) => {
  const { state, job, run } = await setup(page);
  await expect(page.getByRole('combobox', { name: 'Namespace', exact: true })).toHaveValue(
    job.namespace,
  );
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('');
  await expect(page.getByRole('textbox', { name: 'CPU request', exact: true })).toBeHidden();
  await expect(page.getByRole('button', { name: `Run once ${job.name}`, exact: true })).toHaveCount(
    0,
  );
  await createHello(page);
  await expect(page.getByText('Saved version is ready', { exact: true })).toBeVisible();
  expect(state.checks).toHaveLength(1);
  expect(state.saves).toHaveLength(1);
  expect(state.saves[0].manifest_digest).toBe('checked-hello-digest');
  expect(state.saves[0].spec_json).toBe(state.checks[0].spec_json);
  expect(JSON.parse(state.saves[0].spec_json)).toMatchObject({
    schedule: '',
    suspend: false,
    jobTemplate: {
      image: job.job_template.image,
      command: ['printf'],
      args: job.job_template.args,
    },
  });
  expect(state.triggers).toEqual([]);
  await page.getByRole('button', { name: `Run once ${job.name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm run', exact: true }).click();
  const receipt = page.getByRole('region', {
    name: `Trigger receipt for ${job.name}`,
    exact: true,
  });
  await expect(receipt.getByRole('link', { name: 'View run', exact: true })).toHaveAttribute(
    'href',
    `/console/runs/${run.id}`,
  );
  expect(state.triggers).toHaveLength(1);
  expect(state.triggers[0]).toBeTruthy();
  const correlated = state.reads.find((url) => url.searchParams.has('occurrence_key'))!;
  expect(correlated.searchParams.get('source_uid')).toBe(job.source_uid);
  expect(correlated.searchParams.get('occurrence_key')).toBe(run.occurrence_key);
  await receipt.getByRole('link', { name: 'View run', exact: true }).click();
  await expect(page.getByRole('heading', { name: `Run #${run.id}`, exact: true })).toBeVisible();
  await expect(page.locator('.run-meta').getByText('Succeeded', { exact: true })).toBeVisible();
  expect(state.triggers).toHaveLength(1);
});

test('first-run controls wait for the exact saved generation and stay absent for a newer active generation', async ({
  page,
}) => {
  const { state, job } = await setup(page);
  state.acceptedGeneration = 2;
  await createHello(page);
  await expect(page.getByText('Saved. Getting ready', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: `Run once ${job.name}`, exact: true })).toHaveCount(
    0,
  );
  state.observedGeneration = 3;
  await page.getByRole('button', { name: 'Check readiness again', exact: true }).click();
  await expect(page.getByText('A newer version is active', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: `Run once ${job.name}`, exact: true })).toHaveCount(
    0,
  );
  expect(state.triggers).toEqual([]);
});

test('choosing a new starting point never inherits a previously saved target update identity', async ({
  page,
}) => {
  const { state } = await setup(page);
  await createHello(page);
  await expect(page.getByText('Saved version is ready', { exact: true })).toBeVisible();
  state.rejectCreateCollision = true;
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByRole('button', { name: 'Use hello example', exact: true }).click();
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace draft', exact: true })
    .click();
  await expect(page.getByRole('region', { name: 'How this Job will run' })).toContainText(
    'Saving creates this Job definition.',
  );
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Choose a new name');
  expect(state.checks).toHaveLength(2);
  expect(state.checks[1].expected_uid).toBe('');
  expect(state.checks[1].expected_resource_version).toBe('');
  expect(state.saves).toHaveLength(1);
  expect(state.triggers).toEqual([]);
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeDisabled();
});

test('draft behavior follows literal input and makes missing content and policy controls actionable', async ({
  page,
}) => {
  const { state } = await setup(page);
  const preview = page.getByRole('region', { name: 'How this Job will run', exact: true });
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('');
  await preview.getByRole('button', { name: 'Choose execution content', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toBeFocused();
  await page.getByRole('button', { name: 'Use hello example', exact: true }).click();
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace draft', exact: true })
    .click();
  await page.getByRole('textbox', { name: 'Command', exact: true }).fill('echo hello');
  await expect(preview.getByLabel('Configured command entries')).toHaveText('"echo"');
  await expect(preview.getByLabel('Configured argument entries')).toHaveText('"hello"');
  await preview.getByText('Resources for each attempt', { exact: true }).click();
  await preview.getByRole('button', { name: 'Edit resources', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'CPU request', exact: true })).toBeFocused();
  await page.getByRole('textbox', { name: 'CPU request', exact: true }).fill('125m');
  await expect(preview.getByText('125m', { exact: true })).toBeVisible();
  await preview.getByRole('button', { name: 'Edit run policies', exact: true }).click();
  await page.getByRole('textbox', { name: 'Maximum attempts', exact: true }).fill('3');
  await expect(preview).toContainText('Up to 3 platform attempts, including the first');
  await expect(preview).toContainText('requires this task to be marked safe to repeat');
  expect(state.checks).toEqual([]);
  expect(state.saves).toEqual([]);
  expect(state.triggers).toEqual([]);
  await page.screenshot({ path: 'test-results/job-live-behavior-1440.png', fullPage: true });
});

test('an incomplete or non-object advanced draft keeps preview repair actions connected to its JSON editor', async ({
  page,
}) => {
  await setup(page);
  await page.goto('/console/definitions/jobs');
  await expect(page.getByRole('heading', { name: 'Job definition', exact: true })).toBeVisible();
  const raw = page.locator('details.definition-raw');
  const input = raw.getByRole('textbox', { name: 'Definition spec JSON', exact: true });
  const preview = page.getByRole('region', { name: 'How this Job will run', exact: true });
  for (const value of ['{}', '[]', 'null']) {
    if (!(await raw.evaluate((node) => (node as HTMLDetailsElement).open))) {
      await raw.locator('summary').click();
    }
    await input.fill(value);
    await raw.locator('summary').click();
    await preview
      .getByRole('button', {
        name: value === '{}' ? 'Choose execution content' : 'Finish the Job configuration',
        exact: true,
      })
      .click();
    await expect(input).toBeFocused();
  }
});

test.describe('320px dark Job creation', () => {
  test.use({
    viewport: { width: 320, height: 844 },
    colorScheme: 'dark',
    isMobile: true,
    hasTouch: true,
  });
  test('keeps starting points and preview navigation usable without horizontal overflow', async ({
    page,
  }) => {
    await setup(page);
    await page.getByRole('button', { name: 'Use hello example', exact: true }).click();
    await page.getByRole('link', { name: 'Preview how this job will run', exact: true }).click();
    await expect(
      page.getByRole('heading', { name: 'How this Job will run', exact: true }),
    ).toBeInViewport();
    await page.getByRole('button', { name: 'Edit execution content', exact: true }).click();
    await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toBeFocused();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({ path: 'test-results/job-live-behavior-320-dark.png', fullPage: true });
  });
});
