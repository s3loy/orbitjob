import { signInAccount } from './account-support';
import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import type { FunctionDefinition, FunctionRun, Workflow } from '../src/api/m4';

const c = JSON.parse(readFileSync(process.env.ORBITJOB_CONSOLE_CREDENTIALS!, 'utf8'));
const manifest = JSON.parse(
  readFileSync(
    join(dirname(process.env.ORBITJOB_CONSOLE_CREDENTIALS!), 'm4-functions.json'),
    'utf8',
  ),
);
const success = manifest.functions.find((f: { case: string }) => f.case === 'success');
const headers = (key = c.key) => ({ Authorization: `Bearer ${key}` });
async function get(page: Page, path: string, key = c.key) {
  const response = await page.request.get(`/api/v1/${path}`, { headers: headers(key) });
  expect(response.status()).toBe(200);
  return response.json();
}
async function open(page: Page, key = c.key) {
  await page.goto('/console/connection');
  await signInAccount(page, key);
}
async function nav(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

test('native UUID validation and newest-first pagination match their API contracts', async ({
  page,
}) => {
  for (const [id, expected] of [
    ['not-a-uuid', 400],
    ['ffffffff-ffff-ffff-ffff-ffffffffffff', 404],
  ] as const) {
    const response = await page.request.get(`/api/v1/functions/${success.id}/runs/${id}`, {
      headers: headers(),
    });
    expect(response.status()).toBe(expected);
  }
  const rows: FunctionDefinition[] = (await get(page, 'functions?limit=100')).items;
  expect(rows.length).toBeGreaterThan(3);
  for (let i = 1; i < rows.length; i++) {
    expect(Date.parse(rows[i - 1].created_at)).toBeGreaterThanOrEqual(
      Date.parse(rows[i].created_at),
    );
    if (rows[i - 1].created_at === rows[i].created_at)
      expect(Number(rows[i - 1].id)).toBeGreaterThan(Number(rows[i].id));
  }
  const first = (await get(page, 'functions?limit=2')).items;
  const next = (await get(page, 'functions?limit=2&offset=2')).items;
  expect([...first, ...next].map((f: FunctionDefinition) => f.id)).toEqual(
    rows.slice(0, 4).map((f) => f.id),
  );
});

test('native List-only credentials retain independent Function and Workflow histories', async ({
  page,
}) => {
  const workflows: Workflow[] = (await get(page, 'workflows', c.listOnlyKey)).items;
  const dag = workflows.find((w) => w.name === 'console-m4-dag')!;
  expect(
    (
      await page.request.get(`/api/v1/functions/${success.id}`, { headers: headers(c.listOnlyKey) })
    ).status(),
  ).toBe(403);
  expect(
    (
      await page.request.get(`/api/v1/workflows/${dag.id}`, { headers: headers(c.listOnlyKey) })
    ).status(),
  ).toBe(403);
  await open(page, c.listOnlyKey);
  await nav(page, 'Functions');
  const fn = (await get(page, 'functions?limit=100', c.listOnlyKey)).items.find(
    (f: FunctionDefinition) => String(f.id) === String(success.id),
  );
  await page.getByRole('link', { name: fn.name, exact: true }).click();
  const history: FunctionRun[] = (await get(page, `functions/${success.id}/runs`, c.listOnlyKey))
    .items;
  expect(history.length).toBeGreaterThan(0);
  await expect(page.getByTitle(history[0].run_id, { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp('^Invoke ') })).toHaveCount(0);
  await nav(page, 'Workflows');
  await page.getByRole('link', { name: dag.name, exact: true }).click();
  await page.getByRole('tab', { name: 'Runs', exact: true }).click();
  const runs = (await get(page, `workflows/${dag.id}/runs`, c.listOnlyKey)).items;
  expect(runs.length).toBeGreaterThan(0);
  await expect(page.getByRole('link', { name: `#${runs[0].id}`, exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: new RegExp('^Trigger ') })).toHaveCount(0);
});
