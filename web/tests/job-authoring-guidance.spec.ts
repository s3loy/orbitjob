import { signIn } from './support';
import { expect, fixtureIdentity, test } from './support';

test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });
const source = `apiVersion: workloads.orbitjob.io/v1alpha1
kind: ScheduledJob
metadata:
  name: ai-report
  namespace: team-ops
spec:
  schedule: ''
  jobTemplate:
    image: busybox:1.37
    command: [echo]
    args: ['hello from the imported task']
`;

async function connect(page: import('@playwright/test').Page) {
  await page.route('**/api/v1/**', (route) => {
    if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
      return route.fallback();
    return route.fulfill({
      json: new URL(route.request().url()).pathname.endsWith('/identity')
        ? {
            ...fixtureIdentity,
            tenant_name: 'Platform operations',
            tenant_slug: 'platform-ops',
            workload_namespaces: ['team-ops'],
          }
        : { items: [] },
    });
  });
  await page.goto('/console/manifests/job');
  await signIn(page, 'otj_authoring_fixture_key');
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
}

test('reviewing an incomplete job keeps the user in configuration with actionable errors', async ({
  page,
}) => {
  let writes = 0;
  page.on('request', (request) => {
    if (request.method() !== 'GET' && !new URL(request.url()).pathname.startsWith('/api/v1/auth/'))
      writes++;
  });
  await connect(page);
  await expect(page.getByRole('combobox', { name: 'Namespace', exact: true })).toHaveValue(
    'team-ops',
  );
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('');
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  const errors = page.getByRole('alert');
  await expect(errors).toContainText('Name');
  await expect(errors).toContainText('Execution namespace');
  await expect(errors).toContainText('Container image');
  await expect(errors).toBeFocused();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('first-task');
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toBeFocused();
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toHaveAttribute(
    'aria-invalid',
    'true',
  );
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('team-ops');
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37');
  await page.getByText('Command examples', { exact: true }).click();
  await page.getByRole('button', { name: 'Insert disk usage command', exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Execution method', exact: true })).toHaveValue(
    'shell',
  );
  await expect(page.getByRole('textbox', { name: 'Shell script', exact: true })).toHaveValue(
    'df -P /',
  );
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Review and save', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  const review = page.getByRole('region', { name: 'Review configuration', exact: true });
  await expect(review.getByRole('heading', { name: 'Review configuration' })).toBeFocused();
  await expect(review).toContainText('busybox:1.37');
  await expect(
    review.getByRole('list', { name: 'Command entries' }).getByRole('listitem'),
  ).toHaveText(['sh', '-c']);
  await expect(
    review.getByRole('list', { name: 'Argument entries' }).getByRole('listitem'),
  ).toHaveText(['df -P /']);
  await expect(review).toContainText('Manual only');
  await expect(
    review.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toBeEnabled();
  await expect(review.getByRole('button', { name: 'Save definition', exact: true })).toBeDisabled();
  expect(writes).toBe(0);
});

test('AI YAML imports into editable configuration and form edits reach the saved spec', async ({
  page,
}) => {
  const specs: string[] = [];
  await connect(page);
  await page.route('**/api/v1/definitions/jobs/**', (route) => {
    const body = route.request().postDataJSON();
    specs.push(body.spec_json);
    return route.fulfill({
      status: 403,
      json: { error: { code: 'FORBIDDEN', message: 'Fixture blocks cluster writes' } },
    });
  });
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill(source);
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue('ai-report');
  await expect(page.getByRole('combobox', { name: 'Namespace', exact: true })).toHaveValue(
    'team-ops',
  );
  await expect(page.getByRole('textbox', { name: 'Timeout in seconds', exact: true })).toHaveValue(
    '',
  );
  await expect(
    page.getByRole('textbox', { name: 'Timeout in seconds', exact: true }),
  ).toHaveAttribute('placeholder', 'Use default');
  await page.getByText('Run policies', { exact: true }).click();
  await expect(page.getByRole('combobox', { name: 'Concurrency policy', exact: true })).toHaveText(
    'Default',
  );
  await expect(page.getByRole('combobox', { name: 'Missed starts', exact: true })).toHaveText(
    'Default',
  );
  for (const label of [
    'Maximum attempts',
    'Kubernetes Job backoff limit',
    'Retain successful runs',
    'Retain failed runs',
  ]) {
    await expect(page.getByRole('textbox', { name: label, exact: true })).toHaveValue('');
    await expect(page.getByRole('textbox', { name: label, exact: true })).toHaveAttribute(
      'placeholder',
      'Use default',
    );
  }
  await page
    .getByRole('textbox', { name: 'Container image', exact: true })
    .fill('registry.example.com/report:release-2');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture blocks cluster writes');
  expect(specs).toHaveLength(1);
  const spec = JSON.parse(specs[0]);
  expect(spec.jobTemplate.image).toBe('registry.example.com/report:release-2');
  expect(spec.jobTemplate.command).toEqual(['echo']);
  expect(spec.jobTemplate.args).toEqual(['hello from the imported task']);
  expect(spec).not.toHaveProperty('timeoutSeconds');
  expect(spec).not.toHaveProperty('concurrencyPolicy');
  expect(spec).not.toHaveProperty('misfirePolicy');
  expect(spec).not.toHaveProperty('history');
  expect(spec).not.toHaveProperty('retryPolicy');
  expect(spec.jobTemplate).not.toHaveProperty('backoffLimit');
});

test('invalid YAML remains available to correct without overwriting the current form', async ({
  page,
}) => {
  await connect(page);
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('keep-this-draft');
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill('kind: [invalid');
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await expect(page.getByRole('alert').first()).toContainText('YAML cannot be read');
  await expect(page.getByRole('textbox', { name: 'Generated YAML', exact: true })).toHaveValue(
    'kind: [invalid',
  );
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    'keep-this-draft',
  );
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Import YAML edits', exact: true }).click();
  await expect(page.getByRole('alert').first()).toContainText('YAML cannot be read');
  await expect(page.getByRole('tab', { name: 'Advanced YAML', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).click();
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Generated YAML', exact: true })).toHaveValue(
    'kind: [invalid',
  );
  await expect(page.getByText('YAML changes need importing', { exact: true })).toBeVisible();
  await expect(page.getByText('Draft checks passed', { exact: true })).toHaveCount(0);
});

test('pending YAML requires an explicit choice before form edits or review can continue', async ({
  page,
}) => {
  const specs: string[] = [];
  await connect(page);
  await page.route('**/api/v1/definitions/jobs/**', (route) => {
    specs.push(route.request().postDataJSON().spec_json);
    return route.fulfill({
      status: 403,
      json: { error: { code: 'FORBIDDEN', message: 'Fixture blocks cluster writes' } },
    });
  });
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill(source);
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page
    .getByRole('textbox', { name: 'Generated YAML', exact: true })
    .fill(source.replace('busybox:1.37', 'image-a:latest'));
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toBeDisabled();
  await expect(page.getByRole('combobox', { name: 'Trigger mode', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Browse images', exact: true })).toBeDisabled();
  await page.getByText('Run policies', { exact: true }).click();
  await expect(
    page.getByRole('combobox', { name: 'Concurrency policy', exact: true }),
  ).toBeDisabled();
  await expect(page.getByRole('combobox', { name: 'Missed starts', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Review and save', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Check draft', exact: true })).toBeDisabled();
  await expect(page.getByRole('region', { name: 'Choose which draft to use' })).toBeVisible();
  await page.getByRole('button', { name: 'Discard YAML edits and keep form', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toHaveValue(
    'busybox:1.37',
  );
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('image-b:latest');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Fixture blocks cluster writes');
  expect(specs).toHaveLength(1);
  expect(JSON.parse(specs[0]).jobTemplate.image).toBe('image-b:latest');
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('another-task');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Discard retained draft and review this one', exact: true }),
  ).toBeEnabled();
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill(source);
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Discard retained draft and review this one', exact: true }),
  ).toBeDisabled();
  expect(specs).toHaveLength(1);
});

test('a saved job keeps its resource identity when configuration changes are reviewed', async ({
  page,
}) => {
  const requests: { dry_run: boolean; expected_uid: string; expected_resource_version: string }[] =
    [];
  await connect(page);
  await page.route('**/api/v1/definitions/jobs/**', (route) => {
    const body = route.request().postDataJSON();
    requests.push(body);
    return route.fulfill({
      json: {
        kind: 'jobs',
        name: 'ai-report',
        namespace: 'team-ops',
        uid: 'saved-resource-uid',
        resource_version: body.dry_run ? '1' : '2',
        generation: 1,
        spec_json: body.spec_json,
        manifest_digest: 'checked-digest',
        last_request_id: body.dry_run ? '' : body.request_id,
        last_manifest_digest: body.dry_run ? '' : 'checked-digest',
        request_id: body.request_id,
        state: body.dry_run ? 'validated' : 'accepted',
      },
    });
  });
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill(source);
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toBeEnabled();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37.1');
  await expect(page.getByRole('region', { name: 'How this Job will run' })).toContainText(
    'Saving updates this Job definition.',
  );
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect.poll(() => requests.length).toBe(3);
  expect(requests[2].expected_uid).toBe('saved-resource-uid');
  expect(requests[2].expected_resource_version).toBe('2');
  expect(requests[2].dry_run).toBe(true);
});

test('failed clipboard access selects YAML and reports a manual recovery instead of success', async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false });
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: () => Promise.reject(new Error('blocked')) },
    });
  });
  await connect(page);
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill(source);
  await page.getByRole('button', { name: 'Copy YAML', exact: true }).click();
  await expect(page.getByText('Clipboard unavailable.', { exact: false })).toBeVisible();
  await expect(page.getByText('YAML copied.', { exact: false })).toHaveCount(0);
  expect(
    await page
      .getByRole('textbox', { name: 'Generated YAML', exact: true })
      .evaluate((element: HTMLTextAreaElement) => ({
        start: element.selectionStart,
        end: element.selectionEnd,
        length: element.value.length,
      })),
  ).toEqual({ start: 0, end: source.length, length: source.length });
});

test('advanced JSON and admission defaults survive configuration and later saves', async ({
  page,
}) => {
  const requests: Record<string, unknown>[] = [];
  await connect(page);
  await page.route('**/api/v1/definitions/jobs/**', (route) => {
    const body = route.request().postDataJSON();
    requests.push(body);
    const canonical = {
      ...JSON.parse(body.spec_json),
      retryPolicy: { maxAttempts: 2 },
      history: { failedRuns: 9 },
    };
    return route.fulfill({
      json: {
        kind: 'jobs',
        name: 'ai-report',
        namespace: 'team-ops',
        uid: body.dry_run ? '' : 'saved-resource-uid',
        resource_version: body.dry_run ? '' : '2',
        generation: body.dry_run ? 0 : 1,
        spec_json: JSON.stringify(canonical),
        manifest_digest: 'checked-digest',
        last_request_id: body.dry_run ? '' : body.request_id,
        last_manifest_digest: body.dry_run ? '' : 'checked-digest',
        request_id: body.request_id,
        state: body.dry_run ? 'validated' : 'accepted',
      },
    });
  });
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await page
    .getByRole('textbox', { name: 'Generated YAML', exact: true })
    .fill(source.replace("  schedule: ''", "  schedule: ''\n  timeoutSeconds: 120"));
  await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByText('Advanced spec JSON', { exact: true }).click();
  const editor = page.getByRole('textbox', { name: 'Definition spec JSON', exact: true });
  const edited = JSON.parse(await editor.inputValue());
  edited.timeoutSeconds = 321;
  edited.jobTemplate.args = ['keep advanced changes', 'two words'];
  await editor.fill(JSON.stringify(edited));
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Timeout in seconds', exact: true })).toHaveValue(
    '321',
  );
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Jobs', exact: true })
    .click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Timeout in seconds', exact: true })).toHaveValue(
    '321',
  );
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByText('Advanced spec JSON', { exact: true }).click();
  expect(JSON.parse(await editor.inputValue())).toEqual(edited);
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect.poll(() => requests.length).toBe(2);
  await expect(
    page.getByRole('button', { name: 'Check against cluster', exact: true }),
  ).toBeEnabled();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByText('Run policies', { exact: true }).click();
  await expect(page.getByRole('textbox', { name: 'Maximum attempts', exact: true })).toHaveValue(
    '2',
  );
  await expect(page.getByRole('textbox', { name: 'Retain failed runs', exact: true })).toHaveValue(
    '9',
  );
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37.1');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  await expect.poll(() => requests.length).toBe(3);
  expect(requests[2]).toMatchObject({
    expected_uid: 'saved-resource-uid',
    expected_resource_version: '2',
    dry_run: true,
  });
  expect(JSON.parse(String(requests[2].spec_json))).toMatchObject({
    timeoutSeconds: 321,
    retryPolicy: { maxAttempts: 2 },
    history: { failedRuns: 9 },
    jobTemplate: { image: 'busybox:1.37.1', args: edited.jobTemplate.args },
  });
});

for (const invalid of ['unknown field', 'incomplete JSON'])
  test(`${invalid} blocks stale configuration until explicitly resolved`, async ({ page }) => {
    await connect(page);
    await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
    await page.getByRole('textbox', { name: 'Generated YAML', exact: true }).fill(source);
    await page.getByRole('button', { name: 'Import YAML into form', exact: true }).click();
    await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    await page.getByRole('button', { name: 'Review and save', exact: true }).click();
    await page.getByText('Advanced spec JSON', { exact: true }).click();
    const editor = page.getByRole('textbox', { name: 'Definition spec JSON', exact: true });
    const retained =
      invalid === 'unknown field'
        ? JSON.stringify({ ...JSON.parse(await editor.inputValue()), futureOption: { keep: true } })
        : '{"jobTemplate":';
    await editor.fill(retained);
    await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
    const recovery = page.getByRole('region', { name: 'Advanced JSON needs review', exact: true });
    await expect(recovery).toBeVisible();
    await expect(
      page.getByRole('textbox', { name: 'Container image', exact: true }),
    ).toBeDisabled();
    await recovery.getByRole('button', { name: 'Return to advanced JSON', exact: true }).click();
    await page.getByText('Advanced spec JSON', { exact: true }).click();
    await expect(editor).toHaveValue(retained);
    await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
    await recovery
      .getByRole('button', { name: 'Discard advanced JSON edits and keep form', exact: true })
      .click();
    await expect(recovery).toHaveCount(0);
    await expect(page.getByRole('textbox', { name: 'Container image', exact: true })).toBeEnabled();
    await page.getByRole('button', { name: 'Review and save', exact: true }).click();
    await page.getByText('Advanced spec JSON', { exact: true }).click();
    expect(JSON.parse(await editor.inputValue()).jobTemplate.image).toBe('busybox:1.37');
    expect(await editor.inputValue()).not.toContain('futureOption');
  });
