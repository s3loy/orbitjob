import { signIn } from './support';
import { expect, test, type Page } from './support';
import { demoClient } from '../src/api/demo';

const origin = 'http://console.orbitjob.test';
const uuidV4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
test.use({ baseURL: origin });

test.beforeEach(async ({ page }) => {
  // Serve installed-profile assets under a real insecure HTTP origin. Only the
  // transport is forwarded; browser security APIs and isSecureContext are intact.
  await page.route(`${origin}/**`, async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/')) return route.fallback();
    if (url.pathname === '/openapi.json') return route.fulfill({ json: { paths: {} } });
    url.host = new URL(process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174').host;
    await route.fulfill({ response: await route.fetch({ url: url.toString() }) });
  });
});

test.afterEach(async ({ page }) => {
  await page.unrouteAll({ behavior: 'wait' });
});

async function connect(page: Page, path: string) {
  await page.goto(`/console${path}`);
  expect(
    await page.evaluate(() => ({
      origin: location.origin,
      secure: isSecureContext,
      uuid: typeof crypto.randomUUID,
      random: typeof crypto.getRandomValues,
      clipboard: typeof navigator.clipboard,
    })),
  ).toEqual({
    origin,
    secure: false,
    uuid: 'undefined',
    random: 'function',
    clipboard: 'undefined',
  });
  await expect(page.getByRole('button', { name: 'Explore sample data' })).toHaveCount(0);
  await signIn(page, 'otj_http_origin_fixture_not_real');
}

for (const kind of ['job', 'workflow', 'function'] as const)
  test(`${kind} execution generates a UUID on a non-localhost HTTP origin`, async ({ page }) => {
    const demo = demoClient();
    const item =
      kind === 'job'
        ? await demo.job('17')
        : kind === 'workflow'
          ? await demo.workflow('412')
          : await demo.functionDefinition('27');
    demo.dispose();
    const collection = kind === 'job' ? 'jobs' : kind === 'workflow' ? 'workflows' : 'functions';
    const path = `/api/v1/${collection}/${item.id}`;
    const verb = kind === 'function' ? 'Invoke' : 'Trigger';
    let requestID = '';
    await page.route('**/api/v1/**', async (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const url = new URL(route.request().url());
      if (url.pathname === '/api/v1/identity') return route.fallback();
      if (route.request().method() === 'POST') {
        requestID = route.request().headers()['x-orbitjob-idempotency-key'];
        return route.fulfill({
          json: {
            namespace: 'orbitjob',
            name: 'http-origin-execution',
            occurrence_key: 'http-origin-occurrence',
            trigger: kind === 'function' ? 'Function' : 'Manual',
            phase: 'Pending',
            created: true,
          },
        });
      }
      return route.fulfill({ json: url.pathname === path ? item : { items: [] } });
    });
    await connect(page, `/${collection}/${item.id}`);
    await page.getByRole('button', { name: `${verb} ${item.name}`, exact: true }).click();
    await page.getByRole('button', { name: `Confirm ${verb.toLowerCase()}`, exact: true }).click();
    await expect.poll(() => requestID).toMatch(uuidV4);
    await expect(page.getByRole('region', { name: /receipt for/ })).toContainText('accepted');
  });

test('function creation and a fresh draft work on a non-localhost HTTP origin', async ({
  page,
}) => {
  const demo = demoClient();
  const template = await demo.functionDefinition('27');
  demo.dispose();
  let requestID = '';
  let saved = template;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/functions' && route.request().method() === 'POST') {
      requestID = route.request().headers()['x-orbitjob-idempotency-key'];
      saved = {
        ...template,
        ...route.request().postDataJSON(),
        version: 1,
        ready: true,
        active_revision_version: 1,
      };
      return route.fulfill({ json: saved });
    }
    return route.fulfill({
      json: url.pathname === '/api/v1/functions/27' ? saved : { items: [] },
    });
  });
  await connect(page, '/functions/new');
  await page.getByLabel('Function name', { exact: true }).fill('http-origin-function');
  await page.getByLabel('Container image', { exact: true }).fill('busybox:1.37.0');
  await page.getByRole('button', { name: 'Review function', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Saved version is ready' })).toBeVisible();
  expect(requestID).toMatch(uuidV4);
  await page.getByRole('button', { name: 'Create another function', exact: true }).click();
  await expect(page.getByLabel('Function name', { exact: true })).toHaveValue('');
});

test('definition checks retain the request UUID when saving on HTTP', async ({ page }) => {
  const requests: { request_id: string; dry_run: boolean }[] = [];
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (!url.pathname.includes('/definitions/')) return route.fulfill({ json: { items: [] } });
    const input = route.request().postDataJSON();
    requests.push(input);
    return route.fulfill({
      json: {
        kind: 'jobs',
        name: 'http-origin-job',
        namespace: 'orbitjob',
        uid: input.dry_run ? '' : 'http-origin-source',
        resource_version: input.dry_run ? '' : '1',
        generation: 1,
        spec_json: input.spec_json,
        manifest_digest: 'http-origin-digest',
        last_request_id: input.dry_run ? '' : input.request_id,
        last_manifest_digest: input.dry_run ? '' : 'http-origin-digest',
        request_id: input.request_id,
        state: input.dry_run ? 'validated' : 'accepted',
      },
    });
  });
  await connect(page, '/manifests/job');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('http-origin-job');
  await page.getByRole('textbox', { name: 'Namespace', exact: true }).fill('orbitjob');
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37.0');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect.poll(() => requests.length).toBe(2);
  expect(requests[0].request_id).toMatch(uuidV4);
  expect(requests[1].request_id).toBe(requests[0].request_id);
  expect(requests.map((input) => input.dry_run)).toEqual([true, false]);
});

test('log copying falls back on HTTP and retains refusal feedback across polling', async ({
  page,
}, testInfo) => {
  const demo = demoClient();
  const run = { ...(await demo.run('1042')), id: 77, phase: 'Succeeded' };
  demo.dispose();
  const output = 'http-origin retained output\n';
  const refreshedOutput = 'http-origin refreshed output\n';
  let releaseRefresh!: () => void;
  const refreshAllowed = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/instances/77') return route.fulfill({ json: run });
    if (url.pathname.endsWith('/logs/archive')) {
      const subsequent = Number(url.searchParams.get('after') || 0) > 0;
      if (subsequent) await refreshAllowed;
      return route.fulfill({
        json: {
          state: 'sealed',
          reason: 'Retained after Pod removal',
          outcome: 'Succeeded',
          records: [
            {
              id: 'http-output',
              sequence: subsequent ? 2 : 1,
              time: run.created_at,
              observed_at: run.created_at,
              kind: 'stdout',
              stage: 'running',
              attempt: 1,
              pod_uid: 'http-pod',
              container: 'task',
              restart: 0,
              text: subsequent ? refreshedOutput : output,
            },
          ],
          gaps: [],
          next: subsequent ? 2 : 1,
          has_more: false,
          output_bytes: output.length + (subsequent ? refreshedOutput.length : 0),
          retained_bytes: output.length + (subsequent ? refreshedOutput.length : 0),
          limit_bytes: 33554432,
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  try {
    await connect(page, '/runs/77');
    const logs = page.getByRole('region', { name: 'Execution logs', exact: true });
    await expect(logs.getByText(output.trim(), { exact: true })).toBeVisible();
    await logs.getByRole('button', { name: 'Copy loaded output', exact: true }).click();
    await expect(logs).toContainText('Loaded log text copied.');
    await page.evaluate(() => {
      document.execCommand = () => false;
    });
    await logs.getByRole('button', { name: 'Copy loaded output', exact: true }).click();
    await expect(logs).toContainText('Select output text or download the logs.');
    releaseRefresh();
    await expect(logs.getByText(refreshedOutput.trim(), { exact: true })).toBeVisible();
    await expect(logs).toContainText('Select output text or download the logs.');
    const refusal = logs.getByText(
      'Clipboard access was refused. Select output text or download the logs.',
      { exact: true },
    );
    await expect(refusal).toBeVisible();
    await refusal.scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath('copy-refusal-after-poll.png') });
    await expect(logs.getByRole('button', { name: 'Download logs', exact: true })).toBeEnabled();
    expect(errors).toEqual([]);
  } finally {
    releaseRefresh();
  }
});
