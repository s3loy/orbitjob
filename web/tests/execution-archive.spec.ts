import { signIn } from './support';
import { expect, test } from './support';
import { demoClient } from '../src/api/demo';
import { readFile } from 'node:fs/promises';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

test('log searches preserve Unicode characters up to the API boundary', async ({ page }) => {
  const demo = demoClient();
  const run = { ...(await demo.run('1042')), id: 77, phase: 'Succeeded' };
  demo.dispose();
  const searches: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/v1/auth/') || url.pathname === '/api/v1/identity')
      return route.fallback();
    if (url.pathname === '/api/v1/instances/77') return route.fulfill({ json: run });
    if (url.pathname.endsWith('/logs/archive')) {
      const search = url.searchParams.get('search') || '';
      if (search) searches.push(search);
      return route.fulfill({
        json: {
          state: 'sealed',
          reason: 'Retained evidence',
          outcome: 'Succeeded',
          records: [],
          gaps: [],
          next: 0,
          has_more: false,
          output_bytes: 0,
          retained_bytes: 0,
          limit_bytes: 1024,
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/runs/77');
  await signIn(page);
  const input = page.getByRole('textbox', { name: 'Search execution logs', exact: true });
  for (const [value, expected] of [
    ['\u4e2d'.repeat(86), '\u4e2d'.repeat(86)],
    ['\u4e2d'.repeat(257), '\u4e2d'.repeat(256)],
    ['\u{1f600}'.repeat(256), '\u{1f600}'.repeat(256)],
    ['\u{1f680}'.repeat(257), '\u{1f680}'.repeat(256)],
  ]) {
    searches.length = 0;
    await input.fill('');
    await expect(input).toHaveValue('');
    await input.fill(value);
    await expect(input).toHaveValue(expected);
    await expect.poll(() => searches.includes(expected)).toBe(true);
    expect(searches.every((search) => Array.from(search).length <= 256)).toBe(true);
  }
});

test('archive reconnect reloads cleared history and keeps both retry choices', async ({ page }) => {
  const demo = demoClient();
  const run = { ...(await demo.run('1042')), id: 77, phase: 'Succeeded' };
  demo.dispose();
  let failNext = false;
  let recoveredFromZero = false;
  let hasFailed = false;
  const records = [
    {
      id: 'first',
      sequence: 1,
      time: '2026-10-26T00:00:00Z',
      observed_at: '2026-10-26T00:00:00Z',
      kind: 'stderr',
      stage: 'running',
      attempt: 1,
      pod_uid: 'pod-first',
      container: 'task',
      restart: 0,
      text: 'first attempt failed: exit 7\n',
    },
    {
      id: 'retry',
      sequence: 2,
      time: '2026-10-26T00:00:02Z',
      observed_at: '2026-10-26T00:00:02Z',
      kind: 'stdout',
      stage: 'running',
      attempt: 2,
      pod_uid: 'pod-retry',
      container: 'task',
      restart: 0,
      text: 'retry succeeded: result 42\n',
    },
  ];
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    const url = new URL(route.request().url());
    if (url.pathname === '/api/v1/identity') return route.fallback();
    if (url.pathname === '/api/v1/instances/77') return route.fulfill({ json: run });
    if (url.pathname.endsWith('/logs/archive')) {
      if (failNext) {
        failNext = false;
        hasFailed = true;
        return route.fulfill({
          status: 503,
          json: { error: { code: 'SERVICE_UNAVAILABLE', message: 'Archive restart in progress' } },
        });
      }
      const after = Number(url.searchParams.get('after') || 0);
      if (hasFailed && after === 0) recoveredFromZero = true;
      const attempt = Number(url.searchParams.get('attempt') || 0);
      return route.fulfill({
        json: {
          state: 'sealed',
          reason: 'Retained after Pod removal',
          outcome: 'Succeeded',
          records: records.filter((r) => r.sequence > after && (!attempt || r.attempt === attempt)),
          gaps: [],
          next: 2,
          has_more: false,
          output_bytes: 60,
          retained_bytes: 60,
          limit_bytes: 33554432,
          protected_until: '2026-11-10T00:00:02Z',
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.clock.install();
  await page.goto('/console/runs/77');
  await signIn(page, 'otj_mock_archive_not_real');
  const logs = page.getByRole('region', { name: 'Execution logs', exact: true });
  await expect(logs.getByRole('button', { name: 'Copy loaded output', exact: true })).toBeVisible();
  await expect(logs.getByRole('button', { name: 'Download logs', exact: true })).toBeVisible();
  await expect(logs.getByText('Collection finished', { exact: true })).toBeVisible();
  await expect(logs.getByText('Log actions', { exact: true })).toBeVisible();
  await expect(
    logs.getByRole('button', { name: 'Delete retained logs', exact: true }),
  ).toBeHidden();
  await logs.getByText('Log actions', { exact: true }).click();
  await expect(
    logs.getByRole('button', { name: 'Delete retained logs', exact: true }),
  ).toBeVisible();
  await logs.getByText('Log actions', { exact: true }).click();
  await expect(logs.getByText('first attempt failed: exit 7', { exact: true })).toBeVisible();
  await expect(logs.getByText('retry succeeded: result 42', { exact: true })).toBeVisible();
  failNext = true;
  await page.clock.runFor(1500);
  await expect(logs.getByRole('alert')).toContainText('Archive restart in progress');
  await expect(logs).toContainText(
    'Waiting for the execution archive. Reconnecting automatically…',
  );
  await page.clock.runFor(2100);
  await expect(logs.getByText('first attempt failed: exit 7', { exact: true })).toBeVisible();
  await expect(logs.getByText('retry succeeded: result 42', { exact: true })).toBeVisible();
  expect(recoveredFromZero).toBe(true);
  await expect(
    logs.getByText('Waiting for the execution archive. Reconnecting automatically…', {
      exact: true,
    }),
  ).toHaveCount(0);
  await logs.getByLabel('Execution log attempt', { exact: true }).selectOption('1');
  await expect(logs.getByText('first attempt failed: exit 7', { exact: true })).toBeVisible();
  await expect(logs.getByText('retry succeeded: result 42', { exact: true })).toHaveCount(0);
  await expect(logs.getByLabel('Execution log attempt').locator('option[value="2"]')).toHaveCount(
    1,
  );
  await logs.getByLabel('Execution log attempt', { exact: true }).selectOption('2');
  await expect(logs.getByText('retry succeeded: result 42', { exact: true })).toBeVisible();
});

test('log downloads retain collection changes and gaps reported on later pages', async ({
  page,
}) => {
  const demo = demoClient();
  const run = { ...(await demo.run('1042')), id: 77, phase: 'Succeeded' };
  demo.dispose();
  const row = (sequence: number, text: string) => ({
    id: `row-${sequence}`,
    sequence,
    time: '2026-11-17T00:00:00Z',
    observed_at: '2026-11-17T00:00:00Z',
    kind: 'stdout',
    stage: 'running',
    attempt: 1,
    pod_uid: 'pod-first',
    container: 'task',
    restart: 0,
    text,
  });
  const exportCursors: number[] = [];
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/v1/auth/') || url.pathname === '/api/v1/identity')
      return route.fallback();
    if (url.pathname === '/api/v1/instances/77') return route.fulfill({ json: run });
    if (url.pathname.endsWith('/logs/archive')) {
      const after = Number(url.searchParams.get('after') || 0);
      const exporting = url.searchParams.get('limit') === '1000';
      if (exporting) exportCursors.push(after);
      const later = exporting && after > 0;
      return route.fulfill({
        json: {
          state: later ? 'partial' : exporting ? 'collecting' : 'sealed',
          reason: later ? 'window rotated' : 'initial retained window',
          outcome: 'Succeeded',
          records: later ? [row(4, 'remaining output\n')] : [row(1, 'first output\n')],
          gaps: [
            { from: 0, through: 0, reason: 'exact missing source range is unavailable' },
            ...(later ? [{ from: 2, through: 3, reason: 'ROTATION_GAP' }] : []),
          ],
          next: later ? 4 : 1,
          has_more: exporting && !later,
          output_bytes: 40,
          retained_bytes: 40,
          limit_bytes: 1024,
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/runs/77');
  await signIn(page);
  const logs = page.getByRole('region', { name: 'Execution logs', exact: true });
  await expect(logs.getByRole('button', { name: 'Download logs', exact: true })).toBeEnabled();
  await expect(logs).toContainText('Output gap (range unknown)');
  const downloading = page.waitForEvent('download');
  await logs.getByRole('button', { name: 'Download logs', exact: true }).click();
  const download = await downloading;
  const text = await readFile((await download.path())!, 'utf8');
  expect(exportCursors).toEqual([0, 1]);
  expect(text).toContain('# Collection: collecting — initial retained window');
  expect(text).toContain('# Collection: partial — window rotated');
  expect(text).toContain('# OUTPUT GAP 2-3: ROTATION_GAP');
  expect(
    text.match(/# OUTPUT GAP \(range unknown\): exact missing source range is unavailable/g),
  ).toHaveLength(1);
  expect(text).not.toContain('# OUTPUT GAP 0-0:');
  expect(text).toContain('remaining output\n');
  expect(text).toContain('# END OF RETAINED LOG EXPORT');
  await expect(logs).toContainText(
    'Reported collection changes and omitted ranges are included in the file.',
  );
});

test('log downloads report an archive lost during pagination as incomplete', async ({ page }) => {
  const demo = demoClient();
  const run = { ...(await demo.run('1042')), id: 77, phase: 'Succeeded' };
  demo.dispose();
  const exportCursors: number[] = [];
  await page.route('**/api/v1/**', (route) => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith('/api/v1/auth/') || url.pathname === '/api/v1/identity')
      return route.fallback();
    if (url.pathname === '/api/v1/instances/77') return route.fulfill({ json: run });
    if (url.pathname.endsWith('/logs/archive')) {
      const after = Number(url.searchParams.get('after') || 0);
      const exporting = url.searchParams.get('limit') === '1000';
      if (exporting) exportCursors.push(after);
      const lost = exporting && after > 0;
      return route.fulfill({
        json: {
          state: lost ? 'expired' : 'sealed',
          reason: lost ? 'retention elapsed during export' : 'retained evidence',
          outcome: 'Succeeded',
          records: lost
            ? []
            : [
                {
                  id: 'row-1',
                  sequence: 1,
                  time: '2026-11-17T00:00:00Z',
                  observed_at: '2026-11-17T00:00:00Z',
                  kind: 'stdout',
                  stage: 'running',
                  attempt: 1,
                  pod_uid: 'pod-first',
                  container: 'task',
                  restart: 0,
                  text: 'retained before expiration\n',
                },
              ],
          gaps: lost ? [{ from: 2, through: 9, reason: 'retention expired' }] : [],
          next: 1,
          has_more: exporting && !lost,
          output_bytes: 40,
          retained_bytes: lost ? 0 : 40,
          limit_bytes: 1024,
        },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/runs/77');
  await signIn(page);
  const logs = page.getByRole('region', { name: 'Execution logs', exact: true });
  await expect(logs.getByRole('button', { name: 'Download logs', exact: true })).toBeEnabled();
  const downloading = page.waitForEvent('download');
  await logs.getByRole('button', { name: 'Download logs', exact: true }).click();
  const download = await downloading;
  const text = await readFile((await download.path())!, 'utf8');
  expect(exportCursors).toEqual([0, 1]);
  expect(text).toContain('retained before expiration\n');
  expect(text).toContain('# Collection: expired — retention elapsed during export');
  expect(text).toContain('# OUTPUT GAP 2-9: retention expired');
  expect(text).toContain('# EXPORT INCOMPLETE: logs expired');
  expect(text).not.toContain('# END OF RETAINED LOG EXPORT');
  await expect(logs).toContainText('Download incomplete: logs expired.');
});
