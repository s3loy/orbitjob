import { expect, fixtureIdentity, signIn, test, type Page } from './support';
import type { Definition, DefinitionInput } from '../src/api/definitions';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function connect(page: Page, definition?: Definition) {
  const checks: DefinitionInput[] = [];
  await page.route('**/api/v1/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/auth/')) return route.fallback();
    if (path === '/api/v1/identity')
      return route.fulfill({ json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] } });
    if (path.startsWith('/api/v1/definitions/') && request.method() === 'PUT') {
      checks.push(request.postDataJSON());
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Fixture stops at checked payload' } },
      });
    }
    if (
      definition &&
      path === `/api/v1/definitions/jobs/${definition.namespace}/${definition.name}`
    )
      return route.fulfill({ json: definition });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto(
    definition
      ? `/console/definitions/jobs?namespace=${definition.namespace}&name=${definition.name}`
      : '/console/manifests/job',
  );
  await signIn(page);
  await expect(page.getByRole('combobox', { name: 'Runtime', exact: true })).toBeVisible();
  return checks;
}

async function frequency(page: Page, name: string) {
  await page.getByRole('dialog').getByRole('combobox', { name: 'Frequency', exact: true }).click();
  await page.getByRole('option', { name, exact: true }).click();
}

test('default Shell and aligned starting actions lead to editable language presets with replacement protection', async ({
  page,
}) => {
  const checks = await connect(page);
  await expect(page.getByRole('combobox', { name: 'Runtime', exact: true })).toHaveValue('shell');
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toHaveValue(
    'busybox:1.37.0',
  );
  await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveValue(
    /Hello from OrbitJob/,
  );
  const bounds = await page.locator('.job-starting-actions button').evaluateAll((nodes) =>
    nodes.map((node) => {
      const r = node.getBoundingClientRect();
      return { top: r.top, height: r.height };
    }),
  );
  expect(new Set(bounds.map(({ top }) => top)).size).toBe(1);
  expect(new Set(bounds.map(({ height }) => height)).size).toBe(1);
  await page.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption('python');
  await expect(page.getByRole('alertdialog')).toHaveCount(0);
  const python = page.getByRole('textbox', { name: 'Python script', exact: true });
  await python.fill('print("my report")');
  await page.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption('node');
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Keep current code', exact: true })
    .click();
  await expect(python).toHaveValue('print("my report")');
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toHaveValue(
    'python:3.14-slim',
  );
  await page.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption('node');
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace runtime and code', exact: true })
    .click();
  const javascript = page.getByRole('textbox', { name: 'JavaScript', exact: true });
  await javascript.fill('console.log("my report");');
  await page.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption('custom');
  await expect(javascript).toHaveValue('console.log("my report");');
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('node-report');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture stops at checked payload');
  expect(checks).toHaveLength(1);
  expect(JSON.parse(checks[0].spec_json).jobTemplate).toMatchObject({
    image: 'node:24-bookworm-slim',
    command: ['node', '-e'],
    args: ['console.log("my report");'],
  });
});

test('typing a Python invocation keeps Command focused instead of switching editors mid-input', async ({
  page,
}) => {
  await connect(page);
  await page
    .getByRole('combobox', { name: 'Execution method', exact: true })
    .selectOption('executable');
  const command = page.getByRole('textbox', { name: 'Command', exact: true });
  await command.pressSequentially("python -c 'print(1)'");
  await expect(command).toBeFocused();
  await expect(command).toHaveValue("python -c 'print(1)'");
  await expect(page.getByRole('textbox', { name: 'Python script', exact: true })).toHaveCount(0);
  await expect(page.getByRole('combobox', { name: 'Execution method', exact: true })).toHaveValue(
    'executable',
  );
});

test('the generator writes an explicit weekly UTC schedule only after confirmation', async ({
  page,
}) => {
  const checks = await connect(page);
  await page.getByRole('combobox', { name: 'Trigger mode', exact: true }).click();
  await page.getByRole('option', { name: 'Scheduled and manual', exact: true }).click();
  const cron = page.getByRole('textbox', { name: 'Configured schedule', exact: true });
  await expect(page.locator('input[aria-label="Configured schedule"]')).toHaveValue('0 2 * * *');
  await page.getByRole('button', { name: 'Generate schedule', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Time zone: UTC');
  await frequency(page, 'Weekly');
  await dialog.getByLabel('Time', { exact: true }).fill('15:30');
  await dialog.getByRole('checkbox', { name: 'Friday', exact: true }).check();
  await expect(dialog.getByLabel('Generated cron expression')).toHaveText('30 15 * * 1,5');
  await expect(page.locator('input[aria-label="Configured schedule"]')).toHaveValue('0 2 * * *');
  await dialog.getByRole('button', { name: 'Use this schedule', exact: true }).click();
  await expect(cron).toHaveValue('30 15 * * 1,5');
  expect(checks).toEqual([]);
});

test('custom cron remains unchanged on cancel and invalid hourly input cannot be applied', async ({
  page,
}) => {
  await connect(page);
  await page.getByRole('combobox', { name: 'Trigger mode', exact: true }).click();
  await page.getByRole('option', { name: 'Scheduled and manual', exact: true }).click();
  const cron = page.getByRole('textbox', { name: 'Configured schedule', exact: true });
  await cron.fill('0 0 1 * MON');
  await page.getByRole('button', { name: 'Generate schedule', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('custom pattern');
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(cron).toHaveValue('0 0 1 * MON');
  await page.getByRole('button', { name: 'Generate schedule', exact: true }).click();
  await frequency(page, 'Hourly');
  const minute = page.getByRole('dialog').getByLabel('Minute of hour', { exact: true });
  await minute.fill('60');
  await expect(
    page.getByRole('dialog').getByRole('button', { name: 'Use this schedule', exact: true }),
  ).toBeDisabled();
  await minute.fill('7');
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Use this schedule', exact: true })
    .click();
  await expect(cron).toHaveValue('7 * * * *');
});

test('workflow generation preserves its configured timezone', async ({ page }) => {
  const checks = await connect(page);
  await page.goto('/console/manifests/workflow');
  await page.getByRole('combobox', { name: 'Trigger mode', exact: true }).click();
  await page.getByRole('option', { name: 'Scheduled and manual', exact: true }).click();
  await page.getByRole('textbox', { name: 'Time zone', exact: true }).fill('Asia/Shanghai');
  await page.getByRole('button', { name: 'Generate schedule', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('Time zone: Asia/Shanghai');
  await page.getByRole('dialog').getByLabel('Time', { exact: true }).fill('09:00');
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Use this schedule', exact: true })
    .click();
  await expect(page.getByRole('textbox', { name: 'Configured schedule', exact: true })).toHaveValue(
    '0 9 * * *',
  );
  await expect(page.getByRole('textbox', { name: 'Time zone', exact: true })).toHaveValue(
    'Asia/Shanghai',
  );
  expect(checks).toEqual([]);
});

test('changing an existing definition runtime preserves opaque fields and update identity', async ({
  page,
}) => {
  const definition: Definition = {
    kind: 'jobs',
    namespace: 'team-ops',
    name: 'existing-runtime',
    uid: 'existing-runtime-uid',
    resource_version: '12',
    generation: 2,
    manifest_digest: 'existing-digest',
    last_request_id: '',
    last_manifest_digest: '',
    spec_json: JSON.stringify({
      schedule: '',
      timeoutSeconds: 123,
      jobTemplate: {
        image: 'private.example/team/python:1',
        command: ['python', '-c'],
        args: ['print("saved")'],
        nodeSelector: { workload: 'batch' },
      },
    }),
  };
  const checks = await connect(page, definition);
  await page.getByRole('combobox', { name: 'Runtime', exact: true }).selectOption('node');
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace runtime and code', exact: true })
    .click();
  await page
    .getByRole('textbox', { name: 'JavaScript', exact: true })
    .fill('console.log("updated");');
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture stops at checked payload');
  expect(checks).toHaveLength(1);
  expect(checks[0]).toMatchObject({
    expected_uid: definition.uid,
    expected_resource_version: definition.resource_version,
  });
  expect(JSON.parse(checks[0].spec_json)).toMatchObject({
    timeoutSeconds: 123,
    jobTemplate: {
      image: 'node:24-bookworm-slim',
      command: ['node', '-e'],
      args: ['console.log("updated");'],
      nodeSelector: { workload: 'batch' },
    },
  });
});
