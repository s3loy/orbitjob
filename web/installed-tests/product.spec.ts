import { signInAccount } from './account-support';
import { test, expect, type Page } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
const state = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));
const evidenceDir = dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!);
const name = `${state.prefix}-ui-${Date.now()}`;
const evidence: Record<string, unknown> = { prefix: state.prefix, functionName: name };

async function saveDefinition(page: Page, kind: 'jobs' | 'workflows', name: string) {
  const path = `/api/v1/definitions/${kind}/orbitjob/${name}`;
  const checked = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === path &&
      response.request().method() === 'PUT' &&
      response.request().postDataJSON()?.dry_run === true,
  );
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  const check = await checked;
  expect(check.status()).toBe(200);
  const reviewed = await check.json();
  expect(reviewed.state).toBe('validated');
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  const accepted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === path &&
      response.request().method() === 'PUT' &&
      response.request().postDataJSON()?.dry_run === false,
  );
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  const saved = await accepted;
  expect(saved.status()).toBe(200);
  expect(saved.request().postDataJSON()).toMatchObject({
    request_id: reviewed.request_id,
    manifest_digest: reviewed.manifest_digest,
    spec_json: reviewed.spec_json,
  });
  expect(await saved.json()).toMatchObject({
    state: 'accepted',
    request_id: reviewed.request_id,
    manifest_digest: reviewed.manifest_digest,
  });
  await expect(page.getByText('Saved version is ready', { exact: true })).toBeVisible();
}

test('installed product creates, invokes, reads logs and edits Function state through forms', async ({
  page,
}) => {
  test.setTimeout(240_000);
  await page.goto('/console/functions/new');
  await signInAccount(page, state.logsKey);
  await page.getByLabel('Function name').fill(name);
  await page.getByLabel('Container image').fill('busybox:1.37');
  for (const entry of ['sh', '-c']) {
    await page.getByRole('button', { name: 'Add command', exact: true }).click();
    await page.getByLabel(`Command entry ${entry === 'sh' ? 1 : 2}`, { exact: true }).fill(entry);
  }
  await page.getByRole('button', { name: 'Add argument', exact: true }).click();
  await page.getByLabel('Arguments entry 1', { exact: true }).fill('echo product-ui-executed');
  await page.getByRole('button', { name: 'Review function' }).click();
  const created = page.waitForResponse(
    (r) =>
      new URL(r.url()).pathname.endsWith('/api/v1/functions') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Confirm save' }).click();
  const response = await created;
  expect(response.status()).toBe(201);
  const item = await response.json();
  evidence.functionId = item.id;
  await expect(
    page.getByRole('heading', { name: 'Saved version is ready', exact: true }),
  ).toBeVisible();
  await page.screenshot({
    path: join(evidenceDir, 'installed-function-ready.png'),
    fullPage: true,
  });
  await page.getByRole('button', { name: `Invoke ${name}` }).click();
  const invoked = page.waitForResponse(
    (r) => r.url().includes(`/functions/${item.id}/invoke`) && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Confirm invoke', exact: true }).click();
  const invocation = await invoked;
  expect(invocation.status()).toBe(201);
  expect(new URL(invocation.url()).searchParams.get('expected_version')).toBe('1');
  evidence.receipt = await invocation.json();
  const exact = page.getByRole('link', { name: 'Open this execution' });
  await expect(exact).toBeVisible();
  await exact.click();
  await expect
    .poll(
      async () => {
        await page.getByRole('button', { name: 'Refresh', exact: true }).click();
        return page.locator('.run-meta').innerText();
      },
      { intervals: [1000, 2000] },
    )
    .toBe('Success');
  const logs = page.locator('details[aria-label="Attempt logs"]');
  await logs.locator(':scope > summary').click();
  await logs.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(logs.getByLabel('Container log snapshot')).toHaveValue(/product-ui-executed/);
  await page.screenshot({ path: join(evidenceDir, 'installed-function-logs.png'), fullPage: true });
  await page.getByRole('link', { name: `Function #${item.id}`, exact: true }).click();
  await page.getByRole('link', { name: 'Edit function', exact: true }).click();
  await page.getByRole('combobox', { name: 'Invocation state' }).click();
  await page.getByRole('option', { name: 'Paused', exact: true }).click();
  await page.getByRole('button', { name: 'Review function' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(page.getByRole('heading', { name: 'Saved and paused' })).toBeVisible();
  await page.getByRole('button', { name: 'Continue editing current version' }).click();
  await page.getByRole('combobox', { name: 'Invocation state' }).click();
  await page.getByRole('option', { name: 'Active', exact: true }).click();
  await page.getByRole('button', { name: 'Review function' }).click();
  await page.getByRole('button', { name: 'Confirm save' }).click();
  await expect(
    page.getByRole('heading', { name: 'Saved version is ready', exact: true }),
  ).toBeVisible();
  evidence.completed = true;
  writeFileSync(
    join(evidenceDir, 'installed-product-evidence.json'),
    JSON.stringify(evidence, null, 2),
  );
});

test('installed workload forms create a manual Job and a paused scheduled Workflow with a working manual run', async ({
  page,
}) => {
  test.setTimeout(240_000);
  const jobName = `${state.prefix}-ui-job-${Date.now()}`;
  const workflowName = `${state.prefix}-ui-flow-${Date.now()}`;
  evidence.jobName = jobName;
  evidence.workflowName = workflowName;
  await page.goto('/console/manifests/job');
  await signInAccount(page, state.logsKey);
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(jobName);
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('orbitjob');
  await page.getByRole('textbox', { name: 'Container image', exact: true }).fill('busybox:1.37');
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await saveDefinition(page, 'jobs', jobName);
  await page.getByRole('link', { name: 'Open job', exact: true }).click();
  await page.getByRole('button', { name: `Trigger ${jobName}` }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const jobReceipt = page.getByRole('region', {
    name: `Trigger receipt for ${jobName}`,
    exact: true,
  });
  const run = jobReceipt.getByRole('link', { name: 'View run', exact: true });
  await expect(run).toBeVisible();
  await run.click();
  await expect(page.locator('.run-meta').getByText('Succeeded', { exact: true })).toBeVisible();
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name: 'Workflows', exact: true })
    .click();
  await page.getByRole('link', { name: 'Create a workflow', exact: true }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(workflowName);
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('orbitjob');
  await page.getByRole('combobox', { name: 'Trigger mode' }).click();
  await page.getByRole('option', { name: 'Scheduled and manual' }).click();
  await page.getByRole('textbox', { name: 'Configured schedule', exact: true }).fill('* * * * *');
  await page.getByRole('textbox', { name: 'Time zone', exact: true }).fill('Asia/Shanghai');
  await page
    .getByRole('checkbox', { name: 'Pause automatic starts; manual runs stay available' })
    .check();
  await page.getByRole('button', { name: 'Preview next starts' }).click();
  await expect(page.getByText('Times shown in Asia/Shanghai', { exact: true })).toBeVisible();
  await page.getByRole('tab', { name: 'Tasks', exact: true }).click();
  await page.getByRole('textbox', { name: 'Task name', exact: true }).fill('first');
  await page.getByRole('textbox', { name: 'Referenced Job name', exact: true }).fill(jobName);
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  await saveDefinition(page, 'workflows', workflowName);
  await page.getByRole('link', { name: 'Open workload and run' }).click();
  await expect(page.getByText('Automatic starts paused', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: `Trigger ${workflowName}` }).click();
  await page.getByRole('button', { name: 'Confirm trigger', exact: true }).click();
  const receipt = page.getByRole('region', {
    name: `Workflow trigger receipt for ${workflowName}`,
    exact: true,
  });
  const execution = receipt.getByRole('link', { name: 'Open this execution' });
  await expect(execution).toBeVisible();
  await execution.click();
  await expect(page.locator('.run-meta').getByText('Succeeded', { exact: true })).toBeVisible();
  await page.screenshot({
    path: join(evidenceDir, 'installed-workflow-execution.png'),
    fullPage: true,
  });
  evidence.workflowCompleted = true;
  writeFileSync(
    join(evidenceDir, 'installed-product-evidence.json'),
    JSON.stringify(evidence, null, 2),
  );
});
