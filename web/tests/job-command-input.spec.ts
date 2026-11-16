import { parse } from 'yaml';
import { parseExecutionCommandLine } from '../src/execution-command';
import type { Definition, DefinitionInput } from '../src/api/definitions';
import { expect, fixtureIdentity, signIn, test, type Page } from './support';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

type Template = { image: string; command: string[]; args: string[] };

async function connect(page: Page, existing?: Definition) {
  const requests: DefinitionInput[] = [];
  await page.route('**/api/v1/**', (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/auth/')) return route.fallback();
    if (path === '/api/v1/identity')
      return route.fulfill({
        json: { ...fixtureIdentity, workload_namespaces: ['team-ops'] },
      });
    if (path.startsWith('/api/v1/definitions/jobs/') && request.method() === 'PUT') {
      requests.push(request.postDataJSON());
      return route.fulfill({
        status: 403,
        json: { error: { code: 'FORBIDDEN', message: 'Fixture blocks cluster writes' } },
      });
    }
    if (existing && path === `/api/v1/definitions/jobs/${existing.namespace}/${existing.name}`)
      return route.fulfill({ json: existing });
    return route.fulfill({ json: { items: [] } });
  });
  await page.goto(
    existing
      ? `/console/definitions/jobs?namespace=${existing.namespace}&name=${existing.name}`
      : '/console/manifests/job',
  );
  await signIn(page, 'otj_command_input_fixture_key');
  await expect(
    page.getByRole('heading', {
      name: existing ? 'Job definition' : 'Create a job',
      exact: true,
    }),
  ).toBeVisible();
  if (existing) {
    await expect(
      page.getByRole('combobox', { name: 'Execution method', exact: true }),
    ).toBeVisible();
  } else {
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('command-example');
    await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('team-ops');
    await page
      .getByRole('textbox', { name: 'Container image', exact: true })
      .fill('busybox:1.37.0');
  }
  return requests;
}

async function generatedTemplate(page: Page): Promise<Template> {
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  const manifest = parse(
    await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).inputValue(),
  );
  return manifest.spec.jobTemplate;
}

async function checkBuilder(page: Page, requests: DefinitionInput[]) {
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture blocks cluster writes');
  expect(requests).toHaveLength(1);
  expect(requests[0].dry_run).toBe(true);
  return JSON.parse(requests[0].spec_json);
}

test('one command field preserves quoted executable names and empty arguments without shell conversion', async ({
  page,
}) => {
  const requests = await connect(page);
  await page
    .getByRole('combobox', { name: 'Execution method', exact: true })
    .selectOption('executable');
  await page.getByRole('textbox', { name: 'Command', exact: true }).fill('echo hello');
  expect(await generatedTemplate(page)).toMatchObject({ command: ['echo'], args: ['hello'] });
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page
    .getByRole('textbox', { name: 'Command', exact: true })
    .fill('"echo hello" "  two words  " ""');
  await expect(page.getByRole('combobox', { name: 'Execution method', exact: true })).toHaveValue(
    'executable',
  );
  await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveCount(0);
  const expected = { command: ['echo hello'], args: ['  two words  ', ''] };
  expect(await generatedTemplate(page)).toMatchObject(expected);
  expect((await checkBuilder(page, requests)).jobTemplate).toMatchObject(expected);
});

test('explicit shell script edits and the chosen shell reach YAML and the checked spec', async ({
  page,
}) => {
  const requests = await connect(page);
  await page.getByRole('combobox', { name: 'Execution method', exact: true }).selectOption('shell');
  await page
    .getByRole('combobox', { name: 'Shell executable', exact: true })
    .selectOption('/bin/sh');
  const script = 'printf "%s\\n" "$1"\necho done';
  await page.getByRole('textbox', { name: 'Shell script', exact: true }).fill('echo first draft');
  await page.getByRole('textbox', { name: 'Shell script', exact: true }).fill(script);
  await page.locator('details.execution-command-advanced > summary').click();
  await page.getByRole('button', { name: 'Add argument', exact: true }).click();
  await page.getByRole('textbox', { name: 'Argument 1', exact: true }).fill('job-name');
  await page.getByRole('button', { name: 'Add argument', exact: true }).click();
  await page.getByRole('textbox', { name: 'Argument 2', exact: true }).fill('  literal value  ');
  const expected = {
    command: ['/bin/sh', '-c'],
    args: [script, 'job-name', '  literal value  '],
  };
  expect(await generatedTemplate(page)).toMatchObject(expected);
  expect((await checkBuilder(page, requests)).jobTemplate).toMatchObject(expected);
});

test('editing an existing executable keeps additional and empty command tokens in the checked spec', async ({
  page,
}) => {
  const existing: Definition = {
    kind: 'jobs',
    name: 'existing-report',
    namespace: 'team-ops',
    uid: 'existing-report-uid',
    resource_version: '12',
    generation: 2,
    spec_json: JSON.stringify({
      schedule: '',
      jobTemplate: {
        image: 'python:3.14',
        command: ['python', '-u', '', '/app/report.py'],
        args: ['--format=json', ''],
      },
    }),
    manifest_digest: 'existing-report-digest',
    last_request_id: '',
    last_manifest_digest: '',
  };
  const requests = await connect(page, existing);
  const advanced = page.locator('details.execution-command-advanced');
  await advanced.locator('summary').click();
  await expect(page.getByRole('textbox', { name: 'Command token 2', exact: true })).toHaveValue(
    '-u',
  );
  await expect(page.getByRole('textbox', { name: 'Command token 3', exact: true })).toHaveValue('');
  await expect(page.getByRole('textbox', { name: 'Command', exact: true })).toHaveAttribute(
    'readonly',
    '',
  );
  await page
    .getByRole('textbox', { name: 'Command token 1', exact: true })
    .fill('/usr/bin/python3');
  await page.getByRole('textbox', { name: 'Argument 1', exact: true }).fill('--format=text');
  await expect(advanced).toHaveAttribute('open', '');
  await expect(page.getByRole('textbox', { name: 'Command token 4', exact: true })).toHaveValue(
    '/app/report.py',
  );
  const expected = {
    command: ['/usr/bin/python3', '-u', '', '/app/report.py'],
    args: ['--format=text', ''],
  };
  await page.getByText('Advanced spec JSON', { exact: true }).click();
  const spec = JSON.parse(
    await page.getByRole('textbox', { name: 'Definition spec JSON', exact: true }).inputValue(),
  );
  expect(spec.jobTemplate).toMatchObject(expected);
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture blocks cluster writes');
  expect(requests).toHaveLength(1);
  expect(requests[0]).toMatchObject({
    dry_run: true,
    expected_uid: existing.uid,
    expected_resource_version: existing.resource_version,
  });
  expect(JSON.parse(requests[0].spec_json).jobTemplate).toMatchObject(expected);
});

test('method changes restore typed drafts and a new starting source replaces retained command content', async ({
  page,
}) => {
  await connect(page);
  const method = page.getByRole('combobox', { name: 'Execution method', exact: true });
  await method.selectOption('executable');
  await page
    .getByRole('textbox', { name: 'Command', exact: true })
    .fill('/app/custom "custom argument"');
  await method.selectOption('shell');
  await page
    .getByRole('textbox', { name: 'Shell script', exact: true })
    .fill('echo retained shell');
  await method.selectOption('image');
  await expect(page.getByRole('textbox', { name: 'Command', exact: true })).toHaveCount(0);
  await method.selectOption('executable');
  expect(
    parseExecutionCommandLine(
      await page.getByRole('textbox', { name: 'Command', exact: true }).inputValue(),
    ).value,
  ).toEqual({ command: ['/app/custom'], args: ['custom argument'] });
  await method.selectOption('shell');
  await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveValue(
    'echo retained shell',
  );

  await page.getByRole('button', { name: 'Use hello example', exact: true }).click();
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace draft', exact: true })
    .click();
  await expect(method).toHaveValue('executable');
  const helloCommand = await page
    .getByRole('textbox', { name: 'Command', exact: true })
    .inputValue();
  expect(parseExecutionCommandLine(helloCommand).value).toEqual({
    command: ['printf'],
    args: ['Hello from OrbitJob!\\n'],
  });
  await method.selectOption('shell');
  await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveValue('');
  await expect(page.getByRole('textbox', { name: 'Argument 1', exact: true })).toHaveCount(0);
  await method.selectOption('executable');
  expect(
    parseExecutionCommandLine(
      await page.getByRole('textbox', { name: 'Command', exact: true }).inputValue(),
    ).value.command,
  ).toEqual(['printf']);
  await method.selectOption('shell');
  await page.getByRole('textbox', { name: 'Shell script', exact: true }).fill('echo old source');
  await method.selectOption('executable');
  await page.getByRole('button', { name: 'Use hello example', exact: true }).click();
  await page
    .getByRole('alertdialog')
    .getByRole('button', { name: 'Replace draft', exact: true })
    .click();
  await method.selectOption('shell');
  await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveValue('');
});
