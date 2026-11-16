import { signIn } from './support';
import { expect, test } from './support';

test('lost apply response reconciles by read without a second write or draft replacement', async ({
  page,
}) => {
  let saved: Record<string, unknown> | undefined;
  let writes = 0;
  await page.route('**/api/v1/**', async (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
    if (!route.request().url().includes('/definitions/'))
      return route.fulfill({ json: { items: [] } });
    if (route.request().method() === 'GET') {
      if (new URL(route.request().url()).pathname.includes('/operations/'))
        return route.fulfill({
          json: {
            request_id: saved?.request_id,
            kind: 'jobs',
            namespace: 'orbitjob',
            name: 'settlement-export',
            state: 'accepted',
            result: { ...saved, state: 'reconciled' },
          },
        });
      return route.fulfill({ json: saved });
    }
    const input = route.request().postDataJSON();
    const resource = {
      kind: 'jobs',
      name: 'settlement-export',
      namespace: 'orbitjob',
      uid: input.dry_run ? '' : 'owned-uid',
      resource_version: input.dry_run ? '' : '24',
      generation: 1,
      spec_json: input.spec_json,
      manifest_digest: 'checked-digest',
      last_request_id: input.dry_run ? '' : input.request_id,
      last_manifest_digest: input.dry_run ? '' : 'checked-digest',
      request_id: input.request_id,
      state: input.dry_run ? 'validated' : 'accepted',
    };
    if (input.dry_run) return route.fulfill({ json: resource });
    writes++;
    saved = resource;
    return route.abort('connectionreset');
  });
  await page.goto('/console/manifests/job');
  await signIn(page, 'otj_mock_definition_not_real');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('settlement-export');
  await page.getByRole('textbox', { name: 'Namespace', exact: true }).fill('orbitjob');
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37.0');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster' }).click();
  await page.getByRole('button', { name: 'Save definition' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByText('Save outcome unknown', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save definition' })).toBeDisabled();
  await page.getByRole('link', { name: 'Back to Jobs' }).click();
  await page.getByRole('link', { name: 'Create a job' }).click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    'settlement-export',
  );
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Check draft', exact: true })).toBeDisabled();
  // Review returns to the retained uncertain write without allowing a replacement draft.
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(page.getByText('Save outcome unknown', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeDisabled();
  const originalRequestID = saved?.request_id;
  await page.getByRole('button', { name: 'Check save outcome' }).click();
  await expect(
    page.getByText('This save was confirmed. The live definition may have changed since then.'),
  ).toBeVisible();
  expect(writes).toBe(1);
  expect(saved?.request_id).toBe(originalRequestID);
});

for (const state of ['denied', 'absent', 'truncated'] as const)
  test(`logs ${state} leaves ordinary execution evidence available`, async ({ page }) => {
    const timestamp = '2026-10-19T00:00:00Z';
    const run = {
      id: 77,
      tenant_id: 'tenant',
      source_uid: 'source',
      revision_id: 17,
      occurrence_key: 'exact-occurrence',
      trigger: 'Manual',
      actor: 'fixture',
      phase: 'Succeeded',
      attempt: 1,
      max_attempts: 1,
      created_at: timestamp,
      updated_at: timestamp,
      attempts: [
        {
          id: 78,
          run_id: 77,
          attempt_number: 1,
          phase: 'Succeeded',
          kubernetes_job_name: 'owned-job',
          kubernetes_job_uid: 'owned-job-uid',
          created_at: timestamp,
        },
      ],
    };
    let logReads = 0;
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      if (new URL(route.request().url()).pathname === '/api/v1/identity') return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path.endsWith('/logs')) {
        logReads++;
        return state === 'denied'
          ? route.fulfill({
              status: 403,
              json: { error: { code: 'FORBIDDEN', message: 'A separate log grant is required.' } },
            })
          : route.fulfill({
              json: {
                state: state === 'absent' ? 'absent' : 'ready',
                text: state === 'truncated' ? '<script>literal untrusted log</script>' : '',
                truncated: state === 'truncated',
                tail_lines: 100,
                limit_bytes: 65536,
                attempt: 1,
                container: 'task',
                reason: state === 'absent' ? 'Pod cleaned' : '',
              },
            });
      }
      return route.fulfill({ json: path === '/api/v1/instances/77' ? run : { items: [] } });
    });
    await page.goto('/console/runs/77');
    await signIn(page, 'otj_mock_logs_not_real');
    await expect(page.getByRole('heading', { name: 'Run #77' })).toBeVisible();
    expect(logReads).toBe(0);
    const containerLogs = page.locator('details.attempt-log-disclosure');
    await expect(containerLogs).not.toHaveAttribute('open');
    await containerLogs.locator('summary').click();
    await page.getByRole('button', { name: 'Read logs', exact: true }).click();
    if (state === 'denied')
      await expect(containerLogs.getByRole('alert')).toContainText('separate log grant');
    if (state === 'absent')
      await expect(page.getByText('No retained log source', { exact: false })).toBeVisible();
    if (state === 'truncated') {
      await expect(page.getByText('Output was truncated', { exact: false })).toBeVisible();
      await expect(page.getByRole('textbox', { name: 'Container log snapshot' })).toHaveValue(
        '<script>literal untrusted log</script>',
      );
    }
    await expect(page.getByRole('heading', { name: 'Run #77' })).toBeVisible();
    const evidence = page.getByRole('region', { name: 'Selected attempt evidence' });
    await evidence.getByText('Resource identity', { exact: true }).click();
    await expect(page.getByText('owned-job', { exact: true })).toBeVisible();
    expect(logReads).toBe(1);
  });

test('online create checks then confirms and retains the receipt across routes', async ({
  page,
}) => {
  const writes: string[] = [];
  page.on('request', (r) => {
    if (r.method() !== 'GET') writes.push(r.url());
  });
  await page.goto('/console/manifests/job');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page.getByRole('button', { name: 'Check draft', exact: true }).click();
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(page.getByText('hello-orbitjob', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save definition' })).toBeDisabled();
  await page.getByRole('button', { name: 'Check sample definition' }).click();
  await expect(
    page.getByText('Sample configuration checked. Save to keep it for this session.', {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeFocused();
  await page.getByRole('button', { name: 'Save definition' }).click();
  await expect(page.getByRole('button', { name: 'Keep reviewing' })).toBeFocused();
  await page.getByRole('button', { name: 'Keep reviewing' }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeFocused();
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByText('Saved for this session.', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Back to Jobs' }).click();
  await expect(page).toHaveURL(/\/console\/jobs$/);
  const activity = page.locator('details.operation-activity');
  await expect(activity).not.toHaveAttribute('open');
  await activity.locator('summary').first().click();
  await page.getByRole('link', { name: 'Review saved definition' }).click();
  await expect(page.getByRole('textbox', { name: 'Definition name', exact: true })).toHaveValue(
    'hello-orbitjob',
  );
  expect(writes).toEqual([]);
});

test('existing definition reload is explicit and each change invalidates the cluster check', async ({
  page,
}) => {
  await page.goto('/console/jobs/17');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(page.getByLabel('Timeout in seconds', { exact: true })).toHaveValue('300');
  await page.getByRole('button', { name: 'Reload live definition', exact: true }).click();
  await page.getByRole('button', { name: 'Load live spec', exact: true }).click();
  await page.getByText('Resource identity and version', { exact: true }).click();
  await expect(page.getByText('Resource version', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Check sample definition' }).click();
  await expect(page.getByRole('button', { name: 'Save definition' })).toBeEnabled();
  const editor = page.getByRole('textbox', { name: 'Definition spec JSON' });
  await page.getByText('Advanced spec JSON', { exact: true }).click();
  await editor.fill((await editor.inputValue()).replace('300', '301'));
  await expect(page.getByRole('button', { name: 'Save definition' })).toBeDisabled();
  await page.getByRole('button', { name: 'Reload live definition' }).click();
  await page.getByRole('button', { name: 'Keep editing' }).click();
  expect(await editor.inputValue()).toContain('301');
});

test('attempt logs are read only on demand and are cleared when selection changes', async ({
  page,
}) => {
  await page.goto('/console/runs/1042');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await expect(page.getByRole('textbox', { name: 'Container log snapshot' })).toHaveCount(0);
  await page.locator('details.attempt-log-disclosure > summary').click();
  await page.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container log snapshot' })).toHaveValue(
    /attempt 2/,
  );
  await page.getByRole('button', { name: 'Attempt 1 Failed', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container log snapshot' })).toHaveCount(0);
  await page.locator('details.attempt-log-disclosure > summary').click();
  await page.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container log snapshot' })).toHaveValue(
    /attempt 1/,
  );
  await page.getByRole('button', { name: 'Clear log text' }).click();
  await expect(page.getByRole('textbox', { name: 'Container log snapshot' })).toHaveCount(0);
});
