import { signIn } from './support';
import { expect, test } from './support';
import { demoClient } from '../src/api/demo';
test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

for (const variant of ['recovered', 'unavailable', 'empty'] as const)
  test(`Function log selection uses ${variant} attempt evidence without guessing`, async ({
    page,
  }) => {
    const fixture = demoClient();
    const definition = await fixture.functionDefinition('27');
    const original = (await fixture.functionRuns('27'))[0];
    fixture.dispose();
    const attempts =
      variant === 'recovered'
        ? [
            {
              attempt_number: 1,
              phase: 'Failed',
              started_at: original.started_at,
              completed_at: original.finished_at,
            },
            {
              attempt_number: 2,
              phase: 'Succeeded',
              started_at: original.started_at,
              completed_at: original.finished_at,
            },
          ]
        : variant === 'empty'
          ? []
          : null;
    const outcome = { ...original, attempts, last_attempt: variant === 'recovered' ? 2 : null };
    const reads: number[] = [];
    await page.route('**/api/v1/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fallback();
      if (path === '/api/v1/functions/27') return route.fulfill({ json: definition });
      if (path === `/api/v1/functions/27/runs/${original.run_id}`)
        return route.fulfill({ json: outcome });
      const attempt = /\/attempts\/([1-9]\d*)\/logs$/.exec(path);
      if (attempt) {
        const number = Number(attempt[1]);
        reads.push(number);
        return route.fulfill({
          json: {
            state: 'ready',
            text: number === 1 ? 'first attempt failed' : 'recovered',
            attempt: number,
            container: 'task',
            tail_lines: 100,
            limit_bytes: 65536,
            truncated: false,
            reason: '',
          },
        });
      }
      return route.fulfill({ json: { items: [] } });
    });
    await page.goto(`/console/functions/27/runs/${original.run_id}`);
    await signIn(page, 'function_attempt_fixture');
    await expect(
      page.getByRole('heading', { name: 'Execution outcome', exact: true }),
    ).toBeVisible();
    const logs = page.locator('details[aria-label="Attempt logs"]');
    await expect(logs).not.toHaveAttribute('open', '');
    await logs.locator(':scope > summary').click();
    if (variant === 'recovered') {
      await expect(logs.getByLabel('Log attempt', { exact: true })).toHaveValue('2');
      await logs.getByRole('button', { name: 'Read logs', exact: true }).click();
      await expect(logs.getByLabel('Container log snapshot')).toHaveValue('recovered');
      await logs.getByLabel('Log attempt', { exact: true }).selectOption('1');
      await expect(logs).toHaveAttribute('open', '');
      await expect(logs.getByLabel('Container log snapshot')).toHaveCount(0);
      await logs.getByRole('button', { name: 'Read logs', exact: true }).click();
      await expect(logs.getByLabel('Container log snapshot')).toHaveValue('first attempt failed');
      await logs.getByRole('button', { name: 'Clear log text', exact: true }).click();
      await expect(logs).toHaveAttribute('open', '');
      await expect(logs.getByRole('button', { name: 'Read logs', exact: true })).toBeVisible();
      expect(reads).toEqual([2, 1]);
    } else {
      await expect(logs.getByRole('button', { name: 'Read logs', exact: true })).toBeDisabled();
      if (variant === 'unavailable') {
        await expect(logs).toContainText('Attempt details unavailable');
        await expect(logs.getByLabel('Log attempt number')).toHaveValue('');
      } else {
        await expect(logs).toContainText('No execution attempts were recorded');
        await expect(logs.getByLabel('Log attempt number')).toHaveCount(0);
      }
      expect(reads).toEqual([]);
    }
  });
