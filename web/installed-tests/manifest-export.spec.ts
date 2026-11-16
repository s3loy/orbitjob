import { signInAccount } from './account-support';
import { expect, test } from '@playwright/test';
import { readFileSync, writeFileSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { dirname, join } from 'node:path';
const credentials = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));
const key = credentials.fullKey ?? credentials.key;
test('installed CSP permits offline validators and both exports pass actual server dry-run', async ({
  page,
}) => {
  const errors: string[] = [];
  const writes: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  page.on('request', (r) => {
    if (r.method() !== 'GET') writes.push(r.url());
  });
  expect(execFileSync('kubectl', ['config', 'current-context'], { encoding: 'utf8' }).trim()).toBe(
    'kind-orbitjob-console-e2e',
  );
  const response = await page.goto('/console/manifests/job');
  const csp = response!.headers()['content-security-policy'];
  expect(csp).toContain("script-src 'self'");
  expect(csp).not.toContain('unsafe-eval');
  await signInAccount(page, key);
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
  writes.length = 0;
  for (const kind of ['job', 'workflow']) {
    if (kind === 'workflow') {
      await page
        .getByRole('navigation')
        .getByRole('link', { name: 'Workflows', exact: true })
        .click();
      await page.getByRole('link', { name: 'Create a workflow', exact: true }).click();
    }
    await page
      .getByRole('textbox', { name: 'Name', exact: true })
      .fill(`console-roadmap-export-${kind}`);
    await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('orbitjob');
    if (kind === 'job') {
      await page
        .getByRole('textbox', { name: 'Container image', exact: true })
        .fill('busybox:1.37');
    } else {
      await page.getByRole('tab', { name: 'Tasks', exact: true }).click();
      await page.getByRole('textbox', { name: 'Task name', exact: true }).fill('first');
      await page
        .getByRole('textbox', { name: 'Referenced Job name', exact: true })
        .fill('console-success');
    }
    await page.getByRole('button', { name: 'Check draft', exact: true }).click();
    await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Download YAML' })).toBeEnabled();
    const yaml = await page.getByLabel('Generated YAML').inputValue();
    const output = execFileSync(
      'kubectl',
      ['--context', 'kind-orbitjob-console-e2e', 'apply', '--dry-run=server', '-f', '-'],
      { input: yaml, encoding: 'utf8' },
    );
    expect(output).toContain('(server dry run)');
    writeFileSync(
      join(dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!), `export-${kind}.yaml`),
      yaml,
    );
  }
  expect(errors).toEqual([]);
  expect(writes).toEqual([]);
});
