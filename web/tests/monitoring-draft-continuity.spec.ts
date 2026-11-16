import { signIn } from './support';
import { expect, test, type Page } from './support';

async function section(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}
async function demo(page: Page) {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Explore sample data', exact: true }).click();
  await section(page, 'Checks');
}

test('closing a monitoring form retains input across Escape, outside click and navigation until discard', async ({
  page,
}) => {
  await demo(page);
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Name', { exact: true }).fill('retained-check');
  await dialog.getByLabel('URL', { exact: true }).fill('https://service.example/health');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('retained-check');
  await expect(dialog.getByLabel('URL', { exact: true })).toHaveValue(
    'https://service.example/health',
  );
  await page.mouse.click(5, 5);
  await expect(dialog).toHaveCount(0);
  await section(page, 'Jobs');
  await section(page, 'Checks');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('retained-check');
  await dialog.getByRole('button', { name: 'Discard draft', exact: true }).click();
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('');
  await expect(dialog.getByLabel('URL', { exact: true })).toHaveValue('');
});

test('a successful Check creation clears its own draft while an unfinished SLI survives', async ({
  page,
}) => {
  await demo(page);
  await section(page, 'SLOs');
  await page.getByRole('link', { name: 'SLIs', exact: true }).click();
  await page.getByRole('button', { name: 'New SLI', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Name', { exact: true }).fill('unfinished-indicator');
  await dialog.getByRole('combobox', { name: 'Source type', exact: true }).selectOption('checks');
  await dialog
    .getByRole('combobox', { name: 'Monitoring source', exact: true })
    .selectOption('check-1');
  await dialog.getByText('Custom criteria', { exact: true }).click();
  await dialog.getByLabel('Good-event criteria (JSON)', { exact: true }).fill('{"status":');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await section(page, 'Checks');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await dialog.getByLabel('Name', { exact: true }).fill('completed-check');
  await dialog.getByLabel('URL', { exact: true }).fill('https://service.example/health');
  await dialog.getByRole('button', { name: 'Create check', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'completed-check', exact: true })).toBeVisible();
  await section(page, 'Checks');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await section(page, 'SLOs');
  await page.getByRole('link', { name: 'SLIs', exact: true }).click();
  await page.getByRole('button', { name: 'New SLI', exact: true }).click();
  await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('unfinished-indicator');
  await expect(dialog.getByRole('combobox', { name: 'Source type', exact: true })).toHaveValue(
    'checks',
  );
  await expect(
    dialog.getByRole('combobox', { name: 'Monitoring source', exact: true }),
  ).toHaveValue('check-1');
  await expect(dialog.getByLabel('Good-event criteria (JSON)', { exact: true })).toHaveValue(
    '{"status":',
  );
});

test('disconnect clears retained monitoring drafts', async ({ page }) => {
  await demo(page);
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await page.getByRole('dialog').getByLabel('Name', { exact: true }).fill('old-connection');
  await page.keyboard.press('Escape');
  await page
    .locator('.sidebar-bottom')
    .getByRole('link', { name: 'Workspace', exact: true })
    .click();
  await page.getByRole('button', { name: 'Leave sample data', exact: true }).click();
  await page.getByRole('button', { name: 'Explore sample data', exact: true }).click();
  await section(page, 'Checks');
  await page.getByRole('button', { name: 'New check', exact: true }).click();
  await expect(page.getByRole('dialog').getByLabel('Name', { exact: true })).toHaveValue('');
});

test.describe('unconfirmed creation', () => {
  test.use({ baseURL: process.env.ORBITJOB_BROWSER_INSTALLED_URL || 'http://127.0.0.1:4174' });

  test('closing or navigating cannot turn an unknown write into another creation', async ({
    page,
  }) => {
    let writes = 0;
    let release!: () => void;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route('**/api/v1/**', async (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/api/v1/auth/'))
        return route.fallback();
      const path = new URL(route.request().url()).pathname;
      if (path === '/api/v1/identity') return route.fallback();
      if (path === '/api/v1/checks' && route.request().method() === 'POST') {
        writes++;
        await held;
        return route.fulfill({
          status: 500,
          json: { error: { code: 'INTERNAL', message: 'Outcome unknown' } },
        });
      }
      return route.fulfill({ json: { items: [] } });
    });
    await page.goto('/console/checks');
    await signIn(page, 'monitoring_draft_fixture_not_a_real_key');
    await section(page, 'Jobs');
    await section(page, 'Checks');
    await page.getByRole('button', { name: 'New check', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Name', { exact: true }).fill('possibly-created');
    await dialog.getByLabel('URL', { exact: true }).fill('https://service.example/health');
    await dialog.getByRole('button', { name: 'Create check', exact: true }).click();
    await expect(dialog.getByRole('button', { name: 'Creating…', exact: true })).toBeDisabled();
    await page.goBack();
    await expect(page.getByRole('heading', { name: 'Jobs', exact: true })).toBeVisible();
    release();
    await section(page, 'Checks');
    await page.getByRole('button', { name: 'New check', exact: true }).click();
    await expect(dialog.getByRole('button', { name: 'Create check', exact: true })).toBeDisabled();
    await expect(dialog).toContainText('Creation was not confirmed');
    await page.keyboard.press('Escape');
    await section(page, 'Jobs');
    await section(page, 'Checks');
    await page.getByRole('button', { name: 'New check', exact: true }).click();
    await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('possibly-created');
    await expect(dialog.getByRole('button', { name: 'Create check', exact: true })).toBeDisabled();
    await page.keyboard.press('Enter');
    expect(writes).toBe(1);
    await dialog.getByRole('button', { name: 'Discard draft', exact: true }).click();
    await expect(
      dialog.getByRole('region', { name: 'Discard unconfirmed monitoring draft' }),
    ).toContainText('creating again may duplicate it');
    await dialog.getByRole('button', { name: 'Keep draft', exact: true }).click();
    await expect(dialog.getByRole('button', { name: 'Create check', exact: true })).toBeDisabled();
    await dialog.getByRole('button', { name: 'Discard draft', exact: true }).click();
    await dialog.getByRole('button', { name: 'Discard unconfirmed draft', exact: true }).click();
    await page.getByRole('button', { name: 'New check', exact: true }).click();
    await expect(dialog.getByLabel('Name', { exact: true })).toHaveValue('');
    expect(writes).toBe(1);
  });
});
