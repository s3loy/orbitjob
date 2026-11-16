import { expect, test } from './support';
test('administration key creation reveals once, supports manual copy and confirms revoke', async ({
  page,
}) => {
  await page.goto('/console/');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page.getByRole('link', { name: 'Administration', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Administration', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Accounts', exact: true })).toHaveCount(0);
  await page.getByRole('tab', { name: 'API keys', exact: true }).click();
  await page.getByRole('button', { name: 'Management workspace', exact: true }).click();
  await page.getByRole('button', { name: /Commerce Operations/ }).click();
  await page.getByRole('button', { name: 'New API key', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Create API key', exact: true })).toBeFocused();
  await page.getByLabel('Key name', { exact: true }).fill('Release service');
  await page.getByRole('checkbox', { name: /Workload operator/ }).check();
  await page.getByRole('button', { name: 'Create API key', exact: true }).click();
  const reveal = page.getByRole('region', { name: 'One-time API key' });
  await expect(reveal).toBeVisible();
  await expect(reveal.getByRole('heading', { name: 'Save this key now' })).toBeFocused();
  const secret = await page.getByLabel('Generated API key').inputValue();
  expect(secret).toMatch(/^demo-only-/);
  expect(page.url()).not.toContain(secret);
  expect(
    await page.evaluate(
      (value) => JSON.stringify({ ...localStorage, ...sessionStorage }).includes(value),
      secret,
    ),
  ).toBe(false);
  await page.evaluate(() => {
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => false });
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: {
        writeText: async () => {
          throw new Error('Clipboard denied');
        },
      },
    });
  });
  await page.getByRole('button', { name: 'Copy key', exact: true }).click();
  await expect(
    page.getByText('Copy unavailable. Select the key above and copy manually.', { exact: true }),
  ).toBeVisible();
  await page.getByRole('button', { name: 'Select key', exact: true }).click();
  expect(
    await page
      .getByLabel('Generated API key')
      .evaluate((element: HTMLTextAreaElement) => [element.selectionStart, element.selectionEnd]),
  ).toEqual([0, secret.length]);
  await expect(page.getByLabel('Generated API key')).toHaveValue(secret);
  await page.evaluate(() =>
    Object.defineProperty(document, 'execCommand', { configurable: true, value: () => true }),
  );
  await page.getByRole('button', { name: 'Copy key', exact: true }).click();
  await expect(page.getByText('Copied.', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Copy key', exact: true })).toBeFocused();
  await page.getByRole('button', { name: 'Discard key', exact: true }).click();
  await expect(reveal).toHaveCount(0);
  await expect(page.getByRole('rowheader', { name: /Release service/ })).toBeVisible();
  await page.getByRole('button', { name: 'Rename', exact: true }).click();
  await page.getByLabel('New key name').fill('Production release');
  await page.getByRole('button', { name: 'Save name', exact: true }).click();
  await expect(page.getByRole('rowheader', { name: /Production release/ })).toBeVisible();
  await page.getByRole('button', { name: 'Revoke', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('cannot be undone');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Revoke', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Revoke', exact: true }).click();
  await page.getByRole('button', { name: 'Confirm revoke', exact: true }).click();
  await expect(page.getByRole('cell', { name: 'Revoked', exact: true })).toBeVisible();
});
