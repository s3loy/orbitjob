import { expect, test } from './support';

test('local manifests preserve drafts across navigation and require a new check after edits', async ({
  page,
}) => {
  const writes: string[] = [];
  page.on('request', (r) => {
    if (r.method() !== 'GET') writes.push(r.url());
  });
  await page.goto('/console/connection');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page.getByRole('link', { name: 'Create a job' }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('review-draft');
  await page.getByRole('button', { name: 'Check draft', exact: true }).click();
  await expect(page.getByRole('tab', { name: 'Configuration', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Download YAML' })).toBeEnabled();
  await page.getByRole('tab', { name: 'Configuration', exact: true }).click();
  await page.getByRole('textbox', { name: 'Name', exact: true }).fill('retained-draft');
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Download YAML' })).toBeEnabled();
  const online = page.getByRole('button', { name: 'Review and save' });
  await expect(online).toBeDisabled();
  await expect(page).toHaveURL(/\/manifests\/job$/);
  await page.getByRole('link', { name: 'Back to Jobs' }).click();
  await page.getByRole('link', { name: 'Create a job' }).click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    'retained-draft',
  );
  await page.getByRole('button', { name: 'Discard draft', exact: true }).click();
  await page.getByRole('link', { name: 'Discard and return' }).click();
  await page.getByRole('link', { name: 'Create a job' }).click();
  await expect(page.getByRole('textbox', { name: 'Name', exact: true })).toHaveValue(
    'hello-orbitjob',
  );
  expect(writes).toEqual([]);
});

test('workflow separators remain typeable and invalid dependencies block online review while preserving export', async ({
  page,
}) => {
  await page.goto('/console/manifests/workflow');
  await page.getByRole('button', { name: 'Explore sample data' }).click();
  await page.getByRole('tab', { name: 'Tasks', exact: true }).click();
  await page.getByRole('button', { name: 'publish', exact: true }).click();
  const dependency = page.getByRole('textbox', { name: 'Dependencies', exact: true });
  await dependency.fill('extract');
  await dependency.press('End');
  await dependency.pressSequentially(',missing');
  await expect(dependency).toHaveValue('extract,missing');
  await page.getByRole('button', { name: 'Check draft', exact: true }).click();
  await expect(page.getByText('Invalid dependency missing.', { exact: false })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'Tasks', exact: true })).toHaveAttribute(
    'aria-selected',
    'true',
  );
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Download YAML' })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Review and save' })).toBeDisabled();
});
