import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { signInAccount } from './account-support';

const state = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));

test('first-job tutorial saves the reviewed Shell job and follows its execution and archive', async ({
  page,
}) => {
  const name = `${state.prefix}-doc-${Date.now().toString(36).slice(-5)}`.padEnd(63, 'x');
  await page.goto('/console/manifests/job');
  await signInAccount(page, state.logsKey);
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill(name);
  await page.getByRole('combobox', { name: 'Namespace', exact: true }).selectOption('orbitjob');
  await expect(page.getByLabel('Runtime', { exact: true })).toHaveValue('shell');
  await expect(page.getByLabel('Execution method', { exact: true })).toHaveValue('shell');
  await page.getByLabel('Shell script', { exact: true }).fill("printf 'hello from OrbitJob\\n'");
  await page.getByRole('button', { name: 'Review and save', exact: true }).click();
  const checked = page.waitForResponse(
    (response) =>
      response.request().method() === 'PUT' &&
      new URL(response.url()).pathname.includes('/definitions/jobs/') &&
      response.request().postDataJSON()?.dry_run === true,
  );
  await page.getByRole('button', { name: 'Check against cluster', exact: true }).click();
  const check = await checked;
  if (check.status() !== 200)
    await test.info().attach('definition-check.json', {
      body: JSON.stringify({
        request: check.request().postDataJSON(),
        response: await check.json(),
      }),
      contentType: 'application/json',
    });
  expect(check.status()).toBe(200);
  expect((await check.json()).name).toBe(name);
  await page.getByRole('button', { name: 'Save definition', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm save', exact: true }).click();
  await expect(page.getByText('Saved version is ready', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: `Run once ${name}`, exact: true }).click();
  await page.getByRole('button', { name: 'Confirm run', exact: true }).click();
  const receipt = page.getByRole('region', { name: `Trigger receipt for ${name}`, exact: true });
  const run = receipt.getByRole('link', { name: 'View run', exact: true });
  await expect(run).toBeVisible();
  const id = (await run.getAttribute('href'))!.split('/').at(-1)!;
  await run.click();
  await expect(page.locator('.run-meta').getByText('Succeeded', { exact: true })).toBeVisible();
  const detail = await page.request.get(`/api/v1/instances/${id}`, {
    headers: { Authorization: `Bearer ${state.key}` },
  });
  expect(detail.status()).toBe(200);
  const recorded = await detail.json();
  expect(recorded.actor).toBe(state.accountsByKey[state.logsKey].userId);
  expect(recorded.attempts).toHaveLength(1);
  expect(recorded.attempts[0].phase).toBe('Succeeded');
  let retainedArchive = '';
  await expect
    .poll(async () => {
      const result = await page.request.get(
        `/api/v1/instances/${id}/logs/archive?after=0&limit=500`,
        {
          headers: { Authorization: `Bearer ${state.key}` },
        },
      );
      if (result.status() !== 200) return { status: result.status() };
      const archive = await result.json();
      retainedArchive = JSON.stringify(archive, null, 2);
      const protectionRemaining = Date.parse(archive.protected_until) - Date.now();
      return {
        state: archive.state,
        outcome: archive.outcome,
        gaps: archive.gaps,
        stdout: (archive.records ?? [])
          .filter((record: { kind: string }) => record.kind === 'stdout')
          .map((record: { text: string }) => record.text)
          .join(''),
        protectedForFifteenDays:
          Math.abs(protectionRemaining - 15 * 24 * 60 * 60 * 1000) < 5 * 60 * 1000,
      };
    })
    .toEqual({
      state: 'sealed',
      outcome: 'Succeeded',
      gaps: [],
      stdout: 'hello from OrbitJob\n',
      protectedForFifteenDays: true,
    });
  await page.getByRole('button', { name: 'Refresh output', exact: true }).click();
  await expect(
    page
      .getByRole('region', { name: 'Execution logs', exact: true })
      .getByText('Collection finished', { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .getByRole('region', { name: 'Execution logs', exact: true })
      .getByText('hello from OrbitJob', { exact: true }),
  ).toBeVisible();
  await test.info().attach('first-job-archive.json', {
    body: retainedArchive,
    contentType: 'application/json',
  });
  await test.info().attach('first-job-output.png', {
    body: await page.screenshot({ fullPage: true }),
    contentType: 'image/png',
  });
});
