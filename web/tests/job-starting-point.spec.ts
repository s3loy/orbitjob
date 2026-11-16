import { parse } from 'yaml';
import { demoClient } from '../src/api/demo';
import type { Definition } from '../src/api/definitions';
import { expect, fixtureIdentity, signIn, test, type Page, type Route } from './support';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function fixture(page: Page) {
  const demo = demoClient();
  const source = { ...(await demo.job('17')), tenant_id: fixtureIdentity.tenant_id };
  const replacement = await demo.job('18');
  const original = await demo.definition('jobs', source.namespace, source.name);
  demo.dispose();
  const spec = {
    executionPolicyVersion: 1,
    retrySafe: true,
    schedule: '30 5 * * *',
    suspend: true,
    concurrencyPolicy: 'Replace',
    misfirePolicy: 'CatchUpBounded',
    timeoutSeconds: 321,
    history: { successfulRuns: 0, failedRuns: 7 },
    retryPolicy: { maxAttempts: 3 },
    jobTemplate: {
      image: source.job_template.image,
      command: ['/app/export', '--command-token'],
      args: ['', 'two words', '$HOME', 'line1\nline2'],
      backoffLimit: 2,
      resources: {
        requests: { cpu: '125m' },
        limits: { memory: '256Mi', 'ephemeral-storage': '1Gi' },
      },
    },
  };
  const definition: Definition = { ...original, spec_json: JSON.stringify(spec) };
  let releaseDefinition!: () => void;
  const delayed = new Promise<void>((resolve) => {
    releaseDefinition = resolve;
  });
  const state = {
    definition,
    listStatus: 200,
    definitionStatus: 200,
    delayDefinition: false,
    delayedRoute: null as Route | null,
    delayedFinished: false,
    delayedAborted: false,
    identities: 0,
    lists: [] as URL[],
    definitions: [] as URL[],
    details: 0,
    writes: [] as string[],
    sourceRequests: [] as {
      url: URL;
      method: string;
      body: string | null;
      authorization?: string;
    }[],
  };
  const definitionPath = `/api/v1/definitions/jobs/${source.namespace}/${source.name}`;
  page.on('requestfailed', (request) => {
    if (request === state.delayedRoute?.request()) state.delayedAborted = true;
  });
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname.startsWith('/api/v1/auth/')) return route.fallback();
    if (request.method() !== 'GET') {
      state.writes.push(`${request.method()} ${url.pathname}`);
      return route.fulfill({
        status: 405,
        json: {
          error: { code: 'METHOD_NOT_ALLOWED', message: 'This fixture only reads sources.' },
        },
      });
    }
    if (url.pathname === '/api/v1/identity') {
      state.identities++;
      return route.fulfill({
        json: {
          ...fixtureIdentity,
          workload_namespaces: ['target-space', 'other-space'],
        },
      });
    }
    if (url.pathname === '/api/v1/jobs' || url.pathname === definitionPath)
      state.sourceRequests.push({
        url,
        method: request.method(),
        body: request.postData(),
        authorization: request.headers()['authorization'],
      });
    if (url.pathname === '/api/v1/jobs') {
      state.lists.push(url);
      if (state.listStatus !== 200)
        return route.fulfill({
          status: state.listStatus,
          json: { error: { code: 'FORBIDDEN', message: 'Job listing is not readable.' } },
        });
      const search = url.searchParams.get('search') || '';
      return route.fulfill({
        json: {
          items: source.name.includes(search) ? [{ ...source, source_kind: 'ScheduledJob' }] : [],
        },
      });
    }
    if (url.pathname === `/api/v1/jobs/${source.id}`) {
      state.details++;
      return route.fulfill({
        json: {
          ...source,
          job_template: {
            image: 'busybox:1.37.0',
            command: ['false'],
            args: ['projection-only'],
            backoff_limit: 0,
          },
        },
      });
    }
    if (url.pathname === definitionPath) {
      state.definitions.push(url);
      const snapshot = { ...state.definition };
      const status = state.definitionStatus;
      const held = state.delayDefinition;
      if (held) {
        state.delayDefinition = false;
        state.delayedRoute = route;
      }
      try {
        if (held) await delayed;
        if (request.failure()) return;
        return await route.fulfill(
          status === 200
            ? { json: snapshot }
            : {
                status,
                json: {
                  error: { code: 'FORBIDDEN', message: 'Source definition is not readable.' },
                },
              },
        );
      } finally {
        if (held) state.delayedFinished = true;
      }
    }
    return route.fulfill({
      json: url.pathname === '/api/v1/instances/summary' ? { total: 0, phases: {} } : { items: [] },
    });
  });
  return { state, source, replacement, spec, definitionPath, releaseDefinition };
}

async function connect(page: Page) {
  await page.goto('/console/manifests/job');
  await signIn(page, 'job_starting_point_fixture_password');
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
}

async function draft(page: Page) {
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  const yaml = await page
    .getByRole('textbox', { name: 'Generated YAML', exact: true })
    .inputValue();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  return { yaml, manifest: parse(yaml) };
}

async function ownDraft(page: Page, name = 'keep-my-draft') {
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('target-space');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await page
    .getByRole('textbox', { name: 'Container image', exact: true })
    .fill('private.example.com/my-image:v2');
}

async function chooseSource(page: Page, source: { namespace: string; name: string }) {
  await page.getByRole('button', { name: 'Reuse a Job', exact: true }).click();
  await page
    .getByRole('button', { name: `Reuse Job ${source.namespace}/${source.name}`, exact: true })
    .click();
}

function expectReadBoundary(state: Awaited<ReturnType<typeof fixture>>['state']) {
  expect(state.writes).toEqual([]);
  expect(state.details).toBe(0);
  for (const request of state.sourceRequests) {
    expect(request.method).toBe('GET');
    expect(request.body).toBeNull();
    expect(request.authorization).toBeUndefined();
    expect(request.url.searchParams.get('tenant_id')).toBe(fixtureIdentity.tenant_id);
    expect(request.url.searchParams.has('actor')).toBe(false);
  }
}

test('opening task templates invalidates a pending reuse selection', async ({ page }) => {
  const { state, source, releaseDefinition } = await fixture(page);
  state.delayDefinition = true;
  try {
    await connect(page);
    await ownDraft(page);
    await chooseSource(page, source);
    await expect.poll(() => state.definitions.length).toBe(1);
    await page.getByRole('button', { name: 'Choose a task template', exact: true }).click();
    const dialog = page.getByRole('dialog', { name: 'Choose a task template', exact: true });
    await expect(dialog).toBeVisible();
    releaseDefinition();
    await expect.poll(() => state.delayedFinished).toBe(true);
    await expect(dialog).toBeVisible();
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    await expect(page.getByLabel('Name', { exact: true })).toHaveValue('keep-my-draft');
    expectReadBoundary(state);
  } finally {
    releaseDefinition();
  }
});

test('reuse reads the authoritative definition only after selection and keeps the target execution namespace', async ({
  page,
}) => {
  const { state, source, spec } = await fixture(page);
  await connect(page);
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('target-space');
  expect(state.lists).toHaveLength(0);
  expect(state.definitions).toHaveLength(0);
  await page.getByRole('button', { name: 'Reuse a Job', exact: true }).click();
  const select = page.getByRole('button', {
    name: `Reuse Job ${source.namespace}/${source.name}`,
    exact: true,
  });
  await expect(select).toBeVisible();
  expect(state.lists).toHaveLength(1);
  expect(state.definitions).toHaveLength(0);
  await page.getByRole('searchbox', { name: 'Search Jobs', exact: true }).fill(source.name);
  await expect.poll(() => state.lists.at(-1)?.searchParams.get('search')).toBe(source.name);
  await expect(select).toBeEnabled();
  expect(state.definitions).toHaveLength(0);
  await select.click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    `${source.name}-copy`,
  );
  await expect(page.getByRole('combobox', { name: 'Namespace', exact: true })).toHaveValue(
    'target-space',
  );
  await expect(page.getByRole('combobox', { name: 'Trigger mode', exact: true })).toHaveText(
    'Manual only',
  );
  await expect(
    page.getByRole('checkbox', { name: 'Suspend scheduled and manual starts', exact: true }),
  ).not.toBeChecked();
  await page.locator('details.execution-command-advanced > summary').click();
  await expect(page.getByRole('textbox', { name: 'Command token 1', exact: true })).toHaveValue(
    spec.jobTemplate.command[0],
  );
  for (const [index, argument] of spec.jobTemplate.args.entries())
    await expect(
      page.getByRole('textbox', { name: `Argument ${index + 1}`, exact: true }),
    ).toHaveValue(argument);
  const generated = await draft(page);
  expect(generated.manifest.metadata).toEqual({
    name: `${source.name}-copy`,
    namespace: 'target-space',
  });
  expect(generated.manifest.spec).toEqual({ ...spec, schedule: '', suspend: false });
  expect(generated.yaml).not.toContain(source.source_uid);
  expect(generated.yaml).not.toContain(source.actor);
  expect(generated.yaml).not.toContain(state.definition.manifest_digest);
  expect(state.definitions).toHaveLength(1);
  expectReadBoundary(state);
});

test('existing input is retained until one explicit replacement confirmation and custom image only moves focus', async ({
  page,
}) => {
  const { state, source, spec } = await fixture(page);
  await connect(page);
  await ownDraft(page);
  const before = await draft(page);
  await chooseSource(page, source);
  const confirmation = page.getByRole('alertdialog', {
    name: 'Replace this Job draft?',
    exact: true,
  });
  await expect(confirmation).toBeVisible();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('keep-my-draft');
  await confirmation.getByRole('button', { name: 'Keep current draft', exact: true }).click();
  expect(await draft(page)).toEqual(before);
  await page.getByRole('button', { name: 'Configure my own image', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toBeFocused();
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  expect(state.definitions).toHaveLength(1);
  expect(await draft(page)).toEqual(before);
  await chooseSource(page, source);
  await expect(confirmation).toBeVisible();
  await confirmation.getByRole('button', { name: 'Replace draft', exact: true }).click();
  await expect(confirmation).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    `${source.name}-copy`,
  );
  const after = await draft(page);
  expect(after.manifest.metadata.namespace).toBe('target-space');
  expect(after.manifest.spec).toEqual({ ...spec, schedule: '', suspend: false });
  expect(state.definitions).toHaveLength(2);
  expectReadBoundary(state);
});

for (const failure of ['forbidden', 'replaced-uid'] as const)
  test(`a ${failure} source read preserves the entire existing draft without writing`, async ({
    page,
  }) => {
    const { state, source, replacement } = await fixture(page);
    if (failure === 'forbidden') state.definitionStatus = 403;
    else state.definition.uid = replacement.source_uid;
    await connect(page);
    await ownDraft(page);
    const before = await draft(page);
    await chooseSource(page, source);
    await expect(page.getByRole('alert')).toContainText(
      failure === 'forbidden'
        ? 'Source definition is not readable.'
        : 'This source changed since it was listed.',
    );
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    expect(await draft(page)).toEqual(before);
    expect(state.definitions).toHaveLength(1);
    expectReadBoundary(state);
  });

test('a denied source list stays distinct from no matches and leaves manual authoring available', async ({
  page,
}) => {
  const { state } = await fixture(page);
  state.listStatus = 403;
  await connect(page);
  await ownDraft(page);
  const before = await draft(page);
  await page.getByRole('button', { name: 'Reuse a Job', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Job listing is not readable.');
  await expect(page.getByText('No reusable Jobs on this page.', { exact: false })).toHaveCount(0);
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toBeEnabled();
  expect(await draft(page)).toEqual(before);
  expect(state.lists).toHaveLength(1);
  expect(state.definitions).toHaveLength(0);
  expectReadBoundary(state);
});

test('a late definition cannot overwrite the draft after its target namespace changes', async ({
  page,
}) => {
  const { state, source, definitionPath, releaseDefinition } = await fixture(page);
  state.delayDefinition = true;
  try {
    await connect(page);
    await ownDraft(page);
    const before = await draft(page);
    await chooseSource(page, source);
    await expect.poll(() => state.definitions.length).toBe(1);
    await page
      .getByRole('combobox', { name: 'Namespace', exact: true })
      .selectOption('other-space');
    const response = page.waitForResponse(
      (result) => new URL(result.url()).pathname === definitionPath,
    );
    releaseDefinition();
    await (await response).finished();
    await expect.poll(() => state.delayedFinished).toBe(true);
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    const after = await draft(page);
    expect(after.manifest).toEqual({
      ...before.manifest,
      metadata: { ...before.manifest.metadata, namespace: 'other-space' },
    });
    expect(after.yaml).not.toContain(source.source_uid);
    expect(state.delayedAborted).toBe(false);
    expect(state.definitions).toHaveLength(1);
    expectReadBoundary(state);
  } finally {
    releaseDefinition();
  }
});

test('signing out aborts an old source read and a new session cannot receive its draft', async ({
  page,
}) => {
  const { state, source, releaseDefinition } = await fixture(page);
  state.delayDefinition = true;
  try {
    await connect(page);
    await ownDraft(page, 'old-session-draft');
    await chooseSource(page, source);
    await expect.poll(() => state.definitions.length).toBe(1);
    await page.getByRole('link', { name: 'Account', exact: true }).last().click();
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
    await expect(
      page.getByRole('heading', { name: 'Sign in to OrbitJob', exact: true }),
    ).toBeVisible();
    await expect.poll(() => state.delayedAborted).toBe(true);
    await signIn(page, 'job_starting_point_new_session_password');
    await page
      .getByRole('navigation', { name: 'Main navigation', exact: true })
      .getByRole('link', { name: 'Jobs', exact: true })
      .click();
    await page.getByRole('link', { name: 'Create a job', exact: true }).first().click();
    await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('');
    await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toHaveValue(
      'busybox:1.37.0',
    );
    await expect(page.getByRole('combobox', { name: 'Runtime', exact: true })).toHaveValue('shell');
    await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveValue(
      'echo "Hello from OrbitJob"',
    );
    await ownDraft(page, 'new-session-draft');
    const current = await draft(page);
    releaseDefinition();
    await expect.poll(() => state.delayedFinished).toBe(true);
    expect(await draft(page)).toEqual(current);
    await expect(page.getByRole('alertdialog')).toHaveCount(0);
    expect(state.identities).toBeGreaterThanOrEqual(2);
    expect(state.definitions).toHaveLength(1);
    expectReadBoundary(state);
  } finally {
    releaseDefinition();
  }
});
