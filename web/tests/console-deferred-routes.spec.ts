import { expect, test } from './support';
import { ulid } from 'ulid';
import { demoClient } from '../src/api/demo';

test('the current Demo exposes only supported Console modules', async ({ page }) => {
  await page.goto('/console/connection');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await expect(page).toHaveURL(/\/jobs$/);
  await expect(
    page
      .getByRole('navigation', { name: 'Main navigation' })
      .getByRole('link', { name: 'Jobs', exact: true }),
  ).toBeVisible();
  await expect(page.getByRole('link', { name: 'Experiments', exact: true })).toHaveCount(0);
  const client = demoClient();
  expect('experiments' in client).toBe(false);
  expect('createExperiment' in client).toBe(false);
  client.dispose();
});

test('direct deferred routes render the ordinary missing-page recovery', async ({ page }) => {
  const requests: string[] = [];
  page.on('request', (request) => {
    if (request.url().includes('/api/v1/experiments')) requests.push(request.url());
  });
  for (const suffix of ['', '/new', `/${ulid()}`]) {
    await page.goto(`/console/experiments${suffix}`);
    await expect(page.getByRole('heading', { name: 'Page not found', exact: true })).toBeVisible();
    await expect(page.getByLabel('Manifest JSON')).toHaveCount(0);
    await expect(
      page.getByRole('button', { name: 'Issue launch credential', exact: true }),
    ).toHaveCount(0);
  }
  expect(requests).toEqual([]);
});

test('a stale connection return destination cannot reenter a deferred route', async ({ page }) => {
  await page.addInitScript(() => {
    window.history.replaceState(
      { ...window.history.state, usr: { returnTo: '/experiments/new' } },
      '',
    );
  });
  await page.goto('/console/connection');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await expect(page).toHaveURL(/\/jobs$/);
  await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
  await expect(page.getByLabel('Manifest JSON')).toHaveCount(0);
});
