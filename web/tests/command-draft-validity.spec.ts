import type { Definition, DefinitionInput } from '../src/api/definitions';
import { expect, fixtureIdentity, signIn, test, type Page } from './support';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

const namespace = 'team-ops';
const existing: Definition = {
  kind: 'jobs',
  namespace,
  name: 'existing-command',
  uid: 'existing-command-uid',
  resource_version: '12',
  generation: 2,
  manifest_digest: 'existing-command-digest',
  last_request_id: '',
  last_manifest_digest: '',
  spec_json: JSON.stringify({
    schedule: '',
    concurrencyPolicy: 'Forbid',
    timeoutSeconds: 120,
    jobTemplate: {
      image: 'busybox:1.37.0',
      command: ['printf'],
      args: ['--format=json', ''],
      nodeSelector: { workload: 'batch' },
    },
  }),
};

async function connect(page: Page, editor = false, initialCommand = true) {
  const checks: DefinitionInput[] = [];
  const saves: DefinitionInput[] = [];
  await page.route('**/api/v1/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/auth/')) return route.fallback();
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: { ...fixtureIdentity, workload_namespaces: [namespace] },
      });
    if (path.startsWith('/api/v1/definitions/jobs/') && request.method() === 'PUT') {
      const input = request.postDataJSON() as DefinitionInput;
      (input.dry_run ? checks : saves).push(input);
      return route.fulfill({
        json: {
          ...existing,
          name: path.split('/').at(-1),
          spec_json: input.spec_json,
          request_id: input.request_id,
          manifest_digest: 'checked-command-digest',
          state: input.dry_run ? 'validated' : 'accepted',
        },
      });
    }
    if (path === `/api/v1/definitions/jobs/${namespace}/${existing.name}`)
      return route.fulfill({ json: existing });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto(
    editor
      ? `/console/definitions/jobs?namespace=${namespace}&name=${existing.name}`
      : '/console/manifests/job',
  );
  await signIn(page);
  await expect(
    page.getByRole('heading', { name: editor ? 'Job definition' : 'Create a job', exact: true }),
  ).toBeVisible();
  if (editor) {
    await expect(page.getByRole('textbox', { name: 'Command', exact: true })).toBeVisible();
  } else {
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('draft-command');
    await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption(namespace);
    await page
      .getByRole('textbox', { name: 'Container image', exact: true })
      .fill('busybox:1.37.0');
    if (initialCommand) {
      await page
        .getByRole('combobox', { name: 'Execution method', exact: true })
        .selectOption('executable');
      await page.getByRole('textbox', { name: 'Command', exact: true }).fill('printf seed');
    }
  }
  return { checks, saves };
}

async function checkBuilder(page: Page) {
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
}

test('choosing Command requires a program before either review or checking can proceed', async ({
  page,
}) => {
  const { checks, saves } = await connect(page, false, false);
  await page
    .getByRole('combobox', { name: 'Execution method', exact: true })
    .selectOption('executable');
  const command = page.getByRole('textbox', { name: 'Command', exact: true });
  await expect(command).toHaveValue('');
  await expect(command).toHaveAttribute('aria-invalid', 'true');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check draft', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toHaveCount(0);
  await page.locator('details.execution-command-advanced > summary').click();
  await page.getByRole('textbox', { name: 'Command token 1', exact: true }).fill('');
  await expect(command).toHaveAttribute('aria-invalid', 'true');
  expect(checks).toEqual([]);
  expect(saves).toEqual([]);
  await command.fill('echo hello');
  await checkBuilder(page);
  expect(JSON.parse(checks[0].spec_json).jobTemplate).toMatchObject({
    command: ['echo'],
    args: ['hello'],
  });
  expect(saves).toEqual([]);
});

test('an unfinished command survives tabs and navigation, then repairs the exact checked and saved payload', async ({
  page,
}) => {
  const { checks, saves } = await connect(page);
  const command = page.getByRole('textbox', { name: 'Command', exact: true });
  const unfinished = 'printf "unfinished value';
  await command.fill(unfinished);
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('retained-command');
  await expect(command).toHaveValue(unfinished);
  await expect(command).toHaveAttribute('aria-invalid', 'true');
  await expect(page.getByRole('region', { name: 'Unfinished command', exact: true })).toBeVisible();
  await expect(page.getByLabel('Configured command entries')).toHaveCount(0);
  await page.getByRole('button', { name: 'Check draft', exact: true }).click();
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toHaveCount(0);
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(
    page.getByRole('region', { name: 'YAML waiting for command', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Generated YAML', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Copy YAML', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Download YAML', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(command).toHaveValue(unfinished);
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).first().click();
  await expect(command).toHaveValue(unfinished);
  await expect(command).toHaveAttribute('aria-invalid', 'true');
  await expect(
    page.getByRole('alert', { name: 'Command needs attention', exact: true }),
  ).toContainText('Close the double quote');
  expect(checks).toEqual([]);
  expect(saves).toEqual([]);

  await command.fill("printf '%s\\n' 'two words' ''");
  await expect(command).not.toHaveAttribute('aria-invalid', 'true');
  await checkBuilder(page);
  expect(checks).toHaveLength(1);
  expect(JSON.parse(checks[0].spec_json).jobTemplate).toMatchObject({
    command: ['printf'],
    args: ['%s\\n', 'two words', ''],
  });
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect.poll(() => saves.length).toBe(1);
  expect(saves[0].spec_json).toBe(checks[0].spec_json);
});

test('an unfinished command cannot reopen an earlier checked builder review or export stale YAML', async ({
  page,
}) => {
  const { checks, saves } = await connect(page);
  await checkBuilder(page);
  expect(checks).toHaveLength(1);
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByRole('textbox', { name: 'Command', exact: true }).fill("printf 'unfinished");
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'data-state',
    'active',
  );
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Generated YAML', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Review and save', exact: true })).toHaveCount(0);
  expect(checks).toHaveLength(1);
  expect(saves).toEqual([]);
});

test('explicitly reloading an unchanged live version clears the unfinished command shown in the editor', async ({
  page,
}) => {
  const { checks, saves } = await connect(page, true);
  const command = page.getByRole('textbox', { name: 'Command', exact: true });
  const original = await command.inputValue();
  await command.fill("printf 'unfinished");
  await expect(command).toHaveAttribute('aria-invalid', 'true');
  await page.getByRole('button', { name: 'Reload live definition', exact: true }).click();
  await page
    .getByRole('dialog')
    .getByRole('button', { name: 'Load live spec', exact: true })
    .click();
  await expect(command).toHaveValue(original);
  await expect(command).toHaveAttribute('aria-invalid', 'false');
  await expect(page.getByRole('region', { name: 'Unfinished command', exact: true })).toHaveCount(
    0,
  );
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
  expect(checks).toHaveLength(1);
  expect(checks[0].spec_json).toBe(existing.spec_json);
  expect(saves).toEqual([]);
});

test('an existing Job retains unfinished command text and requires a repaired check before saving', async ({
  page,
}) => {
  const { checks, saves } = await connect(page, true);
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
  const command = page.getByRole('textbox', { name: 'Command', exact: true });
  const unfinished = 'printf "unfinished value';
  await command.fill(unfinished);
  await page.getByRole('textbox', { name: 'Timeout in seconds', exact: true }).fill('240');
  await expect(command).toHaveValue(unfinished);
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Review changes', exact: true })).toBeDisabled();
  await expect(page.getByRole('region', { name: 'Unfinished command', exact: true })).toBeVisible();
  await page.getByText('Advanced spec JSON', { exact: true }).click();
  await expect(
    page.getByRole('textbox', { name: 'Definition spec JSON', exact: true }),
  ).toHaveCount(0);
  await page.getByRole('link', { name: 'Back to Jobs', exact: true }).click();
  await page.goBack();
  await expect(command).toHaveValue(unfinished);
  await expect(command).toHaveAttribute('aria-invalid', 'true');
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toBeDisabled();
  expect(checks).toHaveLength(1);
  expect(saves).toEqual([]);

  await command.fill("printf '%s\\n' 'repaired value' ''");
  const raw = page.locator('details.definition-raw');
  if (!(await raw.evaluate((node) => (node as HTMLDetailsElement).open)))
    await raw.locator('summary').click();
  const spec = JSON.parse(
    await raw.getByRole('textbox', { name: 'Definition spec JSON', exact: true }).inputValue(),
  );
  expect(spec).toMatchObject({
    concurrencyPolicy: 'Forbid',
    timeoutSeconds: 240,
    jobTemplate: {
      command: ['printf'],
      args: ['%s\\n', 'repaired value', ''],
      nodeSelector: { workload: 'batch' },
    },
  });
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save definition', exact: true })).toBeEnabled();
  expect(checks).toHaveLength(2);
  expect(checks[1]).toMatchObject({
    expected_uid: existing.uid,
    expected_resource_version: existing.resource_version,
  });
  expect(JSON.parse(checks[1].spec_json)).toEqual(spec);
  expect(saves).toEqual([]);
});
