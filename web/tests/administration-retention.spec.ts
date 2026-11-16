import { expect, test, type Page } from './support';
async function prepare(page: Page) {
  await page.goto('/console/tests/fixtures/administration-retention.html');
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByRole('button', { name: /Commerce Operations/ }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Retention service');
  await page.getByRole('checkbox', { name: /Workload operator/ }).check();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await expect(page.getByText('Create requests: 1', { exact: true })).toBeVisible();
}
test('pending creation survives navigation and reveal survives tabs and workspace controls', async ({
  page,
}) => {
  await prepare(page);
  await expect(
    page.getByRole('button', { name: 'Management workspace', exact: true }),
  ).toBeDisabled();
  await page.getByRole('link', { name: 'Elsewhere', exact: true }).click();
  await page.getByRole('button', { name: 'Complete create response', exact: true }).click();
  const receipt = page.getByRole('status', { name: 'API key operation' });
  await expect(receipt).toContainText('ready to save');
  await expect(page.getByLabel('Generated API key')).toHaveCount(0);
  await page.getByRole('link', { name: 'Review API key operation', exact: true }).click();
  const field = page.getByLabel('Generated API key');
  await expect(field).toBeVisible();
  const secret = await field.inputValue();
  await expect(
    page.getByRole('button', { name: 'Management workspace', exact: true }),
  ).toBeDisabled();
  await page.getByRole('tab', { name: 'Policies', exact: true }).click();
  await expect(field).toHaveCount(0);
  await page.getByRole('link', { name: 'Review API key operation', exact: true }).click();
  await expect(field).toHaveValue(secret);
  await page.getByRole('link', { name: 'Elsewhere', exact: true }).click();
  await page.getByRole('link', { name: 'Administration', exact: true }).click();
  await expect(field).toHaveValue(secret);
  await page.evaluate(() => {
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
  });
  await page.getByRole('button', { name: 'Copy key', exact: true }).click();
  await expect(
    page.getByText('Copy unavailable. Select the key above and copy manually.'),
  ).toBeVisible();
  await expect(field).toHaveValue(secret);
  await page.getByRole('button', { name: 'Discard key', exact: true }).click();
  await expect(field).toHaveCount(0);
  await expect(receipt).toHaveCount(0);
  await expect(
    page.getByRole('button', { name: 'Management workspace', exact: true }),
  ).toBeEnabled();
});
test('disconnect suppresses late key response and unknown create allows explicit revoke', async ({
  page,
}) => {
  await prepare(page);
  await page.getByRole('button', { name: 'Disconnect fixture', exact: true }).click();
  await page.getByRole('button', { name: 'Complete create response', exact: true }).click();
  await expect(page.getByText('Fixture disconnected', { exact: true })).toBeVisible();
  await expect(page.getByRole('status', { name: 'API key operation' })).toHaveCount(0);
  await expect(page.getByLabel('Generated API key')).toHaveCount(0);
  await prepare(page);
  await page.getByRole('button', { name: 'Fail create response', exact: true }).click();
  await expect(page.getByText('The create outcome is unknown.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Create API key', exact: true })).toBeDisabled();
  await expect(
    page.getByRole('button', { name: 'Management workspace', exact: true }),
  ).toBeDisabled();
  await page.getByRole('button', { name: 'Refresh', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Revoke', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: 'Revoke', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'Revoked', exact: true })).toBeVisible();
  await expect(page.getByText('Create requests: 1', { exact: true })).toBeVisible();
});

test('320px dark administration tabs remain visible and keyboard accessible', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/console/tests/fixtures/administration-retention.html');
  const group = page.getByRole('tab', { name: 'Resource groups', exact: true });
  const bounds = await group.boundingBox();
  expect(bounds).not.toBeNull();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(320);
  await group.click();
  await expect(
    page.getByRole('heading', { name: 'Create resource group', exact: true }),
  ).toBeVisible();
  await group.focus();
  await page.keyboard.press('ArrowLeft');
  await expect(page.getByRole('tab', { name: 'Policies', exact: true })).toBeFocused();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({
    path: 'test-results/administration-retention-320dark.png',
    fullPage: true,
  });
});

test('one compact reminder combines key progress and Activity without exposing the secret', async ({
  page,
}) => {
  await page.goto('/console/tests/fixtures/administration-retention.html');
  await page.getByLabel('Name', { exact: true }).fill('New team');
  await page.getByLabel('Slug', { exact: true }).fill('new-team');
  await page.getByRole('button', { name: 'Create workspace', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Recent changes' })).toContainText('New team');
  await expect(page.locator('.administration-operation')).toHaveCount(0);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByRole('button', { name: /Commerce Operations/ }).click();
  await page.getByLabel('Key name', { exact: true }).fill('Release service');
  await page.getByRole('checkbox', { name: /Workload operator/ }).check();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  await page.getByRole('link', { name: 'Elsewhere', exact: true }).click();
  await expect(page.locator('.administration-operation')).toHaveCount(1);
  await expect(page.locator('.administration-operation')).toContainText(
    'API key creation is pending.',
  );
  await expect(
    page.getByRole('link', { name: 'View recent changes (1)', exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Complete create response', exact: true }).click();
  await expect(page.locator('.administration-operation')).toContainText('ready to save');
  await expect(page.locator('.administration-operation')).not.toContainText('demo-only-');
  await page.getByRole('link', { name: 'View recent changes (1)', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Recent changes', exact: true })).toBeFocused();
  await expect(page.locator('.administration-operation')).toHaveCount(1);
  await expect(page.locator('.administration-operation')).not.toContainText('ready to save');
  await page.getByRole('link', { name: 'Review API key operation', exact: true }).click();
  const secret = await page.getByLabel('Generated API key').inputValue();
  expect(await page.evaluate(() => JSON.stringify([localStorage, sessionStorage]))).not.toContain(
    secret,
  );
});
