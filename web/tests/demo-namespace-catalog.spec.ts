import { expect, test, type Page } from './support';

async function recordManualNamespaceFallback(page: Page) {
  await page.addInitScript(() => {
    new MutationObserver(() => {
      if (document.querySelector('.namespace-field input[aria-label="Namespace"]'))
        document.documentElement.setAttribute('data-demo-manual-namespace-seen', 'true');
    }).observe(document, { childList: true, subtree: true });
  });
}

async function expectDemoNamespaceCatalog(page: Page) {
  await expect(page.getByRole('heading', { name: 'Create a job', exact: true })).toBeVisible();
  const namespace = page.getByRole('combobox', { name: 'Namespace', exact: true });
  await expect(namespace).toHaveValue('platform');
  await expect(namespace.locator('option')).toHaveText([
    'Choose an execution namespace',
    'finance-prod',
    'data-pipelines',
    'notifications',
    'platform',
    'orbitjob',
  ]);
  await expect(page.getByRole('textbox', { name: 'Namespace', exact: true })).toHaveCount(0);
  await expect(
    page.getByText('The workspace has not provided its namespace list.', { exact: false }),
  ).toHaveCount(0);
  await expect(page.locator('html')).not.toHaveAttribute('data-demo-manual-namespace-seen', 'true');
  return namespace;
}

async function openSampleWorkspaceSettings(page: Page) {
  await page
    .locator('.sidebar-bottom')
    .getByRole('link', { name: 'Workspace', exact: true })
    .click();
}

test('sample entry hydrates the namespace catalog and reconnecting starts a fresh draft', async ({
  page,
}) => {
  await recordManualNamespaceFallback(page);
  const businessRequests: string[] = [];
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/v1/') && !path.startsWith('/api/v1/auth/'))
      businessRequests.push(path);
  });
  await page.goto('/console/manifests/job');
  await page.getByRole('button', { name: 'Explore sample data', exact: true }).click();
  const namespace = await expectDemoNamespaceCatalog(page);
  const originalName = await page.getByLabel('Name', { exact: true }).inputValue();
  await namespace.selectOption('finance-prod');
  await expect(namespace).toHaveValue('finance-prod');
  await page.getByLabel('Name', { exact: true }).fill('discarded-demo-namespace-draft');
  await page.getByRole('tab', { name: 'Advanced YAML', exact: true }).click();
  await expect(page.getByLabel('Generated YAML', { exact: true })).toHaveValue(
    /namespace:\s*"?finance-prod"?/,
  );

  await openSampleWorkspaceSettings(page);
  await page.getByRole('button', { name: 'Leave sample data', exact: true }).click();
  await page.getByRole('button', { name: 'Explore sample data', exact: true }).click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).click();
  await expectDemoNamespaceCatalog(page);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue(originalName);
  expect(businessRequests).toEqual([]);
});

test('public-demo bootstrap and reset hydrate identity before mounting namespace fields', async ({
  page,
}) => {
  // Exercise Root's public-demo branches with the fixture server and the real local demo client.
  // Compiled public assets and their policy are verified separately.
  await page.route('**/src/deployment.ts*', (route) =>
    route.fulfill({
      contentType: 'application/javascript',
      body: `export const publicDemo = true;
export const fixtureProfile = false;
export const projectHome = '/';`,
    }),
  );
  await recordManualNamespaceFallback(page);
  const apiRequests: string[] = [];
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith('/api/')) apiRequests.push(path);
  });
  await page.goto('/console/#/manifests/job');
  const namespace = await expectDemoNamespaceCatalog(page);
  const originalName = await page.getByLabel('Name', { exact: true }).inputValue();
  await namespace.selectOption('data-pipelines');
  await expect(namespace).toHaveValue('data-pipelines');
  await page.getByLabel('Name', { exact: true }).fill('discarded-public-demo-draft');

  await openSampleWorkspaceSettings(page);
  await page.getByRole('button', { name: 'Reset sample data', exact: true }).click();
  await page.getByRole('button', { name: 'Open workspace', exact: true }).click();
  await page.getByRole('link', { name: 'Create a job', exact: true }).click();
  await expectDemoNamespaceCatalog(page);
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue(originalName);
  expect(apiRequests).toEqual([]);
});
