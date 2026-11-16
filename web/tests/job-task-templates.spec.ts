import { parse } from 'yaml';
import { expect, fixtureIdentity, signIn, test, type Page } from './support';
import type { DefinitionInput } from '../src/api/definitions';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

async function connect(page: Page) {
  const checks: DefinitionInput[] = [];
  const writes: string[] = [];
  await page.route('**/api/v1/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/auth/')) return route.fallback();
    if (path === '/api/v1/identity')
      return route.fulfill({ json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] } });
    if (request.method() !== 'GET') writes.push(path);
    if (path.startsWith('/api/v1/definitions/') && request.method() === 'PUT') {
      checks.push(request.postDataJSON());
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Fixture stops at checked payload' } },
      });
    }
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto('/console/manifests/job');
  await signIn(page);
  await expect(
    page.getByRole('button', { name: 'Choose a task template', exact: true }),
  ).toBeVisible();
  return { checks, writes };
}

async function openTemplates(page: Page, template = 'message') {
  await page.getByRole('button', { name: 'Choose a task template', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Choose a task template', exact: true });
  await dialog.getByLabel('Task template', { exact: true }).selectOption(template);
  return dialog;
}

async function generatedDraft(page: Page) {
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  const draft = parse(
    await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).inputValue(),
  );
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  return draft;
}

test('message parameters update exact preview and checked payload without running the task', async ({
  page,
}) => {
  const state = await connect(page);
  const dialog = await openTemplates(page);
  await expect(dialog.getByLabel('Message', { exact: true })).toHaveValue('Hello from OrbitJob!');
  const message = 'hello $(HOME); `id` %s\nsecond line';
  await dialog.getByLabel('Message', { exact: true }).fill(message);
  await dialog.locator('.job-task-template-exact > summary').click();
  await expect(dialog.getByLabel('Generated command')).toHaveText('["printf"]');
  await expect(dialog.getByLabel('Argument 1', { exact: true })).toHaveText(
    JSON.stringify(message.replaceAll('$', () => '$$')),
  );
  expect(state.writes).toEqual([]);
  await dialog.getByRole('button', { name: 'Use this template', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('print-message');
  await expect(page.getByRole('combobox', { name: 'Namespace', exact: true })).toHaveValue(
    'team-ops',
  );
  const draft = await generatedDraft(page);
  expect(draft.spec).toMatchObject({
    schedule: '',
    suspend: false,
    retryPolicy: { maxAttempts: 1 },
    jobTemplate: {
      backoffLimit: 0,
      command: ['printf'],
      args: ['%s\\n', message.replaceAll('$', () => '$$')],
    },
  });
  expect(state.writes).toEqual([]);
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture stops at checked payload');
  expect(state.checks).toHaveLength(1);
  expect(JSON.parse(state.checks[0].spec_json)).toEqual(draft.spec);
  expect(state.checks[0].dry_run).toBe(true);
});

test('template cancellation and replacement cancellation retain unfinished Command text', async ({
  page,
}) => {
  const state = await connect(page);
  await page
    .getByRole('combobox', { name: 'Execution method', exact: true })
    .selectOption('executable');
  const command = page.getByRole('textbox', { name: 'Command', exact: true });
  await command.fill("printf 'unfinished");
  let dialog = await openTemplates(page, 'http');
  await dialog.getByLabel('URL', { exact: true }).fill('https://keep.example/path');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(command).toHaveValue("printf 'unfinished");
  await expect(
    page.getByRole('button', { name: 'Choose a task template', exact: true }),
  ).toBeFocused();
  dialog = await openTemplates(page, 'http');
  await expect(dialog.getByLabel('URL', { exact: true })).toHaveValue('https://keep.example/path');
  await dialog.getByRole('button', { name: 'Use this template', exact: true }).click();
  const confirmation = page.getByRole('alertdialog', {
    name: 'Replace this Job draft?',
    exact: true,
  });
  await expect(confirmation).toContainText('resource overrides');
  await confirmation.getByRole('button', { name: 'Keep current draft', exact: true }).click();
  await expect(command).toHaveValue("printf 'unfinished");
  await expect(
    page.getByRole('button', { name: 'Choose a task template', exact: true }),
  ).toBeFocused();
  await expect(page.getByRole('alert', { name: 'Command needs attention' })).toBeVisible();
  dialog = await openTemplates(page, 'http');
  await dialog.getByRole('button', { name: 'Use this template', exact: true }).click();
  await confirmation.getByRole('button', { name: 'Replace draft', exact: true }).click();
  await expect(page.getByLabel('Python script', { exact: true })).toHaveValue(/urllib.request/);
  await expect(page.getByRole('alert', { name: 'Command needs attention' })).toHaveCount(0);
  expect(state.writes).toEqual([]);
});

test('HTTP invalid input removes the preview and preserves explicit defaults and raw values', async ({
  page,
}) => {
  const state = await connect(page);
  const dialog = await openTemplates(page, 'http');
  const targetRequests: string[] = [];
  await page.route('https://example.com/**', (route) => {
    targetRequests.push(route.request().url());
    return route.abort();
  });
  await expect(dialog.getByLabel('URL', { exact: true })).toHaveValue('https://example.com');
  await expect(dialog.getByLabel('Expected status', { exact: true })).toHaveValue('200');
  await expect(dialog.getByLabel('Request timeout (seconds)', { exact: true })).toHaveValue('10');
  await dialog.getByLabel('URL', { exact: true }).fill('https://user:password@example.com');
  await expect(dialog.getByLabel('URL', { exact: true })).toHaveAttribute('aria-invalid', 'true');
  await expect(dialog.locator('[data-task-template-preview]')).toHaveAttribute(
    'data-task-template-preview',
    'unavailable',
  );
  await expect(dialog.getByLabel('Generated command')).toHaveCount(0);
  await expect(
    dialog.getByRole('button', { name: 'Use this template', exact: true }),
  ).toBeDisabled();
  await dialog.getByLabel('URL', { exact: true }).fill('https://example.com/not-found');
  await dialog.getByLabel('Request timeout (seconds)', { exact: true }).fill('0');
  await expect(dialog.getByLabel('Request timeout (seconds)', { exact: true })).toHaveValue('0');
  await expect(
    dialog.getByRole('button', { name: 'Use this template', exact: true }),
  ).toBeDisabled();
  await dialog.getByLabel('Request timeout (seconds)', { exact: true }).fill('3');
  await dialog.getByLabel('Expected status', { exact: true }).fill('404');
  await dialog.locator('.job-task-template-exact > summary').click();
  await expect(dialog.getByLabel('Argument 2', { exact: true })).toHaveText('"404"');
  await expect(dialog.getByLabel('Argument 3', { exact: true })).toHaveText('"3"');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(state.writes).toEqual([]);
  expect(targetRequests).toEqual([]);
});

test('JSON field choices follow the input and generated arguments survive editing and route return', async ({
  page,
}) => {
  const state = await connect(page);
  const dialog = await openTemplates(page, 'json');
  const input = '{"field $(HOME)":"\u4e2d\u6587","count":3}';
  await dialog.getByLabel('JSON input', { exact: true }).fill(input);
  await dialog.getByLabel('Operation', { exact: true }).selectOption('field');
  await dialog.getByLabel('Field', { exact: true }).selectOption('field $(HOME)');
  await dialog.locator('.job-task-template-exact > summary').click();
  await expect(dialog.getByLabel('Argument 3', { exact: true })).toHaveText('"field $$(HOME)"');
  await dialog.getByLabel('JSON input', { exact: true }).fill('{');
  await expect(
    dialog.getByRole('button', { name: 'Use this template', exact: true }),
  ).toBeDisabled();
  await dialog.getByLabel('JSON input', { exact: true }).fill(input);
  await dialog.getByRole('button', { name: 'Use this template', exact: true }).click();
  const before = await generatedDraft(page);
  expect(before.spec.jobTemplate.args.slice(1)).toEqual([
    input.replaceAll('$', () => '$$'),
    'field',
    'field $$(HOME)',
  ]);
  const code = page.getByLabel('Python script', { exact: true });
  const edited = `${await code.inputValue()}\nprint("customized")`;
  await code.fill(edited);
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).click();
  await expect(page.getByLabel('Python script', { exact: true })).toHaveValue(edited);
  const after = await generatedDraft(page);
  expect(after.spec.jobTemplate.args.slice(1)).toEqual(before.spec.jobTemplate.args.slice(1));
  expect(state.writes).toEqual([]);
});

test('template dialog fits a narrow screen with reachable actions and long parameters', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 844 });
  await connect(page);
  const dialog = await openTemplates(page, 'json');
  await dialog
    .getByLabel('JSON input', { exact: true })
    .fill(JSON.stringify({ long: 'x'.repeat(6000) }));
  await expect(
    dialog.getByRole('button', { name: 'Use this template', exact: true }),
  ).toBeInViewport();
  const bounds = await dialog.boundingBox();
  expect(bounds).not.toBeNull();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.y).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(320);
  expect(bounds!.y + bounds!.height).toBeLessThanOrEqual(844);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(320);
  await dialog
    .getByRole('heading', { name: 'Generated Job preview', exact: true })
    .scrollIntoViewIfNeeded();
  await dialog.locator('.job-task-template-exact > summary').click();
  await expect(dialog.getByLabel('Generated Python script')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: 'Choose a task template', exact: true }),
  ).toBeFocused();
});
