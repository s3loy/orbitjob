import { expect, test, type Page } from './support';

async function demo(page: Page, path: string) {
  await page.goto(path);
  await page.getByRole('button', { name: 'Explore sample data' }).click();
}
async function nav(page: Page, name: string) {
  await page
    .getByRole('navigation', { name: 'Main navigation' })
    .getByRole('link', { name, exact: true })
    .click();
}

for (const labels of ['{ "team": "operations" }', '{"team":', '[1]'])
  test(`Function raw labels survive navigation and remain validated: ${labels}`, async ({
    page,
  }) => {
    await demo(page, '/console/functions/new');
    await page.getByLabel('Function name', { exact: true }).fill('retained-function');
    await page.getByLabel('Container image', { exact: true }).fill('busybox:1.37');
    await page.locator('summary').filter({ hasText: 'Advanced settings' }).click();
    await page.getByLabel('Labels JSON', { exact: true }).fill(labels);
    await nav(page, 'Jobs');
    await nav(page, 'Functions');
    await page.getByRole('link', { name: 'Create a function', exact: true }).click();
    await page.locator('summary').filter({ hasText: 'Advanced settings' }).click();
    await expect(page.getByLabel('Function name', { exact: true })).toHaveValue(
      'retained-function',
    );
    await expect(page.getByLabel('Labels JSON', { exact: true })).toHaveValue(labels);
    await page.getByRole('button', { name: 'Review function', exact: true }).click();
    if (labels.includes('operations')) {
      await expect(page.getByRole('region', { name: 'Review Function save' })).toBeVisible();
    } else {
      await expect(page.getByRole('alert')).toBeVisible();
      await expect(page.getByRole('button', { name: 'Confirm save', exact: true })).toHaveCount(0);
      await expect(page.getByLabel('Labels JSON', { exact: true })).toHaveValue(labels);
    }
  });

test('policy name, description, raw document and editing mode survive tabs and page navigation', async ({
  page,
}) => {
  await demo(page, '/console/administration?panel=policies');
  await page.getByRole('button', { name: 'Advanced JSON', exact: true }).click();
  const document = '{"statement":';
  await page.getByLabel('Name', { exact: true }).fill('draft-policy');
  await page.getByLabel('Description', { exact: true }).fill('Keep this local policy draft');
  await page.getByLabel('Policy document', { exact: true }).fill(document);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('draft-policy');
  await expect(page.getByLabel('Description', { exact: true })).toHaveValue(
    'Keep this local policy draft',
  );
  await expect(page.getByLabel('Policy document', { exact: true })).toHaveValue(document);
  await nav(page, 'Jobs');
  await page
    .locator('.sidebar-bottom')
    .getByRole('link', { name: 'Administration', exact: true })
    .click();
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await expect(page.getByLabel('Policy document', { exact: true })).toHaveValue(document);
  await page.getByRole('button', { name: 'Create policy', exact: true }).click();
  await expect(page.getByRole('alert').first()).toBeVisible();
  await expect(page.getByLabel('Policy document', { exact: true })).toHaveValue(document);
  await page
    .locator('.sidebar-bottom')
    .getByRole('link', { name: 'Workspace', exact: true })
    .click();
  await page.getByRole('button', { name: 'Leave sample data', exact: true }).click();
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page
    .locator('.sidebar-bottom')
    .getByRole('link', { name: 'Administration', exact: true })
    .click();
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue('');
});
