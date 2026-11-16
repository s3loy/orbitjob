import { expect, test } from './support';

test('the seeded Workflow opens its complete definition for desktop design review', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1710, height: 1000 });
  await page.goto('/console/workflows/412');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page.getByRole('link', { name: 'Edit definition', exact: true }).click();
  await expect(
    page.getByRole('heading', { name: 'Workflow definition', exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole('button', { name: 'Reload live definition', exact: true }),
  ).toBeEnabled();
  await page.locator('.advanced-run-settings > summary').click();
  await expect(page.getByRole('combobox', { name: 'Failure policy', exact: true })).toHaveText(
    'Stop on failure',
  );
  await expect(
    page.getByText('Load the existing definition before editing its spec.', { exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText('Definition not found.', { exact: false })).toHaveCount(0);
  await expect(page.getByLabel('Definition namespace', { exact: true })).toHaveValue(
    'finance-prod',
  );
  await expect(page.getByLabel('Definition name', { exact: true })).toHaveValue('billing-close');
  await expect(page.locator('.definition-editor')).toBeVisible();
});

test('the failed billing step opens the extraction job and its recorded failure', async ({
  page,
}) => {
  await page.goto('/console/workflows/412/runs/94');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  const step = page
    .getByRole('row')
    .filter({ has: page.getByRole('link', { name: '#1040', exact: true }) });
  await expect(step).toContainText('extract');
  await expect(step).toContainText('Failed');
  await step.getByRole('link', { name: '#1040', exact: true }).click();
  const owner = page.getByRole('link', { name: 'billing-extract (finance-prod)', exact: true });
  await expect(owner).toBeVisible();
  await page.locator('details.attempt-log-disclosure > summary').click();
  await page.getByRole('button', { name: 'Read logs', exact: true }).click();
  await expect(page.getByLabel('Container log snapshot')).toHaveValue(
    /billing-extract.*attempt 1/s,
  );
  await owner.click();
  await expect(page.getByRole('heading', { name: 'billing-extract', exact: true })).toBeVisible();
});
